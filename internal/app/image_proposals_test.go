package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func (f *v2Fixture) analyzeProposals(t *testing.T, u domain.ImageRewrite) (domain.ImageRewrite, domain.Job) {
	t.Helper()
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", map[string]any{"revision": u.Revision, "suggest_proposals": true}, true))
	return f.load(t, u.ID), j
}

func TestImageV2ProposalAnalysisIncludesEveryImageAndReusesCache(t *testing.T) {
	for _, referenceCount := range []int{0, 2} {
		t.Run(fmt.Sprintf("references-%d", referenceCount), func(t *testing.T) {
			f := newImageV2Fixture(t)
			u := f.flexible(t)
			refs := []domain.ImageReference{}
			for i := 0; i < referenceCount; i++ {
				asset := f.flexibleAux(t, fmt.Sprintf("proposal-reference-%d", i), byte(i+11))
				refs = append(refs, domain.ImageReference{AssetID: asset.ID})
			}
			u = f.patch(t, u, map[string]any{"references": refs})
			u, job := f.analyzeProposals(t, u)
			if job.State != "succeeded" {
				t.Fatal(job.Error)
			}
			if len(u.Proposals) != 3 || len(u.Analyses) != 1+referenceCount || u.ProposalsInputDigest == "" || u.SelectedProposalID != "" {
				t.Fatal("missing analyzed options or automatic selection", u)
			}
			if f.visionCalls != 1+referenceCount || f.proposalCalls != 1 || f.imageCalls != 0 {
				t.Fatal("analysis skipped unused references or generated images", f.visionCalls, f.proposalCalls, f.imageCalls)
			}
			images := f.proposalInputs[0]["images"].([]any)
			if len(images) != 1+referenceCount || images[0].(map[string]any)["role"] != "primary" {
				t.Fatal("recommendations received incorrect images", images)
			}
			for index, image := range images {
				row := image.(map[string]any)
				if int(row["number"].(float64)) != index+1 || (index > 0 && row["role"] != "reference") {
					t.Fatal("recommendation image order changed", images)
				}
			}
			response := f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true)
			if response.Code != http.StatusConflict {
				t.Fatal("composed before the user selected a direction", response.Code)
			}
			u = f.patch(t, u, map[string]any{"selected_proposal_id": u.Proposals[1].ID})
			if !reflect.DeepEqual(u.FeatureControls, u.Proposals[1].FeatureControls) {
				t.Fatal("selection did not apply persisted controls")
			}
			u, job = f.analyzeProposals(t, u)
			if job.State != "succeeded" || u.SelectedProposalID != "" || f.visionCalls != 1+referenceCount || f.proposalCalls != 1 {
				t.Fatal("reanalysis ignored valid caches or retained confirmation", job.Error, u.SelectedProposalID, f.visionCalls, f.proposalCalls)
			}
		})
	}
}

func TestImageV2ProposalSelectionAndRequirementsAreAuthoritative(t *testing.T) {
	f := newImageV2Fixture(t)
	u, job := f.analyzeProposals(t, f.flexible(t))
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	response := f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "selected_proposal_id": "unknown"}, true)
	if response.Code != http.StatusConflict {
		t.Fatal("unknown proposal accepted", response.Code)
	}
	response = f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "extra_requirements": strings.Repeat("字", 1001)}, true)
	if response.Code != http.StatusBadRequest {
		t.Fatal("oversized requirements accepted", response.Code)
	}
	extra := "保留人物身份，背景留白更多"
	u = f.patch(t, u, map[string]any{"selected_proposal_id": u.Proposals[2].ID, "extra_requirements": extra})
	input, _, _, _, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
	if err != nil || input["extra_requirements"] != extra {
		t.Fatal("requirements missing from prompt basis", input, err)
	}
	if input["selected_proposal"].(map[string]string)["id"] != u.Proposals[2].ID {
		t.Fatal("selected direction missing from prompt basis", input)
	}
	job = runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	u = f.load(t, u.ID)
	if f.composed[0]["extra_requirements"] != extra || u.PromptInputDigest == "" || f.imageCalls != 0 {
		t.Fatal("composition lost user requirements or generated an image", u)
	}
	digest := u.PromptInputDigest
	u = f.patch(t, u, map[string]any{"output": domain.ImageOutput{Ratio: "1:1", Clarity: "high", Count: 3}})
	if len(u.Proposals) != 3 || u.SelectedProposalID == "" || u.PromptInputDigest != "" {
		t.Fatal("output ratio erased choices or failed to invalidate prompt")
	}
	u = f.patch(t, u, map[string]any{"extra_requirements": ""})
	_, _, _, clearedDigest, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
	if err != nil || clearedDigest == digest || u.SelectedProposalID == "" {
		t.Fatal("clearing optional requirements was not reflected in prompt basis", err)
	}
	u = f.patch(t, u, map[string]any{"extra_requirements": "另一项要求"})
	u = f.patch(t, u, map[string]any{"extra_requirements": nil})
	if u.ExtraRequirements != "" {
		t.Fatal("nullable requirements did not clear")
	}
}

func TestImageV2ProposalSelectionRejectsChangedImagesAndConfig(t *testing.T) {
	for _, mutation := range []string{"bytes", "text-model", "reference"} {
		t.Run(mutation, func(t *testing.T) {
			f := newImageV2Fixture(t)
			u, job := f.analyzeProposals(t, f.flexible(t))
			if job.State != "succeeded" {
				t.Fatal(job.Error)
			}
			proposalID := u.Proposals[0].ID
			u = f.patch(t, u, map[string]any{"selected_proposal_id": proposalID})
			switch mutation {
			case "bytes":
				path := filepath.Join(f.app.Data, f.xhsAsset.LocalPath)
				if err := os.WriteFile(path, append(append([]byte{}, f.png...), 31), 0600); err != nil {
					t.Fatal(err)
				}
			case "text-model":
				settings, err := f.app.settings()
				if err != nil {
					t.Fatal(err)
				}
				config := settings.Providers["text"]
				config.Model = "different-model"
				settings.Providers["text"] = config
				if err := f.app.saveSettings(settings); err != nil {
					t.Fatal(err)
				}
			case "reference":
				aux := f.flexibleAux(t, "new-reference", 33)
				u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}})
				if len(u.Proposals) != 0 || u.SelectedProposalID != "" || u.ProposalsInputDigest != "" {
					t.Fatal("image change retained stale proposals")
				}
			}
			response := f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "selected_proposal_id": proposalID}, true)
			if response.Code != http.StatusConflict {
				t.Fatal("stale proposal was accepted", response.Code)
			}
			if mutation != "reference" {
				response = f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true)
				if response.Code != http.StatusConflict {
					t.Fatal("stale proposal was composed", response.Code)
				}
			}
			if f.imageCalls != 0 {
				t.Fatal("stale analysis started image generation")
			}
		})
	}
}

func TestImageV2ProposalFailurePreservesAnalysisForRetry(t *testing.T) {
	for _, mode := range []string{"proposal-count", "proposal-failed"} {
		t.Run(mode, func(t *testing.T) {
			f := newImageV2Fixture(t)
			f.mode = mode
			u, job := f.analyzeProposals(t, f.flexible(t))
			if job.State != "failed" || len(u.Proposals) != 0 || len(u.Analyses) != 1 || u.SelectedProposalID != "" {
				t.Fatal("invalid proposals stored or visual analysis discarded", job, u)
			}
			f.mode = ""
			u, job = f.analyzeProposals(t, u)
			if job.State != "succeeded" || len(u.Proposals) != 3 || f.visionCalls != 1 || f.proposalCalls != 2 || f.imageCalls != 0 {
				t.Fatal("retry did not reuse completed image analysis", job.Error, f.visionCalls, f.proposalCalls, f.imageCalls)
			}
		})
	}
}

func TestImageV2ProposalMockGenerationPreservesBasisAndRequiresAdoption(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	aux := f.flexibleAux(t, "proposal-flow-reference", 43)
	other := f.flexibleAux(t, "proposal-flow-other-material", 44)
	var rewrite domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &rewrite); err != nil {
		t.Fatal(err)
	}
	before := rewrite.Versions[0]
	before.AssetIDs = []string{other.ID, f.xhsAsset.ID, aux.ID}
	rewrite.Versions[0] = before
	if err := f.app.Store.Put("rewrites", rewrite.ID, rewrite); err != nil {
		t.Fatal(err)
	}
	u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}})
	u, job := f.analyzeProposals(t, u)
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	chosen := u.Proposals[1]
	chosen.FeatureControls = copyImageFeatureControls(chosen.FeatureControls)
	extra := "背景留白更多，保留其他已选要求"
	u = f.patch(t, u, map[string]any{"selected_proposal_id": chosen.ID, "extra_requirements": extra})
	job = runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	u = f.load(t, u.ID)
	if !strings.Contains(u.Prompt, extra) || f.imageCalls != 0 {
		t.Fatal("composition omitted requirements or generated without confirmation")
	}
	request := map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "proposal-full-flow"}
	job = runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", request, true))
	if job.State != "succeeded" || f.imageCalls != 1 {
		t.Fatal("confirmed mock generation failed", job.Error)
	}
	if response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", request, true); response.Code != 202 || f.imageCalls != 1 {
		t.Fatal("duplicate confirmation generated twice", response.Code)
	}
	u = f.load(t, u.ID)
	if len(u.Results) != 1 || len(u.Batches) != 1 {
		t.Fatal("missing generated history")
	}
	result, batch := u.Results[0], u.Batches[0]
	if !reflect.DeepEqual(result.SelectedProposal, &chosen) || !reflect.DeepEqual(batch.SelectedProposal, &chosen) || result.ExtraRequirements != extra || batch.ExtraRequirements != extra {
		t.Fatal("history lost chosen proposal or additional requirements")
	}
	actual := f.submitted[0]
	if actual["prompt"] != u.Prompt || actual["prompt_extend"] != false || actual["size"] != "960x1280" || len(actual["image"].([]any)) != 2 || !reflect.DeepEqual(result.SourceAssetIDs, []string{f.xhsAsset.ID, aux.ID}) {
		t.Fatal("actual generation differs from reviewed basis")
	}
	if err := f.app.Store.Get("rewrites", rewrite.ID, &rewrite); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rewrite.Versions[0], before) || rewrite.ConfirmedVersionID != before.ID {
		t.Fatal("generation changed publishing materials or copy before adoption")
	}
	applied := decodeResponse[domain.Version](t, f.call(t, "POST", f.base+"/"+u.ID+"/apply", map[string]any{"asset_id": result.AssetID, "version_id": before.ID}, true), 201)
	if !reflect.DeepEqual(applied.AssetIDs, []string{other.ID, result.AssetID, aux.ID}) || applied.Title != before.Title || applied.Body != before.Body || applied.Script != before.Script || !reflect.DeepEqual(applied.Topics, before.Topics) {
		t.Fatal("adoption changed non-primary materials, ordering or copy")
	}
	u = f.load(t, u.ID)
	u = f.patch(t, u, map[string]any{"selected_proposal_id": u.Proposals[2].ID, "extra_requirements": "下一次改动"})
	if u.PromptInputDigest != "" || !reflect.DeepEqual(u.Results[0], result) || !reflect.DeepEqual(u.Batches[0], batch) {
		t.Fatal("later edits rewrote historical generation basis or kept stale prompt")
	}
}

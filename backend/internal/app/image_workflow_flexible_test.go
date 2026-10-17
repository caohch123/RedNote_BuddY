package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func allFlexibleControls(mode, assetID string) map[string]domain.ImageFeatureControl {
	controls := map[string]domain.ImageFeatureControl{}
	for _, key := range imageFlexibleFeatureNames {
		controls[key] = domain.ImageFeatureControl{Mode: mode, AssetID: assetID}
	}
	return controls
}

func (f *v2Fixture) flexible(t *testing.T) domain.ImageRewrite {
	t.Helper()
	return decodeResponse[domain.ImageRewrite](t, f.call(t, "POST", f.base, map[string]any{"workflow_version": 2, "version_id": f.xhsRewrite.ConfirmedVersionID, "primary_asset_id": f.xhsAsset.ID}, true), http.StatusCreated)
}

func (f *v2Fixture) flexibleAux(t *testing.T, name string, marker byte) domain.Asset {
	t.Helper()
	asset := f.xhsAsset
	asset.ID, asset.OwnerID, asset.LocalPath = name, f.xhsRewrite.ID, "assets/"+name+".png"
	data := append(append([]byte{}, f.png...), marker)
	if err := os.WriteFile(filepath.Join(f.app.Data, asset.LocalPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
		t.Fatal(err)
	}
	return asset
}

func (f *v2Fixture) saveFlexibleAnalyses(t *testing.T, u domain.ImageRewrite) domain.ImageRewrite {
	t.Helper()
	u.Analyses = nil
	for _, id := range imageV2IDs(u) {
		data, _, err := f.app.readImageV2File(f.xhsRewrite.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		u.Analyses = append(u.Analyses, domain.ImageAnalysis{AssetID: id, SHA256: hex.EncodeToString(hash[:]), RecognizedText: "文字资料-" + id, Features: domain.ImageFeatures{
			Subject: "对象-" + id, Action: "动作-" + id, Scene: "环境-" + id, Composition: "布局-" + id,
			Palette: "颜色-" + id, Lighting: "光影-" + id, Texture: "画法-" + id, Text: "版式-" + id,
		}})
	}
	var rewrite domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &rewrite); err != nil {
		t.Fatal(err)
	}
	_, index, _ := getImageRewrite(rewrite, u.ID)
	rewrite.ImageRewrites[index] = u
	if err := f.app.Store.Put("rewrites", rewrite.ID, rewrite); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestImageV2FlexibleControlsValidationAndIncompleteDrafts(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	cases := map[string]func(map[string]domain.ImageFeatureControl){
		"unknown dimension": func(c map[string]domain.ImageFeatureControl) {
			delete(c, "action")
			c["unknown"] = domain.ImageFeatureControl{Mode: "auto"}
		},
		"unknown mode": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "locked"}
		},
		"foreign source": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "image", AssetID: f.dyAsset.ID}
		},
		"image plus custom": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "image", AssetID: u.PrimaryAssetID, Value: "conflict"}
		},
		"custom plus image": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "custom", AssetID: u.PrimaryAssetID, Value: "object"}
		},
		"long custom": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "custom", Value: strings.Repeat("字", 501)}
		},
		"auto plus image": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "auto", AssetID: u.PrimaryAssetID}
		},
		"auto plus custom": func(c map[string]domain.ImageFeatureControl) {
			c["subject"] = domain.ImageFeatureControl{Mode: "auto", Value: "object"}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			controls := copyImageFeatureControls(u.FeatureControls)
			change(controls)
			response := f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "feature_controls": controls}, true)
			if response.Code != 400 || f.load(t, u.ID).Revision != u.Revision {
				t.Fatal("invalid control was saved", response.Code, response.Body.String())
			}
		})
	}
	controls := copyImageFeatureControls(u.FeatureControls)
	controls["subject"] = domain.ImageFeatureControl{Mode: "image"}
	u = f.patch(t, u, map[string]any{"feature_controls": controls})
	if u.FeatureControls["subject"].AssetID != "" {
		t.Fatal("pending source was not preserved")
	}
	response := f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true)
	if response.Code != 409 || f.textCalls != 0 || f.visionCalls != 0 || f.imageCalls != 0 {
		t.Fatal("incomplete draft made a model request", response.Code, response.Body.String())
	}
	controls["subject"] = domain.ImageFeatureControl{Mode: "custom", Value: "   "}
	u = f.patch(t, u, map[string]any{"feature_controls": controls})
	if u.FeatureControls["subject"].Value != "" {
		t.Fatal("blank custom draft not normalized")
	}
	response = f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true)
	if response.Code != 409 || f.textCalls != 0 || f.visionCalls != 0 || f.imageCalls != 0 {
		t.Fatal("empty custom draft made a model request", response.Code, response.Body.String())
	}
}

func TestImageV2FlexibleAnyDimensionCanUseImagesCustomOrAuto(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	aux1, aux2 := f.flexibleAux(t, "aux-one", 1), f.flexibleAux(t, "aux-two", 2)
	u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux1.ID}, {AssetID: aux2.ID}}})
	u = f.saveFlexibleAnalyses(t, u)
	for _, key := range imageFlexibleFeatureNames {
		for _, mode := range []string{"image", "custom", "auto"} {
			t.Run(key+"/"+mode, func(t *testing.T) {
				controls := allFlexibleControls("auto", "")
				control := domain.ImageFeatureControl{Mode: mode}
				if mode == "image" {
					control.AssetID = aux2.ID
				}
				if mode == "custom" {
					control.Value = "用户定义-" + key
				}
				controls[key] = control
				u = f.patch(t, u, map[string]any{"feature_controls": controls, "feature_sources": map[string]string{key: u.PrimaryAssetID}})
				input, ids, hashes, digest, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
				if err != nil {
					t.Fatal(err)
				}
				wire := input["feature_controls"].(map[string]map[string]any)
				if len(wire) != 8 || wire[key]["mode"] != mode {
					t.Fatal("controls not complete", wire)
				}
				if mode == "image" {
					if !reflect.DeepEqual(ids, []string{aux2.ID}) || len(hashes) != 1 || wire[key]["image_number"] != 1 || u.FeatureSources[key] != aux2.ID {
						t.Fatal("source mapping includes main/unselected auxiliary", ids, wire)
					}
					image := input["images"].([]map[string]any)[0]
					if image["role"] != "reference" || len(image["features"].(map[string]string)) != 1 {
						t.Fatal("unselected feature transmitted", image)
					}
				} else if len(ids) != 0 || len(hashes) != 0 || len(u.FeatureSources) != 0 || len(input["images"].([]map[string]any)) != 0 {
					t.Fatal("custom/auto retained former source", ids, u.FeatureSources)
				}
				encoded, _ := json.Marshal(input)
				var restored map[string]any
				_ = json.Unmarshal(encoded, &restored)
				if digestImageValue([]any{restored, ids, hashes}) != digest {
					t.Fatal("durable snapshot digest changes after JSON round trip")
				}
			})
		}
	}
	if f.textCalls != 0 || f.visionCalls != 0 || f.imageCalls != 0 {
		t.Fatal("ordinary edits or payload construction called a model")
	}
}

func TestImageV2FlexibleSelectedOrderAndTextSources(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	aux := f.flexibleAux(t, "chosen-reference", 3)
	u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}})
	u = f.saveFlexibleAnalyses(t, u)
	controls := allFlexibleControls("auto", "")
	controls["subject"] = domain.ImageFeatureControl{Mode: "image", AssetID: u.PrimaryAssetID}
	controls["action"] = domain.ImageFeatureControl{Mode: "image", AssetID: aux.ID}
	controls["text"] = domain.ImageFeatureControl{Mode: "image", AssetID: aux.ID}
	controls["palette"] = domain.ImageFeatureControl{Mode: "custom", Value: "蓝白"}
	u = f.patch(t, u, map[string]any{"feature_controls": controls, "image_text": "不得传递的旧全局文字"})
	input, ids, _, _, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{u.PrimaryAssetID, aux.ID}) {
		t.Fatal("effective order differs", ids)
	}
	images := input["images"].([]map[string]any)
	if _, ok := images[0]["recognized_text"]; ok {
		t.Fatal("unselected main text leaked")
	}
	if images[1]["recognized_text"] != "文字资料-"+aux.ID {
		t.Fatal("selected text source missing")
	}
	encoded, _ := json.Marshal(input)
	for _, excluded := range []string{"不得传递的旧全局文字", "颜色-", "环境-", "画法-", "动作-" + u.PrimaryAssetID, "文字资料-" + u.PrimaryAssetID} {
		if strings.Contains(string(encoded), excluded) {
			t.Fatal("unselected source content leaked", excluded)
		}
	}
	sources := input["feature_sources"].(map[string]int)
	if sources["subject"] != 1 || sources["action"] != 2 || sources["text"] != 2 || len(sources) != 3 {
		t.Fatal("incorrect dimension binding", sources)
	}
}

func TestImageV2FlexibleOldAnalysisRequiresExplicitRefreshAndZeroImageNeedsNone(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	u = f.saveFlexibleAnalyses(t, u)
	u.Analyses[0].Features.Action = ""
	if _, _, _, _, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u); err == nil || !strings.Contains(err.Error(), "重新分析") {
		t.Fatal("legacy analysis silently reused", err)
	}
	u.FeatureControls = allFlexibleControls("auto", "")
	u.FeatureControls["subject"] = domain.ImageFeatureControl{Mode: "custom", Value: "一只红狐狸"}
	u.Analyses = nil
	input, ids, _, _, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
	if err != nil || len(ids) != 0 || len(input["images"].([]map[string]any)) != 0 || f.visionCalls != 0 {
		t.Fatal("zero-image composition requires analysis or transmits main", err, ids)
	}
}

func TestImageV2FlexibleUpstreamChangesInvalidateWithoutRewritingHistory(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	u.FeatureControls = allFlexibleControls("auto", "")
	u.FeatureControls["subject"] = domain.ImageFeatureControl{Mode: "custom", Value: "一只红狐狸"}
	_, _, _, digest, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	u.Prompt, u.PromptInputDigest, u.PromptRulesVersion = "已确认的狐狸提示词", digest, imagePromptRulesVersion
	u.Status = "prompt_ready"
	u.FeatureSources = imageFeatureControlsSources(u.FeatureControls)
	u.Batches = []domain.ImageBatch{{FeatureControls: copyImageFeatureControls(u.FeatureControls), Prompt: u.Prompt}}
	u.Results = []domain.ImageResult{{FeatureControls: copyImageFeatureControls(u.FeatureControls), Prompt: u.Prompt}}
	var rewrite domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &rewrite)
	rewrite.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", rewrite.ID, rewrite)
	u = f.patch(t, u, map[string]any{"output": domain.ImageOutput{Ratio: "3:4", Clarity: "high", Count: 4}})
	if u.PromptInputDigest != digest {
		t.Fatal("clarity/count invalidated prompt")
	}
	controls := copyImageFeatureControls(u.FeatureControls)
	controls["action"] = domain.ImageFeatureControl{Mode: "custom", Value: "向右奔跑"}
	u = f.patch(t, u, map[string]any{"feature_controls": controls})
	if u.PromptInputDigest != "" || u.Results[0].FeatureControls["action"].Mode != "auto" || u.Batches[0].FeatureControls["action"].Mode != "auto" || u.Results[0].Prompt != "已确认的狐狸提示词" {
		t.Fatal("current edit rewrote history or retained stale prompt")
	}
	response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "stale-flexible"}, true)
	if response.Code != 409 || f.imageCalls != 0 {
		t.Fatal("stale flexible prompt generated", response.Code)
	}
}

func TestImageV2FlexibleMockGenerationUsesOnlySelectedImagesOrNone(t *testing.T) {
	for _, source := range []string{"custom", "auto", "auxiliary"} {
		t.Run(source, func(t *testing.T) {
			f := newImageV2Fixture(t)
			u := f.flexible(t)
			aux := f.flexibleAux(t, "input-reference", 4)
			controls := allFlexibleControls("auto", "")
			controls["subject"] = domain.ImageFeatureControl{Mode: "custom", Value: "一只飞行机械鸟"}
			if source == "auxiliary" {
				controls["action"] = domain.ImageFeatureControl{Mode: "image", AssetID: aux.ID}
			} else if source == "auto" {
				controls["subject"] = domain.ImageFeatureControl{Mode: "auto"}
			}
			u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}, "feature_controls": controls})
			if source == "auxiliary" {
				u = f.saveFlexibleAnalyses(t, u)
			}
			job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
			if job.State != "succeeded" || f.textCalls != 1 || f.visionCalls != 0 || f.imageCalls != 0 {
				t.Fatal("composition failed, retried, or triggered another stage", job.State, job.Error)
			}
			u = f.load(t, u.ID)
			if source == "custom" && !strings.Contains(u.Prompt, "一只飞行机械鸟") {
				t.Fatal("custom requirement omitted")
			}
			request := map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "flexible-" + source}
			job = runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", request, true))
			if job.State != "succeeded" || f.imageCalls != 1 {
				t.Fatal("generation failed", job.Error)
			}
			if response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", request, true); response.Code != 202 || f.imageCalls != 1 {
				t.Fatal("duplicate generated again", response.Code)
			}
			actual := f.submitted[0]
			if actual["prompt"] != u.Prompt || actual["prompt_extend"] != false || actual["size"] != "960x1280" {
				t.Fatal("submitted basis differs from reviewed settings")
			}
			if source == "auxiliary" {
				if len(actual["image"].([]any)) != 1 {
					t.Fatal("unselected main was transmitted")
				}
			} else if _, exists := actual["image"]; exists {
				t.Fatal("zero-image request transmitted an image field")
			}
			u = f.load(t, u.ID)
			if len(u.Results) != 1 || len(u.Batches) != 1 || !reflect.DeepEqual(u.Results[0].FeatureControls, controls) || !reflect.DeepEqual(u.Batches[0].FeatureControls, controls) {
				t.Fatal("history missing exact feature controls")
			}
			if u.Results[0].PrimaryAssetID != f.xhsAsset.ID {
				t.Fatal("unselected main replacement anchor lost")
			}
			if source == "auxiliary" && !reflect.DeepEqual(u.Results[0].SourceAssetIDs, []string{aux.ID}) {
				t.Fatal("auxiliary-only snapshot differs")
			}
			if source != "auxiliary" && len(u.Results[0].SourceAssetIDs) != 0 {
				t.Fatal("zero-input snapshot injected source")
			}
			before := f.xhsRewrite.Versions[0]
			applied := decodeResponse[domain.Version](t, f.call(t, "POST", f.base+"/"+u.ID+"/apply", map[string]any{"asset_id": u.Results[0].AssetID, "version_id": before.ID}, true), 201)
			if has(applied.AssetIDs, f.xhsAsset.ID) || !has(applied.AssetIDs, u.Results[0].AssetID) || applied.Body != before.Body || applied.Title != before.Title {
				t.Fatal("apply lost anchor or modified copy")
			}
		})
	}
}

func TestImageV2FlexibleDeletionProtectsUnusedHistoricalMainAnchor(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	anchor := f.flexibleAux(t, "old-unused-main-anchor", 5)
	u.Batches = []domain.ImageBatch{{PrimaryAssetID: anchor.ID}}
	var rewrite domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &rewrite)
	rewrite.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", rewrite.ID, rewrite)
	response := f.call(t, "DELETE", "/assets/"+anchor.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if response.Code != 409 {
		t.Fatal("historical replacement anchor was deleted", response.Code, response.Body.String())
	}
}

func TestImageV2FlexibleAnalyzeOnlyContributorsAndReuseCache(t *testing.T) {
	for _, mode := range []string{"zero", "auxiliary", "explicit_unselected"} {
		t.Run(mode, func(t *testing.T) {
			f := newImageV2Fixture(t)
			u := f.flexible(t)
			aux := f.flexibleAux(t, "only-analyzed-reference", 6)
			controls := allFlexibleControls("auto", "")
			if mode == "auxiliary" {
				controls["scene"] = domain.ImageFeatureControl{Mode: "image", AssetID: aux.ID}
			}
			u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}, "feature_controls": controls})
			for attempt := 0; attempt < 2; attempt++ {
				body := map[string]any{"revision": u.Revision}
				if mode == "explicit_unselected" {
					body["asset_id"] = aux.ID
				}
				job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", body, true))
				if job.State != "succeeded" {
					t.Fatal("analysis failed", job.Error)
				}
				u = f.load(t, u.ID)
			}
			if mode == "zero" {
				if f.visionCalls != 0 || len(u.Analyses) != 0 {
					t.Fatal("zero-source analysis made a vision call")
				}
			} else if f.visionCalls != 1 || len(u.Analyses) != 1 || u.Analyses[0].AssetID != aux.ID {
				t.Fatal("analyzed unselected images or ignored cache", f.visionCalls, u.Analyses)
			}
			if f.textCalls != 0 || f.imageCalls != 0 {
				t.Fatal("analysis triggered later stages")
			}
		})
	}
}

func TestImageV2FlexibleReanalysisKeepsLocalOCRWithoutSharingCorrections(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.flexible(t)
	u = f.saveFlexibleAnalyses(t, u)
	u.Analyses[0].RecognizedText = "用户人工修正的项目私有文字"
	var rewrite domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &rewrite)
	rewrite.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", rewrite.ID, rewrite)
	for attempt := 0; attempt < 2; attempt++ {
		job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", map[string]any{"revision": u.Revision}, true))
		if job.State != "succeeded" {
			t.Fatal(job.Error)
		}
		u = f.load(t, u.ID)
		if u.Analyses[0].RecognizedText != "用户人工修正的项目私有文字" {
			t.Fatal("reanalysis lost local OCR")
		}
	}
	if f.visionCalls != 1 {
		t.Fatal("repeat analysis bypassed content cache")
	}
	settings, _ := f.app.settings()
	key := digestImageValue([]string{u.Analyses[0].SHA256, imageConfigSignature(settings.Providers["vision"]), "visual-features-v2.4-flexible"})
	var cache imageAnalysisCache
	if err := f.app.Store.Get("image_analysis_cache", key, &cache); err != nil || cache.Analysis.RecognizedText != "" {
		t.Fatal("project OCR leaked into shared cache", err)
	}
	// The same image in another scheme gets the shared analysis, not the
	// previous project's or scheme's private text correction.
	other := f.flexible(t)
	job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+other.ID+"/analyze", map[string]any{"revision": other.Revision}, true))
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	other = f.load(t, other.ID)
	if f.visionCalls != 1 || other.Analyses[0].RecognizedText != "" {
		t.Fatal("shared cache contains another scheme's text correction")
	}
}

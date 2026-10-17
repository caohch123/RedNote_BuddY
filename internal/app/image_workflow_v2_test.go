package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

type v2Fixture struct {
	workspaceFixture
	base                               string
	visionCalls, textCalls, imageCalls int
	submitted                          []map[string]any
	composed                           []map[string]any
	proposalCalls                      int
	proposalInputs                     []map[string]any
	mode                               string
	downloads                          int
	server                             *httptest.Server
}

func newImageV2Fixture(t *testing.T) *v2Fixture {
	f := &v2Fixture{}
	f.workspaceFixture = seedWorkspaceFixture(t)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/result.png" {
			f.downloads++
			if f.downloads == 1 {
				http.Error(w, "temporary", 503)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(f.png)
			return
		}
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		if r.URL.Path == "/images/generations" {
			f.imageCalls++
			f.submitted = append(f.submitted, request)
			if f.mode == "second-failed" && f.imageCalls == 2 {
				http.Error(w, `{"error":{"message":"known failure"}}`, 400)
				return
			}
			if f.mode == "unknown" {
				http.Error(w, "unknown", 503)
				return
			}
			w.Header().Set("x-request-id", "request-fixture")
			if f.mode == "receipt" {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"url": f.server.URL + "/result.png"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(f.png)}}})
			return
		}
		content := `{"subject":"主角与动作","action":"自然站立","scene":"可选背景","composition":"中心构图","palette":"未选择的秘密配色","lighting":"侧光","texture":"写实质感","text":"两栏排版"}`
		if request["model"] == "text-fixture" {
			f.textCalls++
			raw, _ := json.Marshal(request)
			if strings.Contains(string(raw), f.xhs.Body) {
				t.Error("unselected features or note body entered prompt planning")
			}
			messages := request["messages"].([]any)
			var input map[string]any
			_ = json.Unmarshal([]byte(messages[1].(map[string]any)["content"].(string)), &input)
			if input["feature_sources"] == nil && input["edit_intent"] == nil {
				f.proposalCalls++
				f.proposalInputs = append(f.proposalInputs, input)
				options := []map[string]any{}
				for i := 0; i < 3; i++ {
					controls := map[string]any{}
					for _, key := range imageFlexibleFeatureNames {
						controls[key] = map[string]any{"mode": "image", "image_number": 1}
					}
					controls["palette"] = map[string]any{"mode": "custom", "value": fmt.Sprintf("明确配色方案%d", i+1)}
					if i == 1 && len(input["images"].([]any)) > 1 {
						controls["subject"] = map[string]any{"mode": "image", "image_number": 2}
					}
					options = append(options, map[string]any{"title": fmt.Sprintf("创作方向%d", i+1), "summary": fmt.Sprintf("保留图片依据，采用第%d种明确配色", i+1), "feature_controls": controls})
				}
				if f.mode == "proposal-count" {
					options = options[:2]
				}
				if f.mode == "proposal-failed" {
					http.Error(w, "recommendation unavailable", http.StatusBadRequest)
					return
				}
				encoded, _ := json.Marshal(map[string]any{"proposals": options})
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(encoded)}}}})
				return
			}
			f.composed = append(f.composed, input)
			intent, _ := input["edit_intent"].(map[string]any)
			if intent["mode"] != "preserve_content" && intent["mode"] != "flexible" && strings.Contains(string(raw), "未选择的秘密配色") {
				t.Error("unselected features entered prompt planning")
			}
			subject := ""
			for _, image := range input["images"].([]any) {
				if value, ok := image.(map[string]any)["features"].(map[string]any)["subject"].(string); ok {
					subject = value
				}
			}
			constraints := "不添加文字、标志或水印。"
			sources := input["feature_sources"].(map[string]any)
			if source, ok := sources["subject"]; ok {
				if palette, selected := sources["palette"]; selected && palette != source {
					constraints += fmt.Sprintf("最终配色以图片%.0f为准。", palette)
				}
				if texture, selected := sources["texture"]; selected && texture != source {
					constraints += fmt.Sprintf("最终画法与材质以图片%.0f为准。", texture)
				}
			}
			encoded, _ := json.Marshal(map[string]any{"prompt": "画面主体：" + subject + "；中心构图，背景使用所选可选背景；输出单张图片。" + constraints, "feature_sources": input["feature_sources"]})
			if intent["mode"] == "preserve_content" {
				target := map[string]string{}
				for _, image := range input["images"].([]any) {
					for key, value := range image.(map[string]any)["features"].(map[string]any) {
						target[key] = value.(string)
					}
				}
				if style, _ := intent["style"].(string); style != "" {
					target["texture"] = style
				}
				if palette, _ := intent["palette"].(string); palette != "" {
					target["palette"] = palette
				}
				prompt := "以图片1为底图，保留主图内容、动作与布局；保留主图已有文字，不新增文案、标志或水印。"
				for _, key := range imageFeatureNames {
					if value := target[key]; value != "" {
						prompt += value + "。"
					}
				}
				encoded, _ = json.Marshal(map[string]any{"prompt": prompt, "feature_sources": sources, "target_features": target, "source": subject, "preserved_entities": []string{subject}, "checks": map[string]bool{"main_content_preserved": true, "reference_subject_excluded": true, "appearance_conflicts_resolved": true}})
			}
			if intent["mode"] == "flexible" {
				controls := input["feature_controls"].(map[string]any)
				resolved := map[string]string{}
				for _, key := range imageFlexibleFeatureNames {
					control := controls[key].(map[string]any)
					resolved[key] = "自动设计" + key
					if control["mode"] == "custom" {
						resolved[key] = control["value"].(string)
					} else if control["mode"] == "image" {
						number := int(control["image_number"].(float64))
						resolved[key] = input["images"].([]any)[number-1].(map[string]any)["features"].(map[string]any)[key].(string)
					}
				}
				encoded, _ = json.Marshal(map[string]any{"resolved_features": resolved, "feature_controls": controls, "conflicts": []string{}})
			}
			if f.mode == "compose-omits-subject" {
				encoded, _ = json.Marshal(map[string]any{"prompt": "红棕背景和白色线条，不出现人物、猫耳、女仆装或具象角色。", "feature_sources": input["feature_sources"]})
			}
			content = string(encoded)
		} else {
			f.visionCalls++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
	}))
	t.Cleanup(f.server.Close)
	_ = f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{"vision": {BaseURL: f.server.URL, APIKey: "fixture", Model: "vision-fixture"}, "text": {BaseURL: f.server.URL, APIKey: "fixture", Model: "text-fixture"}, "image": {BaseURL: f.server.URL, APIKey: "fixture", Model: "qwen-image-3.0-pro"}}})
	f.base = "/rewrites/" + f.xhsRewrite.ID + "/images"
	return f
}
func (f *v2Fixture) load(t *testing.T, id string) domain.ImageRewrite {
	t.Helper()
	var r domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &r); err != nil {
		t.Fatal(err)
	}
	u, _, ok := getImageRewrite(r, id)
	if !ok {
		t.Fatal("unit missing")
	}
	return u
}
func (f *v2Fixture) create(t *testing.T) domain.ImageRewrite {
	t.Helper()
	u := decodeResponse[domain.ImageRewrite](t, f.call(t, "POST", f.base+"?platform=xiaohongshu", map[string]any{"workflow_version": 2, "version_id": f.xhsRewrite.ConfirmedVersionID, "primary_asset_id": f.xhsAsset.ID}, true), 201)
	// Existing regression cases exercise schemes saved before preservation
	// mode. New-default and preservation behavior have separate tests below.
	return f.patch(t, u, map[string]any{"edit_intent": domain.ImageEditIntent{Mode: "feature_mix"}, "feature_sources": map[string]string{"subject": f.xhsAsset.ID, "composition": f.xhsAsset.ID}})
}
func (f *v2Fixture) patch(t *testing.T, u domain.ImageRewrite, fields map[string]any) domain.ImageRewrite {
	t.Helper()
	fields["revision"] = u.Revision
	return decodeResponse[domain.ImageRewrite](t, f.call(t, "PATCH", f.base+"/"+u.ID, fields, true), 200)
}
func (f *v2Fixture) prepare(t *testing.T) domain.ImageRewrite {
	t.Helper()
	u := f.create(t)
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	u = f.load(t, u.ID)
	u = f.patch(t, u, map[string]any{"text_mode": "keep", "image_text": "确认的完整图中文字", "prompt": u.Prompt})
	j = runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	return f.load(t, u.ID)
}
func (f *v2Fixture) claim(t *testing.T) domain.Job {
	t.Helper()
	j, err := f.app.Store.Claim("image_rewrite")
	if err != nil || j == nil {
		t.Fatal("expected queued image", err)
	}
	f.app.runJob(j)
	return *j
}

func TestImageV2NewSchemesDefaultToIndependentControls(t *testing.T) {
	f := newImageV2Fixture(t)
	u := decodeResponse[domain.ImageRewrite](t, f.call(t, "POST", f.base, map[string]any{"workflow_version": 2, "version_id": f.xhsRewrite.ConfirmedVersionID, "primary_asset_id": f.xhsAsset.ID}, true), 201)
	if u.EditIntent.Mode != "flexible" || len(u.FeatureSources) != 8 || len(u.FeatureControls) != 8 {
		t.Fatal("new schemes must expose eight independent controls")
	}
	bad := copyImageFeatureControls(u.FeatureControls)
	delete(bad, "subject")
	response := f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "feature_controls": bad}, true)
	if response.Code != 400 || f.load(t, u.ID).Revision != u.Revision {
		t.Fatal("incomplete controls accepted or changed failed draft")
	}
	before := imageV2SemanticDigest(u)
	u.FeatureControls["texture"] = domain.ImageFeatureControl{Mode: "custom", Value: "数字插画"}
	u.FeatureControls["palette"] = domain.ImageFeatureControl{Mode: "custom", Value: "蓝白"}
	if before == imageV2SemanticDigest(u) {
		t.Fatal("appearance target did not invalidate reviewed prompt")
	}
	legacy := u
	legacy.EditIntent = domain.ImageEditIntent{}
	explicit := legacy
	explicit.EditIntent.Mode = "feature_mix"
	if imageV2SemanticDigest(legacy) != imageV2SemanticDigest(explicit) {
		t.Fatal("blank legacy intent differs from explicit feature mix")
	}
}

func TestImageV2AppearanceOverridesExcludeUnusedAuxiliaryAndPersistIntent(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.create(t)
	aux := f.xhsAsset
	aux.ID, aux.OwnerID, aux.LocalPath = "unused-appearance", f.xhsRewrite.ID, "assets/unused-appearance.png"
	if err := os.WriteFile(filepath.Join(f.app.Data, aux.LocalPath), f.png, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("assets", aux.ID, aux); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, key := range imageFeatureNames {
		sources[key] = u.PrimaryAssetID
	}
	sources["palette"], sources["texture"] = aux.ID, aux.ID
	intent := domain.ImageEditIntent{Mode: "preserve_content", Style: "数字插画", Palette: "蓝白"}
	u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}, "feature_sources": sources, "edit_intent": intent})
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", map[string]any{"revision": u.Revision, "asset_id": u.PrimaryAssetID}, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	u = f.load(t, u.ID)
	j = runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	u = f.load(t, u.ID)
	if len(f.composed) != 1 || len(f.composed[0]["images"].([]any)) != 1 || f.visionCalls != 1 || f.imageCalls != 0 {
		t.Fatal("explicit appearance incorrectly required or transmitted unused auxiliary")
	}
	inputSources := f.composed[0]["feature_sources"].(map[string]any)
	if _, ok := inputSources["palette"]; ok {
		t.Fatal("overridden palette still supplied source text")
	}
	if !strings.Contains(u.Prompt, "蓝白") || !strings.Contains(u.Prompt, "数字插画") || strings.Contains(u.Prompt, "未选择的秘密配色") {
		t.Fatal("explicit targets not applied to final prompt")
	}
	generated := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "preserve-main-blue-white"}, true))
	if generated.State != "succeeded" || f.imageCalls != 1 {
		t.Fatal(generated.Error)
	}
	if len(f.submitted[0]["image"].([]any)) != 1 || f.submitted[0]["prompt_extend"] != false || f.submitted[0]["prompt"] != u.Prompt {
		t.Fatal("image submission differs from reviewed intent")
	}
	u = f.load(t, u.ID)
	if u.Results[0].EditIntent != intent || u.Batches[0].EditIntent != intent || len(u.Results[0].SourceAssetIDs) != 1 || u.Results[0].FeatureSources["texture"] != "" {
		t.Fatal("history does not record effective source inputs and exact intent")
	}
	intent.Palette = "黑白"
	u = f.patch(t, u, map[string]any{"edit_intent": intent})
	if u.PromptInputDigest != "" || u.Results[0].EditIntent.Palette != "蓝白" {
		t.Fatal("target change failed to invalidate prompt or rewrote historical intent")
	}
	response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "stale-preserve"}, true)
	if response.Code != 409 || f.imageCalls != 1 {
		t.Fatal("stale appearance target allowed another generation")
	}
}

func TestImageV2FullFormPatchPreservesReviewedPromptWithoutReferences(t *testing.T) {
	for _, sendReferences := range []bool{true, false} {
		name := "omitted_references"
		if sendReferences {
			name = "empty_references"
		}
		t.Run(name, func(t *testing.T) {
			f := newImageV2Fixture(t)
			u := f.prepare(t)
			// Store.Put omits an empty references slice. Reload the persisted
			// scheme, as the UI does before sending its complete edit form.
			if u.References != nil || u.PromptInputDigest == "" {
				t.Fatal("expected persisted single-image scheme with a reviewed prompt")
			}
			digest := u.PromptInputDigest
			edited := "用户审阅并修改：沿用图片1的主角与中心构图，输出完整单张图片。"
			fields := map[string]any{
				"primary_asset_id": u.PrimaryAssetID,
				"feature_sources": map[string]string{
					"subject": u.PrimaryAssetID, "scene": "", "composition": u.PrimaryAssetID,
					"palette": "", "lighting": "", "texture": "", "text": "",
				},
				"output":    domain.ImageOutput{Ratio: "3:4", Clarity: "high", Count: 2},
				"text_mode": u.TextMode, "image_text": u.ImageText, "prompt": edited,
			}
			if sendReferences {
				fields["references"] = []domain.ImageReference{}
			}
			u = f.patch(t, u, fields)
			u = f.load(t, u.ID)
			if u.Prompt != edited || u.PromptInputDigest != digest || u.Status != "prompt_ready" {
				t.Fatalf("full form invalidated a reviewed prompt: prompt=%q digest=%q status=%s", u.Prompt, u.PromptInputDigest, u.Status)
			}
			if u.Output.Clarity != "high" || u.Output.Count != 2 || f.textCalls != 1 {
				t.Fatal("output-only changes should persist without composing another prompt")
			}
			// A subsequent refresh/save may omit blank feature entries. That
			// representation change must preserve the same generation basis.
			u = f.patch(t, u, map[string]any{
				"references":      []domain.ImageReference{},
				"feature_sources": map[string]string{"subject": u.PrimaryAssetID, "composition": u.PrimaryAssetID},
				"prompt":          edited,
			})
			u = f.load(t, u.ID)
			_, _, _, actualDigest, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
			if err != nil || actualDigest != digest || u.PromptInputDigest != digest || u.Prompt != edited {
				t.Fatal("empty and omitted fields changed the generation basis", err)
			}
			job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{
				"revision": u.Revision, "prompt": edited, "request_id": "reviewed-full-form",
			}, true))
			if job.State != "succeeded" {
				t.Fatal(job.Error)
			}
			if second := f.claim(t); second.State != "succeeded" {
				t.Fatal(second.Error)
			}
			if f.imageCalls != 2 {
				t.Fatal("updated count did not generate exactly two single-image requests")
			}
			for _, request := range f.submitted {
				if request["prompt"] != edited || request["size"] != "1536x2048" || request["n"] != float64(1) || request["prompt_extend"] != false || len(request["image"].([]any)) != 1 {
					t.Fatal("saved prompt or settings differ from the actual single-main-image request")
				}
			}
			u = f.load(t, u.ID)
			// A real upstream feature change must still require fresh review.
			u = f.patch(t, u, map[string]any{
				"references":      []domain.ImageReference{},
				"feature_sources": map[string]string{"subject": u.PrimaryAssetID},
				"prompt":          edited,
			})
			if u.PromptInputDigest != "" || u.Status != "analyzed" {
				t.Fatal("changed feature selection did not invalidate the reviewed prompt")
			}
			response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{
				"revision": u.Revision, "prompt": edited, "request_id": "stale-full-form",
			}, true)
			if response.Code != http.StatusConflict || f.imageCalls != 2 {
				t.Fatal("stale prompt triggered generation", response.Body.String())
			}
		})
	}
}

func TestImageV2RolesCacheCompositionAndSerialBatch(t *testing.T) {
	f := newImageV2Fixture(t)
	aux := f.xhsAsset
	aux.ID = "auxiliary-fixture"
	aux.Position = 1
	aux.Extraction.Text = "辅助图文字不能混入"
	_ = f.app.Store.Put("assets", aux.ID, aux)
	note := f.xhs
	note.AssetIDs = append(note.AssetIDs, aux.ID)
	_ = f.app.Store.Put("notes", note.ID, note)
	r := f.xhsRewrite
	r.Versions[0].AssetIDs = append(r.Versions[0].AssetIDs, aux.ID)
	_ = f.app.Store.Put("rewrites", r.ID, r)
	u := f.create(t)
	u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}, "prompt": ""})
	for i := 0; i < 2; i++ {
		j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", map[string]any{"revision": u.Revision}, true))
		if j.State != "succeeded" {
			t.Fatal(j.Error)
		}
		u = f.load(t, u.ID)
	}
	if f.visionCalls != 1 {
		t.Fatalf("same content should reuse analysis cache, calls=%d", f.visionCalls)
	}
	if u.ImageText != "" || u.TextMode != "none" {
		t.Fatal("visual analysis unexpectedly copied OCR into image requirements", u.ImageText)
	}
	u = f.patch(t, u, map[string]any{"feature_sources": map[string]string{"subject": f.xhsAsset.ID, "composition": f.xhsAsset.ID, "scene": aux.ID}, "text_mode": "keep", "image_text": "用户修正文字"})
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	u = f.load(t, u.ID)
	if strings.Contains(u.Prompt, "用户修正文字") || u.PromptInputDigest == "" || u.PromptRulesVersion != imagePromptRulesVersion {
		t.Fatal("legacy OCR unexpectedly entered the new composition")
	}
	digest := u.PromptInputDigest
	u = f.patch(t, u, map[string]any{"output": domain.ImageOutput{Ratio: "3:4", Clarity: "high", Count: 3}, "prompt": u.Prompt})
	if u.PromptInputDigest != digest || f.textCalls != 1 {
		t.Fatal("nonsemantic settings unnecessarily rebuilt prompt")
	}
	f.mode = "second-failed"
	body := map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "same-user-click"}
	response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", body, true)
	first := runVisualQueued(t, f.apiFixture, response)
	if first.State != "succeeded" {
		t.Fatal(first.Error)
	}
	if repeated := f.call(t, "POST", f.base+"/"+u.ID+"/generate", body, true); repeated.Code != 202 {
		t.Fatal("idempotent click rejected", repeated.Body.String())
	}
	second := f.claim(t)
	if second.State != "failed" {
		t.Fatal("known error should pause")
	}
	u = f.load(t, u.ID)
	if u.Batches[0].State != "paused" || len(u.Results) != 1 {
		t.Fatal("partial results lost")
	}
	f.app.imageV2EntityState(first)
	if queued, _ := f.app.Store.Claim("image_rewrite"); queued != nil {
		t.Fatal("replayed notification resumed paused batch")
	}
	resume := f.call(t, "POST", f.base+"/"+u.ID+"/batches/"+u.Batches[0].ID+"/resume", map[string]any{"action": "continue"}, true)
	if resume.Code != 202 {
		t.Fatal(resume.Body.String())
	}
	third := f.claim(t)
	if third.State != "succeeded" {
		t.Fatal(third.Error)
	}
	u = f.load(t, u.ID)
	if len(u.Results) != 2 || u.Batches[0].State != "complete" || f.imageCalls != 3 {
		t.Fatal("batch did not finish exactly once")
	}
	for _, request := range f.submitted {
		if request["prompt"] != u.Prompt || request["size"] != "1536x2048" || request["n"] != float64(1) || request["prompt_extend"] != false || len(request["image"].([]any)) != 2 {
			t.Fatal("actual payload differs from review")
		}
	}
	applied := decodeResponse[domain.Version](t, f.call(t, "POST", f.base+"/"+u.ID+"/apply", map[string]any{"asset_id": u.Results[0].AssetID, "version_id": r.Versions[0].ID}, true), 201)
	if !has(applied.AssetIDs, aux.ID) || has(applied.AssetIDs, f.xhsAsset.ID) || applied.Body != r.Versions[0].Body {
		t.Fatal("adoption modified auxiliary or text")
	}
}
func TestImageV2UnselectedReferenceAndStaleInputs(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	body := map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "one-image"}
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", body, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	if len(f.submitted[0]["image"].([]any)) != 1 {
		t.Fatal("unexpected reference sent")
	}
	u = f.load(t, u.ID)
	u = f.patch(t, u, map[string]any{"feature_sources": map[string]string{"subject": f.xhsAsset.ID}, "prompt": u.Prompt})
	if u.PromptInputDigest != "" {
		t.Fatal("feature change did not invalidate prompt")
	}
	if resp := f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "stale"}, true); resp.Code != 409 {
		t.Fatal("stale prompt accepted")
	}
	if resp := f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "primary_asset_id": f.dyAsset.ID}, true); resp.Code != 400 {
		t.Fatal("foreign image accepted")
	}
	otherProject := f.xhsRewrite
	otherProject.ID = "other-project-same-platform"
	otherProject.ImageRewrites = nil
	_ = f.app.Store.Put("rewrites", otherProject.ID, otherProject)
	private := f.xhsAsset
	private.ID = "other-private-image"
	private.OwnerID = otherProject.ID
	_ = f.app.Store.Put("assets", private.ID, private)
	if resp := f.call(t, "PATCH", f.base+"/"+u.ID, map[string]any{"revision": u.Revision, "primary_asset_id": private.ID}, true); resp.Code != 400 {
		t.Fatal("another project's private image accepted", resp.Code)
	}
	data := append([]byte{}, f.png...)
	data = append(data, ' ')
	_ = os.WriteFile(filepath.Join(f.app.Data, f.xhsAsset.LocalPath), data, 0600)
	if _, _, _, _, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u); err == nil {
		t.Fatal("changed image hash accepted")
	}
}

func TestImageV2AuxiliarySubjectNoOCRAndImmutableResultSources(t *testing.T) {
	f := newImageV2Fixture(t)
	aux := f.xhsAsset
	aux.ID = "anime-auxiliary"
	aux.OwnerID = f.xhsRewrite.ID
	aux.LocalPath = "assets/anime-auxiliary.png"
	auxData := append(append([]byte{}, f.png...), 0)
	if err := os.WriteFile(filepath.Join(f.app.Data, aux.LocalPath), auxData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("assets", aux.ID, aux); err != nil {
		t.Fatal(err)
	}
	u := f.create(t)
	u = f.patch(t, u, map[string]any{"references": []domain.ImageReference{{AssetID: aux.ID}}, "feature_sources": map[string]string{
		"subject": aux.ID, "scene": u.PrimaryAssetID, "composition": u.PrimaryAssetID,
		"palette": u.PrimaryAssetID, "lighting": u.PrimaryAssetID, "texture": u.PrimaryAssetID, "text": aux.ID,
	}, "text_mode": "pending", "image_text": "AI生成\nWORKBUDD>"})
	mainHash := sha256.Sum256(f.png)
	auxHash := sha256.Sum256(auxData)
	const selectedSubject = "蓝发猫耳少女身着女仆装，右手挥手，左手张开。"
	u.Analyses = []domain.ImageAnalysis{
		{AssetID: u.PrimaryAssetID, SHA256: hex.EncodeToString(mainHash[:]), RecognizedText: "AI生成\nWORKBUDD>", Features: domain.ImageFeatures{
			Subject: "被排除的书本主体", Scene: "纯色红棕背景，无其他背景元素", Composition: "居中且稍偏下，均匀留白", Palette: "红棕与白色线条", Lighting: "无投影平光", Texture: "扁平数字插画", Text: "AI生成 WORKBUDD>右下小字",
		}},
		{AssetID: aux.ID, SHA256: hex.EncodeToString(auxHash[:]), Features: domain.ImageFeatures{
			Subject: selectedSubject, Scene: "未选中的纯白背景", Palette: "未选中的蓝色配色", Text: "无可见文字",
		}},
	}
	var r domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &r)
	r.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", r.ID, r)
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" {
		t.Fatal("legacy pending text must not block the new workflow", j.Error)
	}
	u = f.load(t, u.ID)
	if f.visionCalls != 0 || len(f.composed) != 1 || !strings.Contains(u.Prompt, selectedSubject) || strings.Contains(u.Prompt, "WORKBUDD") {
		t.Fatal("new composition made OCR calls, omitted selected auxiliary subject, or copied old OCR")
	}
	raw, _ := json.Marshal(f.composed[0])
	for _, excluded := range []string{"confirmed_text", "WORKBUDD", "AI生成", "被排除的书本主体", "未选中的纯白背景", "未选中的蓝色配色"} {
		if strings.Contains(string(raw), excluded) {
			t.Fatal("unselected/OCR data leaked into composition", excluded)
		}
	}
	sources := f.composed[0]["feature_sources"].(map[string]any)
	if sources["subject"] != float64(2) || sources["scene"] != float64(1) || sources["text"] != float64(2) {
		t.Fatal("source numbering differs from actual image order")
	}
	var payload imageV2Payload
	_ = json.Unmarshal(j.Payload, &payload)
	if payload.PromptInput == nil || digestImageValue(payload.PromptInput) != digestImageValue(f.composed[0]) {
		t.Fatal("compose task did not persist its original input snapshot")
	}
	generated := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "auxiliary-subject"}, true))
	if generated.State != "succeeded" || f.imageCalls != 1 {
		t.Fatal("mock generation failed", generated.Error)
	}
	request := f.submitted[0]
	images := request["image"].([]any)
	if request["prompt"] != u.Prompt || request["prompt_extend"] != false || len(images) != 2 || images[0] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(f.png) || images[1] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(auxData) {
		t.Fatal("actual image model input differs from the reviewed prompt and main/aux order")
	}
	u = f.load(t, u.ID)
	if len(u.Results) != 1 || u.Results[0].FeatureSources["subject"] != aux.ID || u.Batches[0].FeatureSources["subject"] != aux.ID || u.Results[0].PromptRulesVersion != imagePromptRulesVersion {
		t.Fatal("result/batch did not preserve source mapping")
	}
	u = f.patch(t, u, map[string]any{"feature_sources": map[string]string{"subject": u.PrimaryAssetID}, "prompt": u.Prompt})
	if u.Results[0].FeatureSources["subject"] != aux.ID || u.Batches[0].FeatureSources["subject"] != aux.ID || !strings.Contains(u.Results[0].Prompt, selectedSubject) || u.PromptInputDigest != "" {
		t.Fatal("current edits changed historical provenance or retained a stale ready prompt")
	}
}

func TestImageV2InvalidCompositionStopsWithoutGenerationOrInputLoss(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	before := u
	f.mode = "compose-omits-subject"
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if j.State != "failed" || !strings.Contains(j.Error, "遗漏了选定主体") || f.textCalls != 2 || f.imageCalls != 0 {
		t.Fatal("invalid subject composition did not stop after one attempt", j.State, j.Error)
	}
	u = f.load(t, u.ID)
	if u.Prompt != before.Prompt || u.FeatureSources["subject"] != before.FeatureSources["subject"] || u.PrimaryAssetID != before.PrimaryAssetID || u.Revision != before.Revision {
		t.Fatal("failed composition lost the saved inputs or replaced the prior prompt")
	}
}

func TestImageV2LegacyTypographyDoesNotReintroduceOCR(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	u = f.patch(t, u, map[string]any{"feature_sources": map[string]string{"subject": u.PrimaryAssetID, "text": u.PrimaryAssetID}})
	u.Analyses[0].Features.Text = "右下角两行小字：上行为中文‘AI生成’（无衬线体），下行为英文“WORKBUDD>”（等宽字体），右对齐，字号小、层级低。另有 SIGNATURE 水印。"
	u.Analyses[0].RecognizedText = "AI生成\nWORKBUDD>\nSIGNATURE"
	var r domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &r)
	r.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", r.ID, r)
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/compose-prompt", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	raw, _ := json.Marshal(f.composed[len(f.composed)-1])
	for _, excluded := range []string{"AI生成", "WORKBUDD", "SIGNATURE"} {
		if strings.Contains(string(raw), excluded) {
			t.Fatal("literal OCR was reintroduced through legacy typography", excluded)
		}
	}
	if !strings.Contains(string(raw), "右对齐") || !strings.Contains(string(raw), "无衬线体") || !strings.Contains(string(raw), "等宽字体") {
		t.Fatal("stripping OCR discarded usable typography")
	}
}

func TestImageV2AnalysisDoesNotMakeAnOCRRequest(t *testing.T) {
	f := newImageV2Fixture(t)
	asset := f.xhsAsset
	asset.Extraction = domain.Extraction{}
	_ = f.app.Store.Put("assets", asset.ID, asset)
	u := f.create(t)
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/analyze", map[string]any{"revision": u.Revision}, true))
	if j.State != "succeeded" || f.visionCalls != 1 {
		t.Fatal("visual analysis unexpectedly made a second OCR model request", j.Error, f.visionCalls)
	}
	u = f.load(t, u.ID)
	if u.ImageText != "" || u.TextMode != "none" {
		t.Fatal("analysis restored the removed text-confirmation module")
	}
}

func TestImageV2LegacyLongOCRDoesNotBlockOrInvalidateCurrentPrompt(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	before := u.PromptInputDigest
	u = f.patch(t, u, map[string]any{"text_mode": "keep", "image_text": strings.Repeat("旧识别文字", 1000)})
	if u.PromptInputDigest != before || u.PromptRulesVersion != imagePromptRulesVersion {
		t.Fatal("unused old OCR controls invalidated the current image prompt")
	}
	_, _, _, actualDigest, err := f.app.imageV2PromptInput(f.xhsRewrite.ID, u)
	if err != nil || actualDigest != before {
		t.Fatal("long unused OCR blocked prompt planning", err)
	}
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "long-legacy-ocr"}, true))
	if j.State != "succeeded" || f.imageCalls != 1 {
		t.Fatal("unused legacy OCR blocked reviewed mock generation", j.Error)
	}
}

func TestImageV2OldPromptRulesRequireRecompositionWithoutChangingHistory(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	u.PromptRulesVersion = 0
	u.Results = append(u.Results, domain.ImageResult{AssetID: "old-result", Prompt: "旧提示词与旧图中文字", Revision: 1})
	var r domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &r)
	r.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", r.ID, r)
	response := f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "old-rules"}, true)
	if response.Code != http.StatusConflict || f.imageCalls != 0 || f.textCalls != 1 {
		t.Fatal("old prompt was submitted or automatically recomposed", response.Code)
	}
	fresh := f.load(t, u.ID)
	if fresh.Prompt != u.Prompt || fresh.Results[0].Prompt != "旧提示词与旧图中文字" || fresh.Revision != u.Revision {
		t.Fatal("old-prompt rejection rewrote existing data")
	}
}
func TestImageV2UnknownNeverResubmitsAndReceiptRestores(t *testing.T) {
	for _, mode := range []string{"unknown", "receipt"} {
		t.Run(mode, func(t *testing.T) {
			f := newImageV2Fixture(t)
			u := f.prepare(t)
			u = f.patch(t, u, map[string]any{"output": domain.ImageOutput{Ratio: "3:4", Clarity: "standard", Count: 2}})
			f.mode = mode
			j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "unknown-test"}, true))
			if j.State != "submission_unknown" {
				t.Fatalf("wanted unknown, got %s %s", j.State, j.Error)
			}
			u = f.load(t, u.ID)
			if next, _ := f.app.Store.Claim("image_rewrite"); next != nil {
				t.Fatal("remaining batch was not paused")
			}
			if resp := f.call(t, "POST", "/jobs/"+j.ID+"/retry", nil, true); resp.Code != 409 {
				t.Fatal("generic retry enabled paid repeat")
			}
			endpoint := f.base + "/" + u.ID + "/batches/" + u.Batches[0].ID + "/resume"
			if resp := f.call(t, "POST", endpoint, map[string]any{"action": "retry"}, true); resp.Code != 409 {
				t.Fatal("unknown blindly retried")
			}
			if mode == "receipt" {
				j.State = "running"
				_ = f.app.Store.Put("jobs", j.ID, j)
				if err := f.app.Store.Recover(); err != nil {
					t.Fatal(err)
				}
				_ = f.app.Store.Get("jobs", j.ID, &j)
				f.app.recoverImageV2(&j)
				if j.State != "queued" {
					t.Fatal("receipt not recovered")
				}
				f.app.runJob(&j)
				if j.State != "succeeded" || f.imageCalls != 1 || f.downloads != 2 {
					t.Fatal("restore repeated paid POST", f.imageCalls, f.downloads, j.Error)
				}
			} else {
				f.app.recoverImageV2(&j)
				if j.State != "submission_unknown" || f.imageCalls != 1 {
					t.Fatal("unknown task automatically retried")
				}
			}
		})
	}
}

func TestImageV2ConfirmedUnknownEndsWithoutLeavingLock(t *testing.T) {
	for _, action := range []string{"cancel", "continue"} {
		t.Run(action, func(t *testing.T) {
			f := newImageV2Fixture(t)
			u := f.prepare(t)
			f.mode = "unknown"
			j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "unknown-confirm"}, true))
			u = f.load(t, u.ID)
			endpoint := f.base + "/" + u.ID + "/batches/" + u.Batches[0].ID + "/resume"
			if resp := f.call(t, "POST", endpoint, map[string]any{"action": action, "confirm_no_result": true}, true); resp.Code != 200 {
				t.Fatal(resp.Body.String())
			}
			_ = f.app.Store.Get("jobs", j.ID, &j)
			if j.State != "failed" || j.Phase != "verified_no_result" || f.app.imageRewriteBusy(f.xhsRewrite.ID, u.ID) {
				t.Fatal("confirmed no-result left an unresolved lock", j.State, j.Phase)
			}
			f.app.imageV2EntityState(j)
			if f.app.imageRewriteBusy(f.xhsRewrite.ID, u.ID) {
				t.Fatal("startup replay relocked skipped item")
			}
			u = f.load(t, u.ID)
			_ = f.patch(t, u, map[string]any{"output": domain.ImageOutput{Ratio: "3:4", Clarity: "high", Count: 1}})
			if f.imageCalls != 1 {
				t.Fatal("ending batch made another paid request")
			}
		})
	}
}

func TestImageV2DeletionProtectsBatchAndResultInputs(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	asset := f.xhsAsset
	asset.ID = "history-input-only"
	asset.OwnerID = f.xhsRewrite.ID
	_ = f.app.Store.Put("assets", asset.ID, asset)
	var r domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &r)
	u.Results = append(u.Results, domain.ImageResult{AssetID: "result-only", PrimaryAssetID: asset.ID, SourceAssetIDs: []string{asset.ID}})
	u.Batches = append(u.Batches, domain.ImageBatch{InputAssetIDs: []string{asset.ID}})
	r.ImageRewrites[0] = u
	_ = f.app.Store.Put("rewrites", r.ID, r)
	resp := f.call(t, "DELETE", "/assets/"+asset.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if resp.Code != 409 {
		t.Fatal("historical reference was deleted", resp.Code, resp.Body.String())
	}
}

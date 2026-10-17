package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestImagePromptReadableRequirementsAndExplicitImageText(t *testing.T) {
	item := domain.ImageRewrite{
		SourceAssetIDs: []string{"private-reference-id"},
		Style:          domain.ImageStyle{Preset: "写实摄影", Ratio: "3:4", Palette: "蓝白", Retain: []string{"subject"}},
		Analyses:       []domain.ImageAnalysis{{Features: domain.ImageFeatures{Subject: "书桌", Palette: "红色", Composition: "参考图构图"}}},
	}
	version := domain.Version{Title: "学习计划", Body: "正文首段\n" + strings.Repeat("内容", 150) + "正文后段"}
	prompt := imagePrompt(item, version)
	for _, required := range []string{"学习计划", "正文后段", "主体与动作：书桌", "目标风格：写实摄影", "配色：蓝白", "不添加文字", "画幅：3:4"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing visible requirement %q", required)
		}
	}
	for _, forbidden := range []string{"private-reference-id", `"new_content"`, "\\n", "色彩：红色", "参考图构图"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("unexpected metadata or unselected feature %q", forbidden)
		}
	}
	item.Brief, item.ImageText, item.StyleReferenceID = "画一只窗边读书的小猫", "今天也要读书", "style-snapshot"
	prompt = imagePrompt(item, version)
	if !strings.Contains(prompt, item.Brief) || !strings.Contains(prompt, item.ImageText) || !strings.Contains(prompt, "最后一张参考图") || strings.Contains(prompt, version.Body) || strings.Contains(prompt, "不添加文字") {
		t.Fatal("explicit image content was overridden by copy background")
	}
}

func TestImageRequirementsSaveRebuildsPromptAndGenerationBindsPreview(t *testing.T) {
	f := seedWorkspaceFixture(t)
	r := f.xhsRewrite
	unit := domain.ImageRewrite{ID: "prompt-unit", VersionID: r.Versions[0].ID, SourceAssetIDs: []string{f.xhsAsset.ID},
		Analyses: []domain.ImageAnalysis{{AssetID: f.xhsAsset.ID, Features: domain.ImageFeatures{Subject: "书桌", Composition: "居中"}}},
		Style:    domain.ImageStyle{Preset: "水彩手绘", Ratio: "3:4", Retain: []string{"subject"}}, Prompt: "旧的水彩提示词", Revision: 2, Status: "ready",
		Results: []domain.ImageResult{{AssetID: "old-image", Revision: 2, Prompt: "旧的水彩提示词"}},
	}
	r.ImageRewrites = []domain.ImageRewrite{unit}
	_ = f.app.Store.Put("rewrites", r.ID, r)
	path := "/rewrites/" + r.ID + "/images/" + unit.ID
	posts := 0
	var submitted struct {
		Prompt string   `json:"prompt"`
		Image  []string `json:"image"`
		Size   string   `json:"size"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		posts++
		if err := json.NewDecoder(req.Body).Decode(&submitted); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(f.png)}}})
	}))
	defer server.Close()
	_ = f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{"image": {BaseURL: server.URL, APIKey: "fixture-key", Model: "qwen-image-3.0-pro"}}})
	unit.Style.Preset, unit.Style.Palette = "写实摄影", "蓝白色"
	patch := map[string]any{"revision": unit.Revision, "style": unit.Style, "analyses": unit.Analyses, "brief": "真实书桌摄影", "image_text": "", "prompt": unit.Prompt}
	// Old clients saving style with an unchanged prompt must not retain stale text.
	updated := decodeResponse[domain.ImageRewrite](t, f.call(t, "PATCH", path+"?platform=xiaohongshu", patch, true), 200)
	if updated.Prompt == unit.Prompt || !strings.Contains(updated.Prompt, "写实摄影") || !strings.Contains(updated.Prompt, "真实书桌摄影") || len(updated.Results) != 1 || updated.Results[0].Prompt != unit.Prompt {
		t.Fatal("requirements and saved prompt diverged, or history was rewritten")
	}
	jobsBefore := len(list[domain.Job](f.app, "jobs"))
	for _, expected := range []map[string]any{{"revision": unit.Revision, "prompt": unit.Prompt}, {"revision": updated.Revision, "prompt": "另一份提示词"}} {
		if response := f.call(t, "POST", path+"/generate?platform=xiaohongshu", expected, true); response.Code != 409 {
			t.Fatal("stale preview accepted", response.Code)
		}
	}
	if posts != 0 || len(list[domain.Job](f.app, "jobs")) != jobsBefore {
		t.Fatal("invalid preview reached paid generation")
	}
	patch["revision"], patch["prompt"], patch["style"] = updated.Revision, "用户手写的新提示词", updated.Style
	patch["brief"] = "同时修改画面要求"
	if response := f.call(t, "PATCH", path+"?platform=xiaohongshu", patch, true); response.Code != 409 {
		t.Fatal("conflicting prompt and requirements silently accepted")
	}
	job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", path+"/generate?platform=xiaohongshu", map[string]any{"revision": updated.Revision, "prompt": updated.Prompt}, true))
	if job.State != "succeeded" || posts != 1 || submitted.Prompt != updated.Prompt || submitted.Size != "960x1280" || len(submitted.Image) != 1 || !strings.HasSuffix(submitted.Image[0], base64.StdEncoding.EncodeToString(f.png)) {
		t.Fatalf("submitted request differs from confirmed preview: state=%s posts=%d", job.State, posts)
	}
	_ = f.app.Store.Get("rewrites", r.ID, &r)
	result := r.ImageRewrites[0].Results[1]
	if result.Prompt != submitted.Prompt || result.Revision != updated.Revision || result.Ratio != "3:4" || len(result.SourceAssetIDs) != 1 || result.SourceAssetIDs[0] != f.xhsAsset.ID {
		t.Fatal("result lacks exact request provenance")
	}
}

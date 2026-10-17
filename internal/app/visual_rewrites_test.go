package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func runVisualQueued(t *testing.T, f apiFixture, response *httptest.ResponseRecorder) domain.Job {
	t.Helper()
	result := decodeResponse[struct {
		ID string `json:"job_id"`
	}](t, response, 202)
	var j domain.Job
	if err := f.app.Store.Get("jobs", result.ID, &j); err != nil {
		t.Fatal(err)
	}
	j.State = "running"
	f.app.runJob(&j)
	return j
}

func TestVisualDeletionPreservesImagesReferencedBySurvivingFeatureAndPlanHistory(t *testing.T) {
	f := seedWorkspaceFixture(t)
	otherNote := f.xhs
	otherNote.ID, otherNote.AssetIDs = "surviving-source", []string{}
	_ = f.app.Store.Put("notes", otherNote.ID, otherNote)
	result := f.xhsAsset
	result.ID, result.OwnerID, result.LocalPath = "visual-history-image", f.xhsRewrite.ID, "assets/visual-history.png"
	_ = os.WriteFile(filepath.Join(f.app.Data, result.LocalPath), f.png, 0600)
	_ = f.app.Store.Put("assets", result.ID, result)
	other := domain.Rewrite{ID: "surviving-visual", NoteID: otherNote.ID, Versions: []domain.Version{}, ImageRewrites: []domain.ImageRewrite{{ID: "surviving-unit", SourceAssetIDs: []string{f.xhsAsset.ID}, StyleReferenceID: f.xhsAsset.ID, Results: []domain.ImageResult{{AssetID: result.ID}}}}}
	_ = f.app.Store.Put("rewrites", other.ID, other)
	plan := domain.VideoProject{ID: "surviving-plan", RewriteID: other.ID, PlanHistory: []domain.ImageVoicePlan{{Options: domain.ImageVoiceOptions{ImageAssetIDs: []string{result.ID}}}}}
	_ = f.app.Store.Put("videos", plan.ID, plan)
	deleted := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	if deleted.RetainedAssets != 2 {
		t.Fatalf("visual references not retained: %+v", deleted)
	}
	for _, id := range []string{f.xhsAsset.ID, result.ID} {
		var asset domain.Asset
		if err := f.app.Store.Get("assets", id, &asset); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.assetPath(asset); err != nil {
			t.Fatal("retained visual file missing", err)
		}
		platform, ok := f.app.recordWorkspacePlatform("assets", id, map[string]bool{})
		if !ok || platform != "xiaohongshu" {
			t.Fatal("retained visual image lost workspace lineage")
		}
	}
}

func TestVisualRewriteFeatureGenerationVersionBindingAndIsolation(t *testing.T) {
	f := seedWorkspaceFixture(t)
	imagePosts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/generations" {
			imagePosts++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(f.png)}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"subject":"书桌","action":"静置","scene":"室内","composition":"居中","palette":"白色","lighting":"自然光","texture":"手绘","text":"无"}`}}}})
	}))
	defer server.Close()
	if err := f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{
		"vision": {BaseURL: server.URL, APIKey: "fixture-key", Model: "qwen3-vl-plus"},
		"image":  {BaseURL: server.URL, APIKey: "fixture-key", Model: "qwen-image-3.0-pro"},
	}}); err != nil {
		t.Fatal(err)
	}
	base := "/rewrites/" + f.xhsRewrite.ID + "/images"
	body := map[string]any{"version_id": f.xhsRewrite.ConfirmedVersionID, "source_asset_ids": []string{f.xhsAsset.ID}}
	// The retired Douyin workspace is rejected before any handler runs.
	if resp := f.call(t, "POST", base+"?platform=douyin", body, true); resp.Code != 400 {
		t.Fatal("cross-workspace image creation accepted")
	}
	body["source_asset_ids"] = []string{f.dyAsset.ID}
	if resp := f.call(t, "POST", base+"?platform=xiaohongshu", body, true); resp.Code != 400 && resp.Code != 404 {
		t.Fatalf("foreign reference accepted: %d %s", resp.Code, resp.Body.String())
	}
	body["source_asset_ids"] = []string{f.xhsAsset.ID}
	unit := decodeResponse[domain.ImageRewrite](t, f.call(t, "POST", base+"?platform=xiaohongshu", body, true), 201)
	unitPath := base + "/" + unit.ID
	load := func() domain.Rewrite {
		var r domain.Rewrite
		if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", unitPath+"/analyze?platform=xiaohongshu", map[string]any{}, true))
	if j.State != "succeeded" {
		t.Fatalf("analysis: %+v", j)
	}
	r := load()
	unit = r.ImageRewrites[0]
	if unit.Revision != 2 || len(unit.Analyses) != 1 || unit.Analyses[0].Features.Subject != "书桌" || !strings.Contains(unit.Prompt, "确认标题") {
		t.Fatalf("analysis not saved: %+v", unit)
	}
	j = runVisualQueued(t, f.apiFixture, f.call(t, "POST", unitPath+"/generate?platform=xiaohongshu", map[string]any{}, true))
	if j.State != "succeeded" || imagePosts != 1 {
		t.Fatalf("generation: %+v", j)
	}
	r = load()
	unit = r.ImageRewrites[0]
	if len(unit.Results) != 1 || unit.Results[0].Revision != 2 || unit.Status != "ready" {
		t.Fatalf("generated history: %+v", unit)
	}
	apply := map[string]string{"asset_id": unit.Results[0].AssetID, "version_id": f.xhsRewrite.ConfirmedVersionID}
	v := decodeResponse[domain.Version](t, f.call(t, "POST", unitPath+"/apply?platform=xiaohongshu", apply, true), 201)
	if v.Confirmed || v.ID == f.xhsRewrite.ConfirmedVersionID || v.ParentVersionID != f.xhsRewrite.ConfirmedVersionID || v.Title != f.xhsRewrite.Versions[0].Title || !reflect.DeepEqual(v.AssetIDs, []string{unit.Results[0].AssetID}) {
		t.Fatalf("image application damaged version: %+v", v)
	}
	again := decodeResponse[domain.Version](t, f.call(t, "POST", unitPath+"/apply?platform=xiaohongshu", apply, true), 200)
	if again.ID != v.ID || len(load().Versions) != 2 {
		t.Fatal("apply is not idempotent")
	}
	var source domain.Note
	if err := f.app.Store.Get("notes", f.xhs.ID, &source); err != nil || !reflect.DeepEqual(source.AssetIDs, f.xhs.AssetIDs) || source.Body != f.xhs.Body {
		t.Fatal("original collection modified")
	}
	r = load()
	changed := v
	changed.ID = "changed-copy"
	changed.Body = "用户后来修改的正文"
	r.Versions = append(r.Versions, changed)
	_ = f.app.Store.Put("rewrites", r.ID, r)
	apply["version_id"] = changed.ID
	changedImage := decodeResponse[domain.Version](t, f.call(t, "POST", unitPath+"/apply?platform=xiaohongshu", apply, true), 200)
	if changedImage.Body != changed.Body {
		t.Fatal("image application overwrote independent copy edits")
	}
	unit.Style.Custom = "修改后的风格"
	unit = decodeResponse[domain.ImageRewrite](t, f.call(t, "PATCH", unitPath+"?platform=xiaohongshu", map[string]any{"revision": unit.Revision, "analyses": unit.Analyses, "style": unit.Style, "rebuild_prompt": true}, true), 200)
	apply["version_id"] = v.ID
	if resp := f.call(t, "POST", unitPath+"/apply?platform=xiaohongshu", apply, true); resp.Code != 409 {
		t.Fatal("stale result applied to new image revision")
	}
	if len(unit.Results) != 1 {
		t.Fatal("style change deleted previous results")
	}
}

func TestVisualRewriteUnknownSubmissionAndLocalRecovery(t *testing.T) {
	f := seedWorkspaceFixture(t)
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.WriteHeader(503)
		_, _ = w.Write([]byte("unavailable"))
	}))
	defer server.Close()
	_ = f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{"image": {BaseURL: server.URL, APIKey: "fixture-key", Model: "qwen-image-3.0-pro"}}})
	unit := domain.ImageRewrite{ID: "unknown-image", VersionID: f.xhsRewrite.ConfirmedVersionID, SourceAssetIDs: []string{f.xhsAsset.ID}, Analyses: []domain.ImageAnalysis{{AssetID: f.xhsAsset.ID, Features: domain.ImageFeatures{Subject: "桌面", Composition: "居中"}}}, Prompt: "桌面的新配图", Style: domain.ImageStyle{Ratio: "3:4"}, Revision: 1, Status: "analyzed", Results: []domain.ImageResult{}}
	r := f.xhsRewrite
	r.ImageRewrites = []domain.ImageRewrite{unit}
	_ = f.app.Store.Put("rewrites", r.ID, r)
	path := "/rewrites/" + r.ID + "/images/" + unit.ID
	j := runVisualQueued(t, f.apiFixture, f.call(t, "POST", path+"/generate?platform=xiaohongshu", map[string]any{}, true))
	if j.State != "submission_unknown" || posts != 1 {
		t.Fatalf("unknown submission: %+v count=%d", j, posts)
	}
	for _, target := range []string{path + "/generate?platform=xiaohongshu", "/jobs/" + j.ID + "/retry?platform=xiaohongshu"} {
		if resp := f.call(t, "POST", target, map[string]any{}, true); resp.Code != 409 {
			t.Fatalf("unknown POST retried: %s %d", target, resp.Code)
		}
	}
	if resp := f.call(t, "POST", path+"/resolve?platform=xiaohongshu", map[string]any{}, true); resp.Code != 400 {
		t.Fatal("resolved without explicit verification")
	}
	decodeResponse[domain.ImageRewrite](t, f.call(t, "POST", path+"/resolve?platform=xiaohongshu", map[string]any{"confirm_no_result": true}, true), 200)
	_ = f.app.Store.Get("jobs", j.ID, &j)
	if j.State != "failed" || j.Phase != "verified_no_result" || posts != 1 {
		t.Fatal("verification retried paid request")
	}
	// A crash after the entity result was persisted must recover without a POST.
	asset := f.xhsAsset
	asset.ID = "saved-generated-image"
	asset.OwnerID = r.ID
	_ = f.app.Store.Put("assets", asset.ID, asset)
	_ = f.app.Store.Get("rewrites", r.ID, &r)
	r.ImageRewrites[0].Results = append(r.ImageRewrites[0].Results, domain.ImageResult{JobID: j.ID, AssetID: asset.ID, Revision: 1})
	_ = f.app.Store.Put("rewrites", r.ID, r)
	j.State = "submission_unknown"
	j.Phase = "submitting"
	j.Error = "interrupted"
	_ = f.app.Store.Put("jobs", j.ID, j)
	f.app.recoverImageRewriteResult(&j)
	if j.State != "queued" || j.Phase != "image_saved" || len(j.ResultAssetIDs) != 1 {
		t.Fatalf("local recovery: %+v", j)
	}
	f.app.runJob(&j)
	_ = f.app.Store.Get("rewrites", r.ID, &r)
	if j.State != "succeeded" || r.ImageRewrites[0].Status != "ready" || posts != 1 {
		t.Fatalf("saved result resubmitted or stuck: %+v %+v", j, r.ImageRewrites[0])
	}
}

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestVideoRewriteReferencesCoverCurrentAndImmutableHistory(t *testing.T) {
	snapshot := domain.VideoGenerationSnapshot{PrimaryAssetID: "historical-main", InputAssetIDs: []string{"input-video", "input-image"}, InputHashes: map[string]string{"hashed-input": "sha"}, PurposeSources: map[string]string{"style": "style-source"}}
	v := domain.VideoProject{VideoRewrite: &domain.VideoRewriteState{
		PrimaryAssetID: "current-main", ReferenceVideoID: "current-reference", ReferenceImageID: "current-image",
		Analyses:    []domain.VideoMaterialAnalysis{{AssetID: "analysis-input"}},
		Submissions: []domain.VideoSubmission{{Snapshot: snapshot}},
		Results:     []domain.VideoRewriteResult{{AssetID: "result", PreviewAssetID: "preview", CoverAssetID: "cover", Snapshot: snapshot}},
	}}
	refs := videoAssetRefs(v)
	for _, id := range []string{"current-main", "current-reference", "current-image", "analysis-input", "historical-main", "input-video", "input-image", "hashed-input", "style-source", "result", "preview", "cover"} {
		if !has(refs, id) {
			t.Fatalf("missing protected video reference %s", id)
		}
	}
	payload, _ := json.Marshal(map[string]any{"snapshot": snapshot})
	for _, kind := range []string{"video_script", "video_rewrite"} {
		refs = jobAssetRefs(domain.Job{Kind: kind, Payload: payload})
		for _, id := range []string{"historical-main", "input-video", "input-image", "hashed-input", "style-source"} {
			if !has(refs, id) {
				t.Fatalf("%s task lost input reference %s", kind, id)
			}
		}
	}
}

func TestVideoRewriteLibraryDoesNotChangePublishingSelection(t *testing.T) {
	f := seedWorkspaceFixture(t)
	result := f.xhsAsset
	result.ID, result.OwnerID, result.Kind, result.MIME = "generated-video-v2", f.xhsVideo.ID, "video", "video/mp4"
	result.SourceID, result.LocalPath = "video_rewrite", "assets/generated-video-v2.mp4"
	if err := os.WriteFile(filepath.Join(f.app.Data, result.LocalPath), f.png, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("assets", result.ID, result); err != nil {
		t.Fatal(err)
	}
	f.xhsVideo.VideoRewrite = &domain.VideoRewriteState{Results: []domain.VideoRewriteResult{{AssetID: result.ID, Snapshot: domain.VideoGenerationSnapshot{Revision: 1, PrimaryAssetID: f.xhsAsset.ID, InputAssetIDs: []string{f.xhsAsset.ID}}, CreatedAt: now()}}}
	if err := f.app.Store.Put("videos", f.xhsVideo.ID, f.xhsVideo); err != nil {
		t.Fatal(err)
	}
	items := decodeResponse[struct {
		Items []mediaLibraryItem `json:"items"`
	}](t, f.call(t, "GET", "/media-library?rewrite_id="+f.xhsRewrite.ID+"&platform=xiaohongshu", nil, false), 200)
	if len(items.Items) != 1 || items.Items[0].Origin != "video_rewrite" || items.Items[0].ID != result.ID {
		t.Fatal("new output missing from project library", items)
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, f.xhsRewrite) {
		t.Fatal("library indexing changed publishing choices")
	}
	response := f.call(t, "DELETE", "/assets/"+result.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if response.Code != 409 {
		t.Fatal("physical deletion ignored video history", response.Code)
	}
	response = f.call(t, "DELETE", "/rewrites/"+f.xhsRewrite.ID+"/media-library/"+result.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(f.app.Data, result.LocalPath)); err != nil {
		t.Fatal("library removal deleted referenced file", err)
	}
	if err := f.app.Store.Get("videos", f.xhsVideo.ID, &f.xhsVideo); err != nil || len(f.xhsVideo.VideoRewrite.Results) != 1 {
		t.Fatal("library removal erased video history", err)
	}
	response = f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]string{"rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID, "video_project_id": f.xhsVideo.ID}, true)
	if response.Code != 409 {
		t.Fatal("unadopted result bypassed normal publishing selection", response.Code)
	}
}

func TestVideoRewriteJobListHidesSignedInputLinks(t *testing.T) {
	f := seedWorkspaceFixture(t)
	payload, _ := json.Marshal(map[string]any{"snapshot": domain.VideoGenerationSnapshot{VideoURLs: map[string]string{"input": "https://example.com/clip.mp4?private-signature=secret"}}})
	job := domain.Job{ID: "video-rewrite-private", Kind: "video_rewrite", EntityID: f.xhsVideo.ID, State: "failed", Payload: payload}
	if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	response := f.call(t, "GET", "/jobs?platform=xiaohongshu", nil, false)
	if strings.Contains(response.Body.String(), "private-signature") || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("job list leaked signed input URL")
	}
}

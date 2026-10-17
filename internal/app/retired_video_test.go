package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestRetiredVideoRoutesCannotCreateEditGenerateOrPublishLegacyProjects(t *testing.T) {
	f := seedWorkspaceFixture(t)
	v, r := f.xhsVideo, f.xhsRewrite
	for _, request := range []struct {
		method, path string
		body         any
		status       int
	}{
		{"POST", "/video-projects", map[string]string{"rewrite_id": r.ID, "version_id": r.ConfirmedVersionID, "workflow": "image_voice", "script": "旧口播"}, 410},
		{"POST", "/video-projects", map[string]string{"rewrite_id": r.ID, "version_id": r.ConfirmedVersionID}, 410},
		{"GET", "/video-projects/" + v.ID, nil, 410},
		{"PATCH", "/video-projects/" + v.ID, map[string]any{"scenes": []domain.Scene{}}, 410},
		{"POST", "/video-projects/" + v.ID + "/generate", map[string]bool{"force": true}, 410},
		{"POST", "/video-projects/" + v.ID + "/plan", map[string]int{"scene_count": 1}, 404},
		{"POST", "/video-projects/" + v.ID + "/render", map[string]any{}, 404},
		{"POST", "/publications", map[string]string{"rewrite_id": r.ID, "version_id": r.ConfirmedVersionID, "video_project_id": v.ID}, 409},
	} {
		response := f.call(t, request.method, request.path+"?platform=xiaohongshu", request.body, true)
		if response.Code != request.status {
			t.Fatalf("%s %s: %d, want %d: %s", request.method, request.path, response.Code, request.status, response.Body.String())
		}
	}
	// The retired Douyin workspace is rejected before any handler runs.
	for _, request := range []struct {
		method, path string
	}{
		{"POST", "/video-projects"},
		{"GET", "/video-projects/" + f.dyVideo.ID},
		{"PATCH", "/video-projects/" + f.dyVideo.ID},
		{"POST", "/video-projects/" + f.dyVideo.ID + "/generate"},
		{"POST", "/publications"},
	} {
		if response := f.call(t, request.method, request.path+"?platform=douyin", map[string]bool{"force": true}, true); response.Code != 400 {
			t.Fatalf("%s %s?platform=douyin: %d, want 400", request.method, request.path, response.Code)
		}
	}
	assertWorkspaceIDs(t, f.apiFixture, "/video-projects?platform=xiaohongshu")
	created := decodeResponse[domain.VideoProject](t, f.call(t, "POST", "/video-projects?platform=xiaohongshu", map[string]string{"rewrite_id": r.ID, "version_id": r.ConfirmedVersionID, "workflow": videoRewriteWorkflow}, true), 201)
	if created.Workflow != videoRewriteWorkflow || created.VideoRewrite == nil {
		t.Fatal("new workflow was removed with legacy flow")
	}
	assertWorkspaceIDs(t, f.apiFixture, "/video-projects?platform=xiaohongshu", created.ID)
	for _, archived := range []domain.VideoProject{f.xhsVideo, f.dyVideo} {
		var saved domain.VideoProject
		if err := f.app.Store.Get("videos", archived.ID, &saved); err != nil || !reflect.DeepEqual(saved, archived) {
			t.Fatal("retired route mutated archived data", err)
		}
	}
	// Retiring the workflow must not delete files used by saved publications.
	if data, err := os.ReadFile(filepath.Join(f.app.Data, f.xhsAsset.LocalPath)); err != nil || !reflect.DeepEqual(data, f.png) {
		t.Fatal("existing media was deleted", err)
	}
	publication := decodeResponse[domain.Publication](t, f.call(t, "GET", "/publications/"+f.xhsPub.ID+"?platform=xiaohongshu", nil, false), 200)
	if publication.Body != f.xhsPub.Body || !reflect.DeepEqual(publication.AssetIDs, f.xhsPub.AssetIDs) {
		t.Fatal("saved publication changed")
	}
}

func TestRetiredVideoJobsNeverExecuteOrRetryAndKeepUnknownReceipts(t *testing.T) {
	f := seedWorkspaceFixture(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(500) }))
	defer server.Close()
	s, _ := f.app.settings()
	for _, key := range []string{"text", "image", "video"} {
		s.Providers[key] = domain.ProviderConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture"}
	}
	if err := f.app.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"image_voice", "image_plan", "storyboard", "video", "video_generate", "render"} {
		for _, state := range []string{"queued", "running", "waiting_config", "submission_unknown"} {
			job := domain.Job{ID: kind + "-" + state, Kind: kind, EntityID: f.xhsVideo.ID, State: state, RemoteTaskID: "saved-receipt", ResultAssetIDs: []string{f.xhsAsset.ID}}
			if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
				t.Fatal(err)
			}
			f.app.runJob(&job)
			wantState := "cancelled"
			if state == "submission_unknown" {
				wantState = state
			}
			if job.State != wantState || job.RemoteTaskID != "saved-receipt" || len(job.ResultAssetIDs) != 1 {
				t.Fatal("retirement lost receipt or did not stop work", job)
			}
			response := f.call(t, "POST", "/jobs/"+job.ID+"/retry?platform=xiaohongshu", map[string]string{"remote_task_id": "new-id"}, true)
			if response.Code != 410 {
				t.Fatalf("retired job retry returned %d", response.Code)
			}
		}
		if _, err := f.app.enqueue(kind, f.xhsVideo.ID, "", "", nil); err == nil {
			t.Fatal("retired task enqueued", kind)
		}
	}
	if requests != 0 {
		t.Fatal("retired tasks called a provider", requests)
	}
	settings := decodeResponse[domain.Settings](t, f.call(t, "GET", "/settings?platform=xiaohongshu", nil, false), 200)
	if _, ok := settings.Providers["tts"]; ok {
		t.Fatal("retired speech configuration remains exposed")
	}
	if response := f.call(t, "POST", "/settings/test?platform=xiaohongshu", map[string]string{"capability": "tts"}, true); response.Code != 400 {
		t.Fatal("retired speech test remains callable", response.Code)
	}
}

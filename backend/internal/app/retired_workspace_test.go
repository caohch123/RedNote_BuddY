package app

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"reflect"
	"testing"
	"xhs-workbench/internal/domain"
)

func TestRetiredWorkspaceHiddenWithoutScope(t *testing.T) {
	f := seedWorkspaceFixture(t)
	assertWorkspaceIDs(t, f.apiFixture, "/assets", f.xhsAsset.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/jobs", "job-xhs", "job-legacy")
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/collections/" + f.dy.ID, nil},
		{"PATCH", "/collections/" + f.dy.ID, map[string]any{"tags": []string{"changed"}}},
		{"POST", "/collections/" + f.dy.ID + "/recollect", nil},
		{"GET", "/rewrites/" + f.dyRewrite.ID, nil},
		{"POST", "/rewrites/" + f.dyRewrite.ID + "/generate", domain.RewriteOptions{}},
		{"GET", "/assets/" + f.dyAsset.ID + "/file", nil},
		{"PATCH", "/assets/" + f.dyAsset.ID + "/text", map[string]string{"text": "changed"}},
		{"GET", "/jobs/job-douyin", nil},
		{"POST", "/jobs/job-asset-douyin/retry", nil},
	} {
		if response := f.call(t, request.method, request.path, request.body, true); response.Code != 404 {
			t.Fatalf("%s %s: %d want 404: %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("owner_id", f.dyRewrite.ID)
	file, err := writer.CreateFormFile("file", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(f.png)
	_ = writer.Close()
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", f.app.CSRF)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	if response.Code != 404 {
		t.Fatalf("unscoped foreign upload: %d: %s", response.Code, response.Body.String())
	}
	var note domain.Note
	var rewrite domain.Rewrite
	_ = f.app.Store.Get("notes", f.dy.ID, &note)
	_ = f.app.Store.Get("rewrites", f.dyRewrite.ID, &rewrite)
	if !reflect.DeepEqual(note, f.dy) || !reflect.DeepEqual(rewrite, f.dyRewrite) {
		t.Fatal("retired records changed")
	}
}

func TestRetiredWorkspaceJobsNeverExecute(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, state := range []string{"queued", "running", "waiting_config", "submission_unknown", "succeeded", "failed"} {
		job := domain.Job{ID: "retired-" + state, Kind: "rewrite", EntityID: f.dyRewrite.ID, State: state,
			RemoteTaskID: "keep-remote-id", ResultAssetIDs: []string{f.dyAsset.ID}}
		if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
			t.Fatal(err)
		}
		f.app.runJob(&job)
		var saved domain.Job
		if err := f.app.Store.Get("jobs", job.ID, &saved); err != nil {
			t.Fatal(err)
		}
		want := state
		if state == "queued" || state == "running" || state == "waiting_config" {
			want = "cancelled"
		}
		if saved.State != want || saved.Attempts != 0 || saved.RemoteTaskID != "keep-remote-id" || !reflect.DeepEqual(saved.ResultAssetIDs, job.ResultAssetIDs) {
			t.Fatalf("retired job executed or lost history: %+v", saved)
		}
	}
	job := domain.Job{ID: "current", Kind: "rewrite", EntityID: f.xhsRewrite.ID, State: "queued"}
	if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	if f.app.retireWorkspaceJob(&job) || job.State != "queued" {
		t.Fatal("current platform job was retired")
	}
}

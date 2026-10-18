package app

import (
	"encoding/json"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestJobResponsesExcludeProviderSecretsWithoutChangingSnapshots(t *testing.T) {
	f := seedWorkspaceFixture(t)
	job := domain.Job{ID: "secret-job", Kind: "rewrite", EntityID: f.xhsRewrite.ID, State: "failed", Message: "provider-current-key", Error: "provider-history-key", InputScene: &domain.Scene{ID: "scene", Error: "provider-current-key"}, Payload: json.RawMessage(`{"internal":"secret-payload"}`), ProviderSnapshot: map[string]domain.ProviderConfig{"text": {BaseURL: "https://example.com", Model: "sample-model", APIKey: "provider-current-key"}}, ProviderSnapshotHistory: []map[string]domain.ProviderConfig{{"text": {APIKey: "provider-history-key"}}}}
	if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/jobs?platform=xiaohongshu", "/jobs/" + job.ID + "?platform=xiaohongshu"} {
		reply := f.call(t, "GET", path, nil, false)
		if reply.Code != 200 {
			t.Fatalf("%s: %d %s", path, reply.Code, reply.Body.String())
		}
		for _, secret := range []string{"provider-current-key", "provider-history-key", "secret-payload", "provider_snapshot", "provider_snapshot_history", "api_key"} {
			if strings.Contains(reply.Body.String(), secret) {
				t.Fatalf("%s leaked %s", path, secret)
			}
		}
	}
	var saved domain.Job
	if err := f.app.Store.Get("jobs", job.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ProviderSnapshot["text"].APIKey != "provider-current-key" || saved.ProviderSnapshotHistory[0]["text"].APIKey != "provider-history-key" || len(saved.Payload) == 0 {
		t.Fatal("response sanitization changed internal task recovery data")
	}
	if job.InputScene.Error != "provider-current-key" {
		t.Fatal("response sanitization modified input scene in place")
	}
}

func TestVideoResponsesExcludeSubmissionProviderSecrets(t *testing.T) {
	f := seedWorkspaceFixture(t)
	project := f.xhsVideo
	project.Workflow = videoRewriteWorkflow
	project.VideoRewrite = newVideoRewriteState("")
	project.VideoRewrite.Submissions = []domain.VideoSubmission{{ID: "sensitive-submission", JobID: "old-job", State: "failed", ProviderSnapshot: map[string]domain.ProviderConfig{"video": {BaseURL: "https://example.com", Model: "wan3.0-video", APIKey: "private-video-provider-key"}}}}
	if err := f.app.Store.Put("videos", project.ID, project); err != nil {
		t.Fatal(err)
	}
	copy := publicVideoProject(project)
	if copy.VideoRewrite.Submissions[0].ProviderSnapshot != nil || project.VideoRewrite.Submissions[0].ProviderSnapshot["video"].APIKey != "private-video-provider-key" {
		t.Fatal("submission response must use independent copies")
	}
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/video-projects?platform=xiaohongshu", nil},
		{"GET", "/video-projects/" + project.ID + "?platform=xiaohongshu", nil},
		{"GET", "/rewrites/" + f.xhsRewrite.ID + "?platform=xiaohongshu", nil},
		{"POST", "/rewrites/" + f.xhsRewrite.ID + "/videos/initialize?platform=xiaohongshu", map[string]any{}},
		{"PATCH", "/video-projects/" + project.ID + "?platform=xiaohongshu", map[string]any{"revision": project.Revision, "video_rewrite": map[string]any{"purpose_sources": map[string]string{}, "output": project.VideoRewrite.Output}}},
	} {
		reply := f.call(t, request.method, request.path, request.body, true)
		if reply.Code != 200 {
			t.Fatalf("%s %s: %d %s", request.method, request.path, reply.Code, reply.Body.String())
		}
		for _, secret := range []string{"private-video-provider-key", "provider_snapshot", "api_key"} {
			if strings.Contains(reply.Body.String(), secret) {
				t.Fatalf("%s %s leaked %s", request.method, request.path, secret)
			}
		}
		var saved domain.VideoProject
		if err := f.app.Store.Get("videos", project.ID, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.VideoRewrite.Submissions[0].ProviderSnapshot["video"].APIKey != "private-video-provider-key" {
			t.Fatal("response sanitization changed persisted recovery snapshot")
		}
	}
}

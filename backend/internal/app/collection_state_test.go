package app

import (
	"encoding/json"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func TestCollectionCreationAtomicAndDuplicate(t *testing.T) {
	f := newAPIFixture(t)
	url := "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f"
	first := decodeResponse[struct {
		NoteIDs []string `json:"note_ids"`
		JobIDs  []string `json:"job_ids"`
	}](t, f.call(t, "POST", "/collections", map[string]any{"text": url}, true), 202)
	second := decodeResponse[struct {
		NoteIDs []string `json:"note_ids"`
		JobIDs  []string `json:"job_ids"`
	}](t, f.call(t, "POST", "/collections", map[string]any{"text": url}, true), 202)
	if len(first.JobIDs) != 1 || len(second.JobIDs) != 1 || first.JobIDs[0] != second.JobIDs[0] {
		t.Fatalf("duplicate enqueue mismatch: %+v %+v", first, second)
	}
	if len(list[domain.Note](f.app, "notes")) != 1 || len(list[domain.Job](f.app, "jobs")) != 1 {
		t.Fatal("duplicate durable placeholder or job")
	}
	var n domain.Note
	if err := f.app.Store.Get("notes", list[domain.Note](f.app, "notes")[0].ID, &n); err != nil || n.Status != "queued" || n.MetadataStatus != "queued" {
		t.Fatalf("placeholder not queued: %+v %v", n, err)
	}
	bad := domain.Note{ID: "bad-payload", Platform: "xiaohongshu", Title: "等待采集"}
	f.app.mu.Lock()
	_, err := f.app.enqueueCollectionLocked(bad, func() {})
	f.app.mu.Unlock()
	if err == nil || f.app.Store.Get("notes", bad.ID, &n) != store.ErrNotFound {
		t.Fatal("unencodable payload left a placeholder")
	}
}

func TestCollectionRecoveryDistinguishesFailedActiveAndContent(t *testing.T) {
	f := newAPIFixture(t)
	notes := []domain.Note{
		{ID: "failed", Platform: "douyin", NoteID: "123", Title: "等待采集", Status: "failed", MetadataStatus: "failed"},
		{ID: "orphan", Platform: "douyin", Title: "等待采集", Status: "queued", MetadataStatus: "queued"},
		{ID: "active", Platform: "douyin", Title: "等待采集", Status: "queued", MetadataStatus: "queued"},
		{ID: "partial", Platform: "xiaohongshu", Title: "已获得的正文", Body: "保留内容", Status: "collecting", MetadataStatus: "succeeded", MediaStatus: "failed"},
		{ID: "ready", Platform: "douyin", Title: "真实作品", Status: "ready", MetadataStatus: "succeeded", MediaStatus: "succeeded"},
	}
	for _, n := range notes {
		if err := f.app.Store.Put("notes", n.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.app.Store.Put("jobs", "active-job", domain.Job{ID: "active-job", Kind: "collect", EntityID: "active", State: "queued"}); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("jobs", "old-job", domain.Job{ID: "old-job", Kind: "collect", EntityID: "failed", State: "failed", Error: "获取作品详情失败", UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	if err := f.app.recoverCollectionStates(); err != nil {
		t.Fatal(err)
	}
	for _, orig := range notes {
		var got domain.Note
		if err := f.app.Store.Get("notes", orig.ID, &got); err != nil {
			t.Fatal(err)
		}
		switch orig.ID {
		case "failed", "orphan":
			if got.Status != "failed" || !strings.HasPrefix(got.Title, "采集失败") {
				t.Fatalf("stale placeholder: %+v", got)
			}
		case "active":
			if got.Status != "queued" || got.Title != "等待采集" {
				t.Fatalf("active job mislabelled: %+v", got)
			}
		case "partial":
			if got.Status != "partial" || got.Body != orig.Body || got.Title != orig.Title {
				t.Fatalf("content discarded: %+v", got)
			}
		case "ready":
			if got.Title != orig.Title || got.Status != "ready" {
				t.Fatal("successful collection changed")
			}
		}
	}
}

func TestCollectionFailureAndRetryKeepEntityStateConsistent(t *testing.T) {
	f := newAPIFixture(t)
	n := domain.Note{ID: "failed-note", Platform: "xiaohongshu", NoteID: "123", Title: "等待采集", Status: "queued", MetadataStatus: "queued"}
	j := domain.Job{ID: "failed-job", Kind: "collect", EntityID: n.ID, State: "failed", Error: "获取详情失败", Payload: json.RawMessage(`{}`)}
	if err := f.app.Store.Put("notes", n.ID, n); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("jobs", j.ID, j); err != nil {
		t.Fatal(err)
	}
	f.app.jobEntityState(j)
	if err := f.app.Store.Get("notes", n.ID, &n); err != nil || n.Status != "failed" || strings.Contains(n.Title, "等待") {
		t.Fatalf("failure not visible: %+v %v", n, err)
	}
	decodeResponse[map[string]string](t, f.call(t, "POST", "/jobs/failed-job/retry", nil, true), 202)
	n = domain.Note{}
	if err := f.app.Store.Get("notes", "failed-note", &n); err != nil || n.Status != "queued" || n.Error != "" || n.Title != "等待采集" {
		t.Fatalf("retry state diverged: %+v %v", n, err)
	}
	j.State = "queued"
	f.app.jobEntityState(j)
	if err := f.app.Store.Get("notes", n.ID, &n); err != nil || n.Status != "queued" {
		t.Fatal("shutdown recovery marked queued note failed")
	}
}

func TestDeletedWorkspaceCannotQueueNewPlaceholder(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.app.Store.Put("workspace_registry", "main", workspaceRegistry{Platforms: []string{"xiaohongshu"}}); err != nil {
		t.Fatal(err)
	}
	response := f.call(t, "POST", "/collections", map[string]string{"text": "https://www.douyin.com/video/7364387149350702376"}, true)
	if response.Code != 400 {
		t.Fatalf("removed source accepted: %d %s", response.Code, response.Body.String())
	}
	if len(list[domain.Note](f.app, "notes")) != 0 || len(list[domain.Job](f.app, "jobs")) != 0 {
		t.Fatal("removed workspace resurrected content")
	}
	if _, err := f.app.enqueue("collect", "missing", "", "", nil); err == nil {
		t.Fatal("deleted entity queued again")
	}
}

func TestUnknownImageSubmissionCannotRetryAsVideoTask(t *testing.T) {
	f := seedWorkspaceFixture(t)
	j := domain.Job{ID: "unknown-image-job", Kind: "image_voice", EntityID: f.xhsVideo.ID, State: "submission_unknown"}
	if err := f.app.Store.Put("jobs", j.ID, j); err != nil {
		t.Fatal(err)
	}
	r := f.call(t, "POST", "/jobs/"+j.ID+"/retry?platform=xiaohongshu", map[string]string{"remote_task_id": "fake-video-id"}, true)
	if r.Code != 410 {
		t.Fatalf("unknown image task was resubmitted: %d %s", r.Code, r.Body.String())
	}
	if err := f.app.Store.Get("jobs", j.ID, &j); err != nil || j.State != "submission_unknown" {
		t.Fatal("unknown submission changed state")
	}
}

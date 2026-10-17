package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func TestClearJobsInterruptsRunningRequestAndPreservesOtherWorkspace(t *testing.T) {
	f := seedWorkspaceFixture(t)
	started, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
			stopped <- struct{}{}
		case <-release:
		}
	}))
	t.Cleanup(func() { f.app.cancel(); close(release); server.Close() })
	if err := f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{
		"text": {BaseURL: server.URL + "/v1", Model: "mock", APIKey: "mock-key"},
	}}); err != nil {
		t.Fatal(err)
	}
	job, err := f.app.enqueue("rewrite", f.xhsRewrite.ID, "", "", domain.RewriteOptions{IncludeBody: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	f.app.background(func() { f.app.runJob(&job); close(done) })
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider request did not start")
	}
	queued := domain.Job{ID: "queued-to-stop", EntityID: f.xhs.ID, Kind: "ocr", State: "queued"}
	if err := f.app.Store.Put("jobs", queued.ID, queued); err != nil {
		t.Fatal(err)
	}
	result := decodeResponse[struct {
		Deleted, Interrupted, Stopping int
		IDs                            []string `json:"ids"`
	}](t, f.call(t, "DELETE", "/jobs?platform=xiaohongshu", nil, true), 200)
	if result.Interrupted < 2 || result.Stopping != 0 {
		t.Fatalf("bad clear result: %+v", result)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("running HTTP request was not interrupted")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	assertWorkspaceIDs(t, f.apiFixture, "/jobs?platform=xiaohongshu")
	// Retired foreign jobs stay untouched by the current workspace cleanup.
	assertWorkspaceIDs(t, f.apiFixture, "/jobs")
	deletionAssertExists(t, f.app, "jobs", "job-douyin", true)
	deletionAssertExists(t, f.app, "jobs", "job-asset-douyin", true)
	var current domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &current); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.Versions, f.xhsRewrite.Versions) {
		t.Fatal("cancellation changed saved versions")
	}
	job.State = "succeeded"
	if err := f.app.Store.Put("jobs", job.ID, job); !errors.Is(err, store.ErrJobCleared) {
		t.Fatalf("late worker recreated cleared job: %v", err)
	}
	f.app.progress(&job, "late", 100, "late callback")
	f.app.runJob(&job)
	assertWorkspaceIDs(t, f.apiFixture, "/jobs?platform=xiaohongshu")
	if next, err := f.app.Store.Claim("ocr"); err != nil || next != nil {
		t.Fatalf("cleared queue was claimed: %+v %v", next, err)
	}
}

func TestClearJobsRemovesEveryStateAndRequiresScopedCSRF(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, state := range []string{"queued", "running", "waiting_config", "submission_unknown", "paused", "blocked", "cancelled"} {
		j := domain.Job{ID: "clear-" + state, Kind: "rewrite", EntityID: f.xhsRewrite.ID, State: state}
		if err := f.app.Store.Put("jobs", j.ID, j); err != nil {
			t.Fatal(err)
		}
	}
	if r := f.call(t, "DELETE", "/jobs?platform=xiaohongshu", nil, false); r.Code != 403 {
		t.Fatalf("missing CSRF: %d", r.Code)
	}
	if r := f.call(t, "DELETE", "/jobs", nil, true); r.Code != 400 {
		t.Fatalf("missing workspace: %d", r.Code)
	}
	decodeResponse[any](t, f.call(t, "DELETE", "/jobs?platform=xiaohongshu", nil, true), 200)
	assertWorkspaceIDs(t, f.apiFixture, "/jobs?platform=xiaohongshu")
	assertWorkspaceIDs(t, f.apiFixture, "/jobs")
	deletionAssertExists(t, f.app, "jobs", "job-douyin", true)
	deletionAssertExists(t, f.app, "jobs", "job-asset-douyin", true)
	result := decodeResponse[struct{ Deleted int }](t, f.call(t, "DELETE", "/jobs?platform=xiaohongshu", nil, true), 200)
	if result.Deleted != 0 {
		t.Fatal("repeat clear was not idempotent")
	}
}

func TestClearedJobEntityCleanupRecoversOnlyOnce(t *testing.T) {
	f := seedWorkspaceFixture(t)
	j := domain.Job{ID: "crash-clear", Kind: "video_script", EntityID: f.xhsVideo.ID, State: "cancelled"}
	video := f.xhsVideo
	video.Workflow = "video_rewrite_v2"
	video.Status = "running"
	video.VideoRewrite = &domain.VideoRewriteState{}
	if err := f.app.Store.Put("videos", video.ID, video); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("job_tombstones", j.ID, clearedJob{Job: j}); err != nil {
		t.Fatal(err)
	}
	if err := f.app.recoverClearedJobs(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Get("videos", video.ID, &video); err != nil || video.Status != "cancelled" {
		t.Fatalf("cleanup not recovered: %s %v", video.Status, err)
	}
	video.Status = "ready"
	video.Revision++
	if err := f.app.Store.Put("videos", video.ID, video); err != nil {
		t.Fatal(err)
	}
	if err := f.app.recoverClearedJobs(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Get("videos", video.ID, &video); err != nil || video.Status != "ready" {
		t.Fatal("old cancellation overwrote newer work")
	}
}

func TestClearJobsStopsImageBatchBeforeNextPaidTask(t *testing.T) {
	f := newImageV2Fixture(t)
	u := f.prepare(t)
	u = f.patch(t, u, map[string]any{"output": domain.ImageOutput{Ratio: "3:4", Clarity: "high", Count: 3}, "prompt": u.Prompt})
	result := decodeResponse[struct {
		JobID string `json:"job_id"`
	}](t, f.call(t, "POST", f.base+"/"+u.ID+"/generate", map[string]any{"revision": u.Revision, "prompt": u.Prompt, "request_id": "clear-batch"}, true), 202)
	var job domain.Job
	if err := f.app.Store.Get("jobs", result.JobID, &job); err != nil {
		t.Fatal(err)
	}
	decodeResponse[any](t, f.call(t, "DELETE", "/jobs?platform=xiaohongshu", nil, true), 200)
	u = f.load(t, u.ID)
	if len(u.Batches) != 1 || u.Batches[0].State != "cancelled" || u.Status != "cancelled" {
		t.Fatalf("batch remains active: %+v", u.Batches)
	}
	for _, item := range u.Batches[0].Items {
		if item.State != "cancelled" {
			t.Fatalf("batch item remains active: %+v", item)
		}
	}
	// A late completion cannot schedule the next image or resurrect the queue.
	job.State = "succeeded"
	f.app.mu.Lock()
	f.app.imageV2EntityState(job)
	f.app.mu.Unlock()
	f.app.runJob(&job)
	if next, err := f.app.Store.Claim("image_rewrite"); next != nil || err != nil || f.imageCalls != 0 {
		t.Fatalf("cleared batch continued: %+v, %v, calls %d", next, err, f.imageCalls)
	}
}

func TestClearJobsAttemptsVideoCancelAndKeepsProviderReceipt(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := seedWorkspaceFixture(t)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/tasks/known-task/cancel" {
					t.Errorf("unexpected provider request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"output":{"task_status":"CANCELED"}}`))
			}))
			defer server.Close()
			if err := f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{
				"video": {BaseURL: server.URL + "/api/v1", Model: "wan3.0-video", APIKey: "fixture"},
			}}); err != nil {
				t.Fatal(err)
			}
			video := f.xhsVideo
			video.Workflow = "video_rewrite_v2"
			video.VideoRewrite = &domain.VideoRewriteState{Submissions: []domain.VideoSubmission{{ID: "clear-submission", JobID: "clear-video", State: "polling", RemoteTaskID: "known-task"}}}
			payload, _ := json.Marshal(videoRewriteJobPayload{SubmissionID: "clear-submission"})
			job := domain.Job{ID: "clear-video", Kind: "video_rewrite", EntityID: video.ID, State: "running", Phase: "polling", RemoteTaskID: "known-task", Payload: payload}
			if err := f.app.Store.Put("videos", video.ID, video); err != nil {
				t.Fatal(err)
			}
			if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
				t.Fatal(err)
			}
			result := decodeResponse[struct {
				RemoteUnstopped int `json:"remote_unstopped"`
			}](t, f.call(t, "DELETE", "/jobs?platform=xiaohongshu", nil, true), 200)
			if calls != 1 || result.RemoteUnstopped != map[int]int{200: 0, 400: 1}[status] {
				t.Fatalf("provider cancellation not reported: calls=%d, %+v", calls, result)
			}
			if err := f.app.Store.Get("videos", video.ID, &video); err != nil {
				t.Fatal(err)
			}
			sub := video.VideoRewrite.Submissions[0]
			if sub.State != "cancelled" || sub.RemoteTaskID != "known-task" {
				t.Fatalf("provider receipt lost: %+v", sub)
			}
			if err := f.app.mutateVideoSubmission(video.ID, sub.ID, func(_ *domain.VideoProject, sub *domain.VideoSubmission) error {
				sub.State = "polling"
				return nil
			}); err == nil {
				t.Fatal("late callback restarted cleared video")
			}
		})
	}
}

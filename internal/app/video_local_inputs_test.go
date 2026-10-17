package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xhs-workbench/internal/domain"
)

func TestVideoLocalUploadReceiptsSurvivePreparationRestartAndExpire(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "reuse", true: "refresh-expired"}[expired], func(t *testing.T) {
			f := newVideoV2Fixture(t)
			v := f.create(t, f.video.ID)
			v.VideoRewrite.Output.Duration = 4
			v = f.compose(t, f.patch(t, v))
			response := decodeResponse[struct {
				JobID string `json:"job_id"`
			}](t, f.call(t, "POST", "/video-projects/"+v.ID+"/generate", map[string]any{"revision": v.Revision, "request_id": "prepare-restart", "prompt": v.VideoRewrite.Prompt}, true), 202)
			v = f.load(t, v.ID)
			expires := time.Now().Add(time.Hour)
			if expired {
				expires = time.Now().Add(-time.Minute)
			}
			v.VideoRewrite.Submissions[0].ReferenceUploads = map[string]domain.VideoReferenceUpload{f.video.ID: {URL: "oss://dashscope-instant/recovered/source.mp4", ExpiresAt: expires.UTC().Format(time.RFC3339)}}
			if err := f.app.Store.Put("videos", v.ID, v); err != nil {
				t.Fatal(err)
			}
			var job domain.Job
			_ = f.app.Store.Get("jobs", response.JobID, &job)
			job.State, job.Phase = "running", "upload_sources"
			_ = f.app.Store.Put("jobs", job.ID, job)
			if err := f.app.Store.Recover(); err != nil {
				t.Fatal(err)
			}
			_ = f.app.Store.Get("jobs", job.ID, &job)
			f.app.recoverVideoRewriteJob(&job)
			if job.State != "queued" {
				t.Fatal("safe preparation restart became unknown", job.State)
			}
			f.app.runJob(&job)
			wantUploads := 0
			if expired {
				wantUploads = 1
			}
			if job.State != "succeeded" || f.posts != 1 || f.uploads != wantUploads {
				t.Fatal("receipt recovery replayed upload/generation", job.Error, f.posts, f.uploads)
			}
		})
	}
}

func TestVideoLocalSourceChangeAfterConfirmationStopsBeforeUpload(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, f.video.ID)
	v.VideoRewrite.Output.Duration = 4
	v = f.compose(t, f.patch(t, v))
	response := decodeResponse[struct {
		JobID string `json:"job_id"`
	}](t, f.call(t, "POST", "/video-projects/"+v.ID+"/generate", map[string]any{"revision": v.Revision, "request_id": "changed-local", "prompt": v.VideoRewrite.Prompt}, true), 202)
	if err := os.WriteFile(filepath.Join(f.app.Data, f.video.LocalPath), []byte("changed after confirmation"), 0600); err != nil {
		t.Fatal(err)
	}
	var job domain.Job
	_ = f.app.Store.Get("jobs", response.JobID, &job)
	f.app.runJob(&job)
	if job.State != "failed" || f.policies != 0 || f.uploads != 0 || f.posts != 0 {
		t.Fatal("changed file reached upload or generation", job.State, job.Error)
	}
}

func TestVideoPromptIsOnlyEditableGenerationBasis(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, "")
	v.VideoRewrite.Requirements = "机械鸟飞过山谷"
	v.VideoRewrite.Output.Duration = 4
	v = f.compose(t, f.patch(t, v))
	digest := v.VideoRewrite.ScriptInputDigest
	prompt := "  机械鸟飞过山谷。\n镜头固定，保持安静。  "
	v.VideoRewrite.Prompt = prompt
	v = f.patch(t, v)
	if v.VideoRewrite.Prompt != prompt || v.VideoRewrite.ScriptInputDigest != digest {
		t.Fatal("prompt edit was rewritten or invalidated inputs")
	}
	// Historical segments are metadata and cannot override the reviewed prompt.
	v.VideoRewrite.Script = nil
	if err := f.app.Store.Put("videos", v.ID, v); err != nil {
		t.Fatal(err)
	}
	job := f.generate(t, v, "edited-final-only")
	if job.State != "succeeded" || f.submitted["input"].(map[string]any)["prompt"] != prompt || f.uploads != 0 {
		t.Fatal("generation used segments instead of final prompt", job.Error)
	}
	v = f.load(t, v.ID)
	if v.VideoRewrite.Results[0].Snapshot.Prompt != prompt {
		t.Fatal("history lost exact reviewed prompt")
	}
	for _, value := range []string{"", strings.Repeat("字", 20001)} {
		response := f.call(t, "PATCH", "/video-projects/"+v.ID, map[string]any{"revision": v.Revision, "video_rewrite": map[string]any{"requirements": v.VideoRewrite.Requirements, "output": v.VideoRewrite.Output, "prompt": value}}, true)
		if value != "" {
			if response.Code != 400 {
				t.Fatal("overlong prompt saved", response.Code)
			}
			continue
		}
		v = decodeResponse[domain.VideoProject](t, response, 200)
		response = f.call(t, "POST", "/video-projects/"+v.ID+"/generate", map[string]any{"revision": v.Revision, "request_id": "empty-prompt", "prompt": value}, true)
		if response.Code != 400 || f.posts != 1 {
			t.Fatal("empty prompt generated", response.Code)
		}
	}
}

func TestVideoLocalUnknownSubmissionDoesNotUploadOrPostAgain(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, f.video.ID)
	v.VideoRewrite.Output.Duration = 4
	v = f.compose(t, f.patch(t, v))
	f.mode = "unknown"
	job := f.generate(t, v, "local-unknown")
	if job.State != "submission_unknown" || f.posts != 1 || f.uploads != 1 {
		t.Fatal(job.State, job.Error)
	}
	f.app.recoverVideoRewriteJob(&job)
	if job.State != "submission_unknown" {
		t.Fatal("uncertain paid task was requeued")
	}
	f.app.runJob(&job)
	if f.posts != 1 || f.uploads != 1 {
		t.Fatal("unknown submission replayed local upload or generation")
	}
}

func TestVideoLocalReferencesKeepConfirmedOrderAndExcludeUnusedImage(t *testing.T) {
	f := newVideoV2Fixture(t)
	reference := f.video
	reference.ID, reference.OwnerID = "local-reference", f.xhsRewrite.ID
	if err := f.app.Store.Put("assets", reference.ID, reference); err != nil {
		t.Fatal(err)
	}
	v := f.create(t, f.video.ID)
	v.VideoRewrite.ReferenceVideoID = reference.ID
	v.VideoRewrite.ReferenceImageID = f.image.ID
	v.VideoRewrite.PurposeSources["camera"] = reference.ID
	v.VideoRewrite.Output.Duration = 4
	v = f.compose(t, f.patch(t, v))
	job := f.generate(t, v, "ordered-local-references")
	if job.State != "succeeded" || f.uploads != 2 || f.posts != 1 {
		t.Fatal(job.State, job.Error)
	}
	v = f.load(t, v.ID)
	sub := v.VideoRewrite.Submissions[0]
	media := f.submitted["input"].(map[string]any)["media"].([]any)
	if len(media) != 2 || len(sub.Snapshot.InputAssetIDs) != 2 || media[0].(map[string]any)["url"] != sub.ReferenceUploads[f.video.ID].URL || media[1].(map[string]any)["url"] != sub.ReferenceUploads[reference.ID].URL || has(sub.Snapshot.InputAssetIDs, f.image.ID) {
		t.Fatal("numbered references reordered or unused image entered request")
	}
}

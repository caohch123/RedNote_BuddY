package app

import (
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestBailianVideoConfigurationMigrationNeverReusesArkKey(t *testing.T) {
	f := newAPIFixture(t)
	s, _ := f.app.settings()
	s.Providers["image"] = domain.ProviderConfig{BaseURL: "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1", Model: "image", APIKey: "image-secret"}
	s.Providers["video"] = domain.ProviderConfig{BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Model: "doubao-seedance-2-5-260628", APIKey: "ark-secret"}
	if err := f.app.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := f.app.settings()
	if err != nil {
		t.Fatal(err)
	}
	p := loaded.Providers["video"]
	if p.Model != "wan3.0-video" || p.BaseURL != "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1" || p.APIKey != "" || p.Configured {
		t.Fatal("old provider key or endpoint carried over", p.Model)
	}
	response := f.call(t, "GET", "/settings", nil, false)
	for _, forbidden := range []string{"ark-secret", "image-secret", "volces.com", "seedance"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatal("settings exposed previous provider configuration")
		}
	}
	for _, bad := range []domain.ProviderConfig{
		{BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Model: "wan3.0-video"},
		{BaseURL: "https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", Model: "wan3.0-video"},
		{Model: "doubao-seedance-2-5-260628"},
	} {
		if response := f.call(t, "PATCH", "/settings", map[string]any{"providers": map[string]domain.ProviderConfig{"video": bad}}, true); response.Code != 400 {
			t.Fatal("old or incompatible native protocol accepted", response.Code)
		}
	}
	decodeResponse[domain.Settings](t, f.call(t, "PATCH", "/settings", map[string]any{"providers": map[string]domain.ProviderConfig{"video": {
		BaseURL: "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis", Model: "wan3.0-video-prime", APIKey: "wan-secret",
	}}}, true), 200)
	loaded, err = f.app.settings()
	if err != nil || loaded.Providers["video"].APIKey != "wan-secret" || loaded.Providers["video"].BaseURL != p.BaseURL {
		t.Fatal("native video settings were not saved correctly", err)
	}
}

func TestBailianVideoBudgetUsesActualSelectedVideoDuration(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, f.video.ID)
	v.VideoRewrite.Output.Duration = 27
	v = f.patch(t, v)
	response := f.call(t, "POST", "/video-projects/"+v.ID+"/compose-script", map[string]int{"revision": v.Revision}, true)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "30 秒") || f.posts+f.vision+f.text != 0 {
		t.Fatal("combined duration limit did not stop analysis/generation", response.Code, response.Body.String())
	}
	v.VideoRewrite.Output.Duration = 26
	v = f.patch(t, v)
	v = f.compose(t, v)
	if v.VideoRewrite.ScriptRulesVersion != videoRewritePromptRules || v.VideoRewrite.Script[0].End != 26 {
		t.Fatal("maximum valid selected output was rejected")
	}
}

func TestBailianSwitchDoesNotPollOrResubmitFormerProviderTasks(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, "")
	v.VideoRewrite.Requirements = "一个物体缓慢旋转"
	v = f.patch(t, v)
	v = f.compose(t, v)
	s, _ := f.app.settings()
	snap, err := f.app.videoRewriteSnapshot(v, s)
	if err != nil {
		t.Fatal(err)
	}
	snap.Provider, snap.Model = "", "doubao-seedance-2-5-260628"
	sub := domain.VideoSubmission{ID: "former-provider", JobID: "former-job", State: "submission_unknown", RemoteTaskID: "former-receipt", Snapshot: snap}
	v.VideoRewrite.Submissions = append(v.VideoRewrite.Submissions, sub)
	if err := f.app.Store.Put("videos", v.ID, v); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(videoRewriteJobPayload{Snapshot: snap, SubmissionID: sub.ID})
	job := domain.Job{ID: sub.JobID, Kind: "video_rewrite", EntityID: v.ID, State: "submission_unknown", RemoteTaskID: sub.RemoteTaskID, Payload: payload}
	if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	f.app.recoverVideoRewriteJob(&job)
	if job.State != "failed" || job.RemoteTaskID != "former-receipt" {
		t.Fatal("previous task queued against the replacement provider")
	}
	f.app.runJob(&job)
	oldScriptPayload, _ := json.Marshal(videoRewriteJobPayload{Revision: v.Revision, Snapshot: snap})
	oldScriptJob := domain.Job{ID: "former-script", Kind: "video_script", EntityID: v.ID, State: "queued", Payload: oldScriptPayload}
	textCalls, visionCalls := f.text, f.vision
	f.app.runJob(&oldScriptJob)
	if oldScriptJob.State != "failed" || f.text != textCalls || f.vision != visionCalls {
		t.Fatal("old queued script was silently recomposed after supplier replacement")
	}
	for _, action := range []string{"restore", "retry"} {
		response := f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]any{"submission_id": sub.ID, "action": action, "remote_task_id": "former-receipt", "confirm_no_result": true}, true)
		if response.Code != 409 {
			t.Fatal("previous receipt accepted by new provider", response.Code)
		}
	}
	if f.posts != 0 || f.polls != 0 {
		t.Fatal("supplier change made an unintended paid submit or poll")
	}
	decodeResponse[map[string]any](t, f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]string{"submission_id": sub.ID, "action": "cancel"}, true), 202)
}

func TestBailianVideoDownloadBoundaryAndResolutionWarning(t *testing.T) {
	for _, raw := range []string{"https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/r.mp4?signature=x", "https://bucket.oss-ap-southeast-1.aliyuncs.com/r.mp4"} {
		if err := validateVideoRewriteResultURL(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"https://aliyuncs.com.evil.example/r.mp4", "http://bucket.oss-cn-beijing.aliyuncs.com/r.mp4", "https://127.0.0.1/r.mp4"} {
		if err := validateVideoRewriteResultURL(raw); err == nil {
			u, _ := url.Parse(raw)
			t.Fatal("unsafe result host allowed", u.Hostname())
		}
	}
	f := newVideoV2Fixture(t)
	assetID, _, _, warning, err := f.app.downloadVideoRewriteResult(context.Background(), &domain.Job{ID: "wan-resolution", EntityID: f.xhsVideo.ID}, "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/result.mp4", domain.VideoOutput{Duration: 4, Resolution: "720p", Ratio: "9:16"}, 1080)
	if err != nil || assetID == "" || !strings.Contains(warning, "分辨率档位") || f.posts != 0 {
		t.Fatal("mismatched valid result was discarded or redrawn", warning, err)
	}
}

func TestBailianVideoNativeProbeBoundsAndUnselectedMaterials(t *testing.T) {
	f := newVideoV2Fixture(t)
	s, _ := f.app.settings()
	for _, seconds := range []string{"1", "16"} {
		local := "assets/wan-" + seconds + ".mp4"
		cmd := exec.Command(s.FFmpegPath, "-nostdin", "-y", "-f", "lavfi", "-i", "color=c=blue:s=240x240:r=16", "-t", seconds, "-c:v", "libx264", "-preset", "ultrafast", "-an", filepath.Join(f.app.Data, local))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		asset := domain.Asset{ID: "wan-" + seconds, OwnerID: f.xhsRewrite.ID, Kind: "video", MIME: "video/mp4", Status: "ready", LocalPath: local, Duration: 1}
		if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
			t.Fatal(err)
		}
		_, err := f.app.validateVideoRewriteMaterial(f.xhsRewrite.ID, asset.ID, "video")
		if seconds == "1" && err != nil || seconds == "16" && err == nil {
			t.Fatal("new video input bounds ignored actual duration/fps/dimensions", seconds, err)
		}
	}
	v := f.create(t, "")
	v.VideoRewrite.PrimaryAssetID = "wan-16"
	v.VideoRewrite.Requirements = "从文字创作一个物体旋转的视频"
	v = f.patch(t, v)
	v = f.compose(t, v)
	if f.vision != 0 || f.posts != 0 || !strings.Contains(v.VideoRewrite.Prompt, "物体") {
		t.Fatal("unselected original video entered analysis or generation")
	}
}

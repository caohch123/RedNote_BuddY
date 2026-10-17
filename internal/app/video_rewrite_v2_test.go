package app

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

type videoV2Fixture struct {
	workspaceFixture
	video                      domain.Asset
	image                      domain.Asset
	output                     []byte
	posts, polls, vision, text int
	uploads, policies          int
	mode                       string
	failDownload               bool
	submitted                  map[string]any
}

func newVideoV2Fixture(t *testing.T) *videoV2Fixture {
	t.Helper()
	f := &videoV2Fixture{workspaceFixture: seedWorkspaceFixture(t)}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("FFmpeg required for video workflow validation", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.app.Data, "assets", "source-video.mp4")
	cmd := exec.Command(ffmpeg, "-nostdin", "-y", "-f", "lavfi", "-i", "color=c=blue:s=720x1280:r=24", "-t", "4", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-an", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	f.output, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.video = domain.Asset{ID: "video-v2-source", OwnerID: f.xhs.ID, Kind: "video", MIME: "video/mp4", Status: "ready", LocalPath: "assets/source-video.mp4", Duration: 4, Width: 720, Height: 1280, Size: int64(len(f.output)), SHA256: digestVideoBytes(f.output), Extraction: domain.Extraction{Status: "pending"}}
	if err := f.app.Store.Put("assets", f.video.ID, f.video); err != nil {
		t.Fatal(err)
	}
	f.xhs.AssetIDs = append(f.xhs.AssetIDs, f.video.ID)
	if err := f.app.Store.Put("notes", f.xhs.ID, f.xhs); err != nil {
		t.Fatal(err)
	}
	picturePath := filepath.Join(f.app.Data, "assets", "reference-image.png")
	picture := image.NewRGBA(image.Rect(0, 0, 640, 640))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	file, err := os.Create(picturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
	file.Close()
	f.image = domain.Asset{ID: "video-v2-image", OwnerID: f.xhsRewrite.ID, Kind: "image", MIME: "image/png", Status: "ready", LocalPath: "assets/reference-image.png", Width: 640, Height: 640, Extraction: domain.Extraction{Status: "pending"}}
	if err := f.app.Store.Put("assets", f.image.ID, f.image); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/uploads" {
			f.policies++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"policy": "fixture-policy", "signature": "fixture-signature", "upload_dir": "dashscope-instant/test", "upload_host": "http://" + r.Host + "/temporary-upload", "max_file_size_mb": 100, "oss_access_key_id": "fixture-key", "x_oss_object_acl": "private", "x_oss_forbid_overwrite": "true"}})
			return
		}
		if r.URL.Path == "/temporary-upload" {
			f.uploads++
			if f.mode == "upload_failed" {
				http.Error(w, "fixture-private-response", 503)
				return
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if digestVideoBytes(data) != f.video.SHA256 {
				t.Error("uploaded bytes differ from analyzed video")
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("bearer key forwarded to object store")
			}
			w.WriteHeader(200)
			return
		}

		if strings.HasSuffix(r.URL.Path, "/video-synthesis") || strings.HasPrefix(r.URL.Path, "/api/v1/tasks/") {
			if r.Method == "POST" {
				if f.uploads > 0 && r.Header.Get("X-DashScope-OssResourceResolve") != "enable" {
					t.Error("native request cannot resolve uploaded references")
				}
				f.posts++
				_ = json.NewDecoder(r.Body).Decode(&f.submitted)
				if f.mode == "unknown" {
					http.Error(w, "unknown", 503)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_id": "task-video-v2", "task_status": "PENDING"}})
				return
			}
			f.polls++
			_ = json.NewEncoder(w).Encode(map[string]any{"output": map[string]string{"task_status": "SUCCEEDED", "video_url": "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/result.mp4"}, "usage": map[string]int{"SR": 720}})
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		messages := req["messages"].([]any)
		var content string
		if req["model"] == "vision-fixture" {
			f.vision++
			if strings.Contains(messages[0].(map[string]any)["content"].(string), "分析视频的") {
				content = `{"content":"展示物体再转身","subject":"一名人物","action":"转身","camera":"固定近景","style":"蓝白插画","audio":"无音轨"}`
			} else {
				content = `{"subject":"一个物体","action":"静止","scene":"干净背景","composition":"居中","palette":"蓝白","lighting":"柔光","texture":"插画","text":"无文字"}`
			}
		} else {
			f.text++
			var input struct {
				Output domain.VideoOutput `json:"output"`
			}
			_ = json.Unmarshal([]byte(messages[1].(map[string]any)["content"].(string)), &input)
			audio := ""
			if input.Output.GenerateAudio {
				audio = "轻柔环境声"
			}
			data, _ := json.Marshal(map[string]any{"segments": []domain.VideoScriptSegment{{Start: 0, End: input.Output.Duration, Visual: "主体居中，蓝白画风", Action: "缓慢转身", Camera: "平视固定近景", Audio: audio}}})
			content = string(data)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	t.Cleanup(server.Close)
	settings, _ := f.app.settings()
	settings.FFmpegPath = ffmpeg
	settings.FFprobePath = ffprobe
	settings.Providers = map[string]domain.ProviderConfig{"text": {BaseURL: server.URL, APIKey: "fixture", Model: "text-fixture"}, "vision": {BaseURL: server.URL, APIKey: "fixture", Model: "vision-fixture"}, "video": {BaseURL: server.URL, APIKey: "fixture", Model: "wan3.0-video"}}
	if err := f.app.saveSettings(settings); err != nil {
		t.Fatal(err)
	}
	previous := videoRewriteFetch
	videoRewriteFetch = func(ctx context.Context, raw, path string, limit int64) (string, error) {
		if strings.Contains(raw, "mismatch") {
			_ = os.WriteFile(path, []byte("different"), 0600)
			return digestVideoBytes([]byte("different")), nil
		}
		if strings.Contains(raw, "result.mp4") && f.failDownload {
			f.failDownload = false
			return "", errors.New("mock interrupted download")
		}
		if err := os.WriteFile(path, f.output, 0600); err != nil {
			return "", err
		}
		return digestVideoBytes(f.output), nil
	}
	t.Cleanup(func() { videoRewriteFetch = previous })
	return f
}

func (f *videoV2Fixture) create(t *testing.T, primary string) domain.VideoProject {
	t.Helper()
	return decodeResponse[domain.VideoProject](t, f.call(t, "POST", "/video-projects?platform=xiaohongshu", map[string]any{"workflow": videoRewriteWorkflow, "rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID, "primary_asset_id": primary}, true), 201)
}
func (f *videoV2Fixture) load(t *testing.T, id string) domain.VideoProject {
	t.Helper()
	var v domain.VideoProject
	if err := f.app.Store.Get("videos", id, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *videoV2Fixture) patch(t *testing.T, v domain.VideoProject) domain.VideoProject {
	t.Helper()
	return decodeResponse[domain.VideoProject](t, f.call(t, "PATCH", "/video-projects/"+v.ID+"?platform=xiaohongshu", map[string]any{"revision": v.Revision, "video_rewrite": v.VideoRewrite}, true), 200)
}
func (f *videoV2Fixture) compose(t *testing.T, v domain.VideoProject) domain.VideoProject {
	t.Helper()
	job := runVisualQueued(t, f.apiFixture, f.call(t, "POST", "/video-projects/"+v.ID+"/compose-script?platform=xiaohongshu", map[string]any{"revision": v.Revision}, true))
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	return f.load(t, v.ID)
}
func (f *videoV2Fixture) generate(t *testing.T, v domain.VideoProject, requestID string) domain.Job {
	t.Helper()
	return runVisualQueued(t, f.apiFixture, f.call(t, "POST", "/video-projects/"+v.ID+"/generate?platform=xiaohongshu", map[string]any{"revision": v.Revision, "request_id": requestID, "prompt": v.VideoRewrite.Prompt}, true))
}

func TestVideoV2InitializeWorkspaceAndPreserveChangedPrimary(t *testing.T) {
	f := newVideoV2Fixture(t)
	f.xhs.MetadataStatus = "succeeded"
	if err := f.app.Store.Put("notes", f.xhs.ID, f.xhs); err != nil {
		t.Fatal(err)
	}
	added := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": f.xhs.ID}, true), 201)
	initial := []domain.VideoProject{}
	for _, project := range list[domain.VideoProject](f.app, "videos") {
		if project.RewriteID == added.ID {
			initial = append(initial, project)
		}
	}
	if len(initial) != 1 || initial[0].VideoRewrite.PrimaryAssetID != f.video.ID {
		t.Fatal("adding note did not initialize its video workbench")
	}
	r := f.xhsRewrite
	path := "/rewrites/" + r.ID + "/videos/initialize?platform=xiaohongshu"
	projects := decodeResponse[[]domain.VideoProject](t, f.call(t, "POST", path, nil, true), 200)
	if len(projects) != 1 || projects[0].VideoRewrite == nil {
		t.Fatal("missing automatic draft", projects)
	}
	v := projects[0]
	if v.VideoRewrite.PrimaryAssetID != f.video.ID {
		t.Fatal("source not bound")
	}
	v.VideoRewrite.PrimaryAssetID = ""
	v.VideoRewrite.PurposeSources = map[string]string{}
	v = f.patch(t, v)
	again := decodeResponse[[]domain.VideoProject](t, f.call(t, "POST", path, nil, true), 200)
	if len(again) != 1 || again[0].ID != v.ID || again[0].VideoRewrite.PrimaryAssetID != v.VideoRewrite.PrimaryAssetID {
		t.Fatal("initialization duplicated or overwrote edited draft")
	}
	// The retired Douyin workspace is rejected before any handler runs.
	if resp := f.call(t, "POST", "/rewrites/"+f.dyRewrite.ID+"/videos/initialize?platform=douyin", nil, true); resp.Code != 400 {
		t.Fatalf("retired workspace initialization status %d", resp.Code)
	}
	if f.vision+f.text+f.posts != 0 {
		t.Fatal("initialization invoked models")
	}
}

func TestVideoV2MockMixedReferenceFlowAndDownloadRecovery(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, f.video.ID)
	v.VideoRewrite.ReferenceImageID = f.image.ID
	v.VideoRewrite.PurposeSources["style"] = f.image.ID
	v.VideoRewrite.Output.Duration = 4
	v.VideoRewrite.Requirements = "只借图片画风，不改变主体"
	v = f.patch(t, v)
	v = f.compose(t, v)
	if f.posts != 0 || f.uploads != 0 || f.policies != 0 || f.vision != 2 || v.VideoRewrite.ScriptInputDigest == "" {
		t.Fatal("analysis missing or auto-generated video")
	}
	digest := v.VideoRewrite.ScriptInputDigest
	v.VideoRewrite.VideoURLs[f.video.ID] = "https://inputs.example/source.mp4"
	v = f.patch(t, v)
	if v.VideoRewrite.ScriptInputDigest != digest || !strings.Contains(v.VideoRewrite.Prompt, "图1") || !strings.Contains(v.VideoRewrite.Prompt, "视频1") {
		t.Fatal("adding link invalidated script or input numbering wrong")
	}
	v.VideoRewrite.Prompt = "视频1的主体向左挥手，画风参考图1。\n 保留我的标点与空格！ "
	v = f.patch(t, v)
	f.failDownload = true
	job := f.generate(t, v, "mixed-confirmed")
	if job.State != "failed" || f.posts != 1 {
		t.Fatal(job.State, job.Error)
	}
	v = f.load(t, v.ID)
	sub := v.VideoRewrite.Submissions[0]
	if sub.RemoteTaskID == "" || sub.ResultURL == "" {
		t.Fatal("receipt lost before download")
	}
	if f.uploads != 1 || len(sub.ReferenceUploads) != 1 || !strings.HasPrefix(sub.ReferenceUploads[f.video.ID].URL, "oss://dashscope-instant/") {
		t.Fatal("local upload receipt missing", sub.ReferenceUploads)
	}
	request := map[string]any{"revision": v.Revision, "request_id": "mixed-confirmed", "prompt": v.VideoRewrite.Prompt}
	if response := f.call(t, "POST", "/video-projects/"+v.ID+"/generate", request, true); response.Code != 202 || f.posts != 1 {
		t.Fatal("duplicate confirmation replayed generation")
	}
	job = runVisualQueued(t, f.apiFixture, f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]any{"submission_id": sub.ID, "action": "restore"}, true))
	if job.State != "succeeded" || f.posts != 1 || f.uploads != 1 {
		t.Fatal("restore created new provider task", job.Error)
	}
	v = f.load(t, v.ID)
	result := v.VideoRewrite.Results[0]
	input := f.submitted["input"].(map[string]any)
	if input["prompt"] != result.Snapshot.Prompt || result.Snapshot.Prompt != "视频1的主体向左挥手，画风参考图1。\n 保留我的标点与空格！ " || !strings.HasPrefix(input["media"].([]any)[0].(map[string]any)["url"].(string), "oss://dashscope-instant/") {
		t.Fatal("edited prompt or uploaded media differs from confirmed request")
	}
	if len(result.Snapshot.Analyses) != 2 || !strings.Contains(result.Snapshot.Prompt, "向左挥手") || f.submitted["parameters"].(map[string]any)["audio"] != true || f.submitted["parameters"].(map[string]any)["prompt_extend"] != false || f.submitted["parameters"].(map[string]any)["duration"] != float64(4) {
		t.Fatal("confirmed basis differs from request or history")
	}
	var saved domain.Rewrite
	_ = f.app.Store.Get("rewrites", f.xhsRewrite.ID, &saved)
	if !reflect.DeepEqual(saved.Versions, f.xhsRewrite.Versions) {
		t.Fatal("generation changed publishing choice")
	}
	old := saved.Versions[0]
	old.AssetIDs = []string{f.xhsAsset.ID, f.video.ID, f.image.ID}
	saved.Versions[0] = old
	_ = f.app.Store.Put("rewrites", saved.ID, saved)
	applied := decodeResponse[domain.Version](t, f.call(t, "POST", "/video-projects/"+v.ID+"/apply", map[string]string{"asset_id": result.AssetID, "version_id": old.ID}, true), 201)
	if !reflect.DeepEqual(applied.AssetIDs, []string{f.xhsAsset.ID, result.AssetID, f.image.ID}) || applied.Body != old.Body || applied.Title != old.Title {
		t.Fatal("adoption changed unrelated material or copy")
	}
	v.VideoRewrite.Requirements = "下一次要求"
	v = f.patch(t, v)
	if v.VideoRewrite.ScriptInputDigest != "" || !reflect.DeepEqual(v.VideoRewrite.Results[0], result) {
		t.Fatal("edit rewrote history or retained stale script")
	}
}

func TestVideoV2NoSourceTextOnlyAndUnknownRestartDoNotReplay(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, "")
	v.VideoRewrite.Requirements = "一只机械鸟飞过天空"
	v.VideoRewrite.Output.Duration = 4
	v = f.patch(t, v)
	v = f.compose(t, v)
	if f.vision != 0 {
		t.Fatal("text-only invoked vision")
	}
	f.mode = "unknown"
	j := f.generate(t, v, "unknown-confirmed")
	if j.State != "submission_unknown" || f.posts != 1 {
		t.Fatal(j.State, j.Error)
	}
	f.app.recoverVideoRewriteJob(&j)
	if j.State != "submission_unknown" {
		t.Fatal("unknown restart was queued for automatic replay")
	}
	v = f.load(t, v.ID)
	sub := v.VideoRewrite.Submissions[0]
	if response := f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]any{"submission_id": sub.ID, "action": "retry"}, true); response.Code != 409 {
		t.Fatal("unconfirmed retry accepted")
	}
	if response := f.call(t, "POST", "/video-projects/"+v.ID+"/generate", map[string]any{"revision": v.Revision, "request_id": "new-click", "prompt": v.VideoRewrite.Prompt}, true); response.Code != 409 {
		t.Fatal("unknown state accepted new paid request")
	}
	f.mode = ""
	j = runVisualQueued(t, f.apiFixture, f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]any{"submission_id": sub.ID, "action": "restore", "remote_task_id": "task-recovered"}, true))
	if j.State != "succeeded" || f.posts != 1 {
		t.Fatal("known task recovery resubmitted", j.Error)
	}
	v = f.load(t, v.ID)
	result := v.VideoRewrite.Results[0]
	old := f.xhsRewrite.Versions[0]
	applied := decodeResponse[domain.Version](t, f.call(t, "POST", "/video-projects/"+v.ID+"/apply", map[string]string{"asset_id": result.AssetID, "version_id": old.ID}, true), 201)
	if !reflect.DeepEqual(applied.AssetIDs, append(append([]string{}, old.AssetIDs...), result.AssetID)) {
		t.Fatal("no-source adoption did not append")
	}
}

func TestVideoV2UploadFailureCacheStaleInputsAndProjectIsolation(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, f.video.ID)
	v.VideoRewrite.Output.Duration = 4
	v = f.patch(t, v)
	v = f.compose(t, v)
	v = f.compose(t, v)
	if f.vision != 1 {
		t.Fatal("no-audio visual analysis was not cached", f.vision)
	}
	v.VideoRewrite.VideoURLs[f.video.ID] = "https://inputs.example/mismatch.mp4"
	v = f.patch(t, v)
	f.mode = "upload_failed"
	j := f.generate(t, v, "upload-failed")
	if j.State != "failed" || f.posts != 0 {
		t.Fatal("upload failure reached paid provider", j.Error)
	}
	for _, raw := range []string{"http://public.example/x.mp4", "https://127.0.0.1/x.mp4", "https://192.168.1.1/x.mp4", "https://localhost/x.mp4"} {
		if validateVideoRewritePublicURL(raw) == nil {
			t.Fatal("accepted non-public HTTPS URL", raw)
		}
	}
	if _, err := f.app.validateVideoRewriteMaterial(f.dyRewrite.ID, f.video.ID, "video"); err == nil {
		t.Fatal("cross-workspace material accepted")
	}
	other := f.xhsRewrite
	other.ID = "different-project"
	_ = f.app.Store.Put("rewrites", other.ID, other)
	if _, err := f.app.validateVideoRewriteMaterial(other.ID, f.image.ID, "image"); err == nil {
		t.Fatal("cross-project uploaded reference accepted")
	}
	v = f.load(t, v.ID)
	sub := v.VideoRewrite.Submissions[0]
	_ = f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]any{"submission_id": sub.ID, "action": "cancel"}, true)
	v = f.load(t, v.ID)
	f.video.Extraction = domain.Extraction{Status: "succeeded", Edited: true, Text: "人工修正文字"}
	_ = f.app.Store.Put("assets", f.video.ID, f.video)
	response := f.call(t, "POST", "/video-projects/"+v.ID+"/generate", map[string]any{"revision": v.Revision, "request_id": "stale-transcript", "prompt": v.VideoRewrite.Prompt}, true)
	if response.Code != 409 || f.posts != 0 {
		t.Fatal("stale corrected source generated", response.Code)
	}
}

func TestVideoV2RestartRestoresReceiptWithoutNewPaidRequest(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, "")
	v.VideoRewrite.Requirements = "蓝白色的机械鸟"
	v.VideoRewrite.Output.Duration = 4
	v = f.patch(t, v)
	v = f.compose(t, v)
	f.failDownload = true
	j := f.generate(t, v, "restart-download")
	if j.State != "failed" {
		t.Fatal(j.Error)
	}
	j.State = "running"
	j.Phase = "download"
	if err := f.app.Store.Put("jobs", j.ID, j); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Recover(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Get("jobs", j.ID, &j); err != nil {
		t.Fatal(err)
	}
	f.app.recoverVideoRewriteJob(&j)
	if j.State != "queued" || j.RemoteTaskID == "" {
		t.Fatal("known remote task not resumed", j)
	}
	f.app.runJob(&j)
	if j.State != "succeeded" || f.posts != 1 {
		t.Fatal("restart replayed paid submission", j.Error, f.posts)
	}
}

func TestVideoV2RecoverySurvivesClearedFailedJob(t *testing.T) {
	f := newVideoV2Fixture(t)
	v := f.create(t, "")
	v.VideoRewrite.Requirements = "一只机械鸟"
	v.VideoRewrite.Output.Duration = 4
	v = f.patch(t, v)
	v = f.compose(t, v)
	f.failDownload = true
	j := f.generate(t, v, "cleaned-history")
	if j.State != "failed" {
		t.Fatal(j.Error)
	}
	if err := f.app.Store.Delete("jobs", j.ID); err != nil {
		t.Fatal(err)
	}
	v = f.load(t, v.ID)
	sub := v.VideoRewrite.Submissions[0]
	j = runVisualQueued(t, f.apiFixture, f.call(t, "POST", "/video-projects/"+v.ID+"/resume", map[string]any{"submission_id": sub.ID, "action": "restore"}, true))
	if j.State != "succeeded" || f.posts != 1 {
		t.Fatal("clearing task index prevented receipt recovery or replayed a paid task", j.Error)
	}
}

func TestVideoV2NativeAudioHEVCPreviewPreservesOriginal(t *testing.T) {
	f := newVideoV2Fixture(t)
	s, _ := f.app.settings()
	path := filepath.Join(f.app.Data, "assets", "hevc-audio.mp4")
	command := exec.Command(s.FFmpegPath, "-nostdin", "-y", "-f", "lavfi", "-i", "color=c=blue:s=480x854:r=24", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "4", "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "pools=1:frame-threads=1:log-level=error", "-pix_fmt", "yuv420p", "-c:a", "aac", path)
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatal(err, string(data))
	}
	var err error
	f.output, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	project := f.create(t, "")
	job := domain.Job{ID: "hevc-output", EntityID: project.ID}
	assetID, previewID, _, warning, err := f.app.downloadVideoRewriteResult(context.Background(), &job, "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/result.mp4", domain.VideoOutput{Duration: 4, Resolution: "480p", Ratio: "9:16", GenerateAudio: true})
	if err != nil || assetID == "" || previewID == "" {
		t.Fatal("missing original or preview", err, warning)
	}
	var original, preview domain.Asset
	_ = f.app.Store.Get("assets", assetID, &original)
	_ = f.app.Store.Get("assets", previewID, &preview)
	originalProbe, err := f.app.probeVideoRewrite(context.Background(), filepath.Join(f.app.Data, original.LocalPath))
	if err != nil {
		t.Fatal(err)
	}
	previewProbe, err := f.app.probeVideoRewrite(context.Background(), filepath.Join(f.app.Data, preview.LocalPath))
	if err != nil {
		t.Fatal(err)
	}
	if !originalProbe.audio() || !previewProbe.audio() || originalProbe.browserCompatible() || !previewProbe.browserCompatible() || originalProbe.width() != previewProbe.width() || originalProbe.height() != previewProbe.height() {
		t.Fatal("compatibility conversion changed native audio, dimensions or original")
	}
}

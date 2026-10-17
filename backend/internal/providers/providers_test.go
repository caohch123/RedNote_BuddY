package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"xhs-workbench/internal/domain"
)

func configured(capability, base string) *Client {
	model := "fixture-model"
	if capability == "video" {
		model = "wan3.0-video"
	}
	return New(domain.Settings{Providers: map[string]domain.ProviderConfig{capability: {APIKey: "test-secret", BaseURL: base, Model: model}}})
}

func TestMissingConfigurationNeverProducesSuccess(t *testing.T) {
	c := New(domain.Settings{})
	if _, e := c.Rewrite(context.Background(), domain.Note{}, nil, domain.RewriteOptions{}); !errors.Is(e, ErrNotConfigured) {
		t.Fatalf("rewrite: %v", e)
	}
	if _, e := c.CreateVideoRewrite(context.Background(), VideoRewriteRequest{Prompt: "x", Output: domain.VideoOutput{Duration: 5, Resolution: "720p", Ratio: "9:16"}}); !errors.Is(e, ErrNotConfigured) {
		t.Fatalf("video: %v", e)
	}
	if _, e := c.OCR(context.Background(), "missing"); !errors.Is(e, ErrNotConfigured) {
		t.Fatalf("ocr: %v", e)
	}
	if _, e := c.Transcribe(context.Background(), "missing"); !errors.Is(e, ErrNotConfigured) {
		t.Fatalf("asr: %v", e)
	}
}

func TestCollectNormalizesAllAssetsAndDiscardsSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xhs/detail" || r.Method != "POST" {
			t.Error("incorrect collector route")
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["download"] != false || req["check_record"] != false || req["cookie"] != "private-cookie" {
			t.Error("collector flags missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"params": map[string]any{"cookie": "private-cookie"}, "data": map[string]any{"作品ID": "abc123", "作品标题": "标题", "作品描述": "完整正文", "作品标签": "生活 #技巧", "作者昵称": "作者", "作者ID": "u1", "作品类型": "视频", "点赞数量": "1.2万", "收藏数量": "0", "评论数量": "-1", "发布时间": "2026-10-06_21:00:00", "下载地址": []string{"https://sns-video.xhscdn.com/1.mp4"}, "封面地址": "https://ci.xiaohongshu.com/cover.jpg", "secret": "private-cookie", "params": map[string]any{"cookie": "private-cookie"}}})
	}))
	defer server.Close()
	c := New(domain.Settings{CollectorURL: server.URL})
	n, assets, e := c.Collect(context.Background(), "https://www.xiaohongshu.com/explore/abc123?xsec_token=sample", "private-cookie")
	if e != nil {
		t.Fatal(e)
	}
	if n.NoteID != "abc123" || n.Body != "完整正文" || n.Likes == nil || *n.Likes != 12000 || n.Collects == nil || *n.Collects != 0 || n.Comments != nil {
		t.Fatalf("incorrect normalized note: %+v", n)
	}
	if n.PublishedAt != "2026-10-06T13:00:00Z" || len(n.Topics) != 2 {
		t.Fatal("time/topic normalization")
	}
	if len(assets) != 2 || assets[0].Kind != "video" || assets[1].Kind != "cover" {
		t.Fatalf("all assets expected: %+v", assets)
	}
	raw, _ := json.Marshal(n.Raw)
	if strings.Contains(string(raw), "private-cookie") || strings.Contains(string(raw), "params") {
		t.Fatal("upstream secrets persisted")
	}
	if _, _, e = c.Collect(context.Background(), "https://example.com/explore/abc", ""); e == nil {
		t.Fatal("unsupported host accepted")
	}
}

func TestRewriteUsesSelectedSourcesAndValidatesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.URL.Path != "/chat/completions" {
			t.Error("missing auth/protocol")
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if _, exists := req["enable_thinking"]; exists {
			t.Error("DashScope extension added to generic compatible endpoint")
		}
		messages := req["messages"].([]any)
		text := messages[1].(map[string]any)["content"].(string)
		if strings.Contains(text, "excluded-body") || !strings.Contains(text, "selected OCR") {
			t.Error("unselected original body leaked into rewrite")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "```json\n{\"title\":\"新标题\",\"body\":\"新正文\",\"topics\":[\"技巧\"],\"script\":\"口播\",\"confirmed\":true,\"asset_ids\":[\"rogue\"]}\n```"}}}})
	}))
	defer server.Close()
	v, e := configured("text", server.URL).Rewrite(context.Background(), domain.Note{Title: "Source", Body: "excluded-body"}, []string{"selected OCR"}, domain.RewriteOptions{IncludeBody: false})
	if e != nil {
		t.Fatal(e)
	}
	if v.Title != "新标题" || v.Script != "口播" || v.Confirmed || len(v.AssetIDs) != 0 {
		t.Fatalf("untrusted metadata accepted: %+v", v)
	}
	var parsed domain.Version
	if parseJSON("not-json", &parsed) == nil {
		t.Fatal("invalid model result accepted")
	}
}

func TestRewriteRejectsModelTitlesOverTwentyCharacters(t *testing.T) {
	for _, title := range []string{strings.Repeat("中", 20), strings.Repeat("中", 21), strings.Repeat("😀", 20), strings.Repeat("😀", 21)} {
		t.Run(title, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []struct{ Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) < 2 || !strings.Contains(req.Messages[0].Content, "不超过 20 个字符") {
					t.Error("missing title limit in model instructions")
				}
				content, _ := json.Marshal(map[string]any{"title": title, "body": "正文", "script": "口播", "topics": []string{}})
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
			}))
			defer server.Close()
			v, err := configured("text", server.URL).Rewrite(context.Background(), domain.Note{Platform: "xiaohongshu"}, nil, domain.RewriteOptions{})
			if len([]rune(title)) == 20 {
				if err != nil || v.Title != title {
					t.Fatalf("valid model title changed or rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "20") || v.Title != "" {
				t.Fatal("invalid model title reached a saved version")
			}
		})
	}
}

func TestRecoverableServiceErrorsAreSpecificAndNeverEchoRawMessages(t *testing.T) {
	for _, fixture := range []struct {
		status   int
		code     string
		contains string
	}{
		{401, "InvalidApiKey", "密钥无效"}, {403, "AccessDenied", "模型权限不足"}, {429, "Throttling", "限流或额度不足"},
		{422, "XHS_NOTE_UNAVAILABLE", "重新填写有效 Cookie 后重新采集"}, {502, "XHS_FETCH_FAILED", "网络异常"}, {400, "XHS_INVALID_INPUT", "粘贴完整笔记链接"},
	} {
		t.Run(fixture.code, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"code": fixture.code, "message": "test-secret/private-cookie", "params": map[string]string{"cookie": "private-cookie"}})
			err := statusError(fixture.status, body)
			if !strings.Contains(err.Error(), fixture.contains) {
				t.Fatalf("missing recovery guidance: %v", err)
			}
			if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "private-cookie") {
				t.Fatalf("raw secret leaked: %v", err)
			}
		})
	}
}

func TestOCRUsesImageDataURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
		image := content[0].(map[string]any)["image_url"].(map[string]any)["url"].(string)
		if !strings.HasPrefix(image, "data:image/png;base64,") {
			t.Error("local image not encoded")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "识别文字"}}}})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "image.png")
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jX8sAAAAASUVORK5CYII=")
	_ = os.WriteFile(path, data, 0600)
	str, e := configured("ocr", server.URL).OCR(context.Background(), path)
	if e != nil || str != "识别文字" {
		t.Fatalf("ocr %q %v", str, e)
	}
}

func TestBailian404RecoveryCodesNeverLeakProviderBody(t *testing.T) {
	for _, fixture := range []struct {
		code string
		hint string
	}{
		{"ModelNotOpen", "核对模型开通状态、地域、工作空间"},
		{"ModelNotFound", "核对模型开通状态、地域、工作空间"},
	} {
		t.Run(fixture.code, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != "POST" || r.URL.Path != "/api/v1/services/aigc/video-generation/video-synthesis" {
					t.Error("incorrect submission route")
				}
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"code": fixture.code, "message": "test-secret/private-cookie https://private.example/signed?token=private-token",
				}, "params": map[string]string{"cookie": "private-cookie"}})
			}))
			defer server.Close()
			id, err := configured("video", server.URL+"/api/v1").CreateVideoRewrite(context.Background(), VideoRewriteRequest{Prompt: "画面", Output: domain.VideoOutput{Duration: 10, Resolution: "720p", Ratio: "9:16"}})
			if err == nil || id != "" || errors.Is(err, ErrSubmissionUnknown) || requests.Load() != 1 {
				t.Fatalf("definite rejection must return no task and no retry: id=%q err=%v requests=%d", id, err, requests.Load())
			}
			if !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(err.Error(), fixture.code) || !strings.Contains(err.Error(), fixture.hint) {
				t.Fatalf("missing official code or recovery guidance: %v", err)
			}
			for _, secret := range []string{"test-secret", "private-cookie", "private-token", "private.example"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("provider body leaked: %v", err)
				}
			}
		})
	}
	// Prefixes of documented codes must not bypass the static allowlist.
	body := []byte(`{"error":{"code":"InvalidEndpointOrModel.NotFound.test-secret","message":"private-cookie"}}`)
	err := statusError(http.StatusNotFound, body)
	if !strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "private-cookie") {
		t.Fatalf("unknown error code was exposed: %v", err)
	}
}

func TestVideoConnectionCheckOnlyQueriesTaskList(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/v1/tasks" || r.URL.Query().Get("page_size") != "1" {
			t.Error("connection check must only query the task list")
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("connection check did not authenticate")
		}
		if r.ContentLength > 0 || r.URL.Query().Get("model") != "" {
			t.Error("task-list check must not claim model-specific entitlement")
		}
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}))
	defer server.Close()
	if err := configured("video", server.URL+"/api/v1").Test(context.Background(), "video"); err != nil || requests.Load() != 1 {
		t.Fatalf("connection check: err=%v requests=%d", err, requests.Load())
	}
}

func TestParaformerWebsocketFramesAndTimedFinalSentences(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("ASR auth missing")
		}
		conn, e := upgrader.Upgrade(w, r, nil)
		if e != nil {
			t.Error(e)
			return
		}
		defer conn.Close()
		var run map[string]any
		if e = conn.ReadJSON(&run); e != nil {
			t.Error(e)
			return
		}
		if run["header"].(map[string]any)["action"] != "run-task" || run["payload"].(map[string]any)["parameters"].(map[string]any)["sample_rate"] != float64(16000) {
			t.Error("wrong run-task")
		}
		_ = conn.WriteJSON(map[string]any{"header": map[string]string{"event": "task-started"}})
		typeID, b, e := conn.ReadMessage()
		if e != nil || typeID != websocket.BinaryMessage || len(b) == 0 {
			t.Error("missing binary PCM", e)
			return
		}
		_ = conn.WriteJSON(map[string]any{"header": map[string]string{"event": "result-generated"}, "payload": map[string]any{"output": map[string]any{"sentence": map[string]any{"text": "中间", "begin_time": 0, "end_time": 10, "sentence_end": false}}}})
		var finish map[string]any
		if e = conn.ReadJSON(&finish); e != nil {
			t.Error(e)
			return
		}
		if finish["header"].(map[string]any)["action"] != "finish-task" {
			t.Error("missing finish-task")
		}
		_ = conn.WriteJSON(map[string]any{"header": map[string]string{"event": "result-generated"}, "payload": map[string]any{"output": map[string]any{"sentence": map[string]any{"text": "识别结果。", "begin_time": 100, "end_time": 900, "sentence_end": true}}}})
		_ = conn.WriteJSON(map[string]any{"header": map[string]string{"event": "task-finished"}})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "audio.pcm")
	_ = os.WriteFile(path, make([]byte, 3200), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	segments, e := configured("asr", "ws"+strings.TrimPrefix(server.URL, "http")+"/api-ws/v1/inference").Transcribe(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if len(segments) != 1 || segments[0].Start != 0.1 || segments[0].End != 0.9 || segments[0].Text != "识别结果。" {
		t.Fatalf("timed sentences %+v", segments)
	}
}

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWanLocalVideoUploadAndNativeOSSReference(t *testing.T) {
	data := []byte("local-video-exact-bytes")
	policyCalls, uploads, generations := 0, 0, 0
	var objectKey, prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/uploads":
			policyCalls++
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-secret" || r.URL.Query().Get("model") != "wan3.0-video" || r.URL.Query().Get("action") != "getPolicy" {
				t.Error("wrong model-bound upload policy request")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"upload_host": "http://" + r.Host + "/oss", "upload_dir": "dashscope-instant/test", "policy": "signed-policy", "signature": "signature", "oss_access_key_id": "object-access", "x_oss_object_acl": "private", "x_oss_forbid_overwrite": "true", "max_file_size_mb": "100"}})
		case "/oss":
			uploads++
			if r.Header.Get("Authorization") != "" {
				t.Error("API key leaked to storage")
			}
			reader, err := r.MultipartReader()
			if err != nil {
				t.Error(err)
				return
			}
			fields := map[string]string{}
			var last string
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Error(err)
					return
				}
				last = part.FormName()
				content, _ := io.ReadAll(part)
				if last == "file" {
					if !bytes.Equal(content, data) || !strings.HasSuffix(part.FileName(), ".mov") {
						t.Error("uploaded content changed")
					}
				} else {
					fields[last] = string(content)
				}
			}
			if last != "file" || fields["x-oss-object-acl"] != "private" || fields["success_action_status"] != "200" || fields["Signature"] != "signature" {
				t.Error("wrong private multipart contract")
			}
			objectKey = fields["key"]
			w.WriteHeader(200)
		case "/api/v1/services/aigc/video-generation/video-synthesis":
			generations++
			if r.Header.Get("X-DashScope-OssResourceResolve") != "enable" {
				t.Error("missing OSS resolver header")
			}
			var payload struct {
				Input struct {
					Prompt string              `json:"prompt"`
					Media  []map[string]string `json:"media"`
				} `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if len(payload.Input.Media) != 1 || payload.Input.Media[0]["url"] != "oss://"+objectKey {
				t.Error("generation didn't use uploaded source")
			}
			prompt = payload.Input.Prompt
			_, _ = io.WriteString(w, `{"output":{"task_id":"local-confirmed","task_status":"PENDING"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := configured("video", server.URL)
	receipt, err := c.UploadVideoRewriteReference(context.Background(), data, ".mov")
	if err != nil {
		t.Fatal(err)
	}
	expires, _ := time.Parse(time.RFC3339, receipt.ExpiresAt)
	if !expires.After(time.Now().Add(46*time.Hour)) || receipt.URL != "oss://"+objectKey || policyCalls != 1 || uploads != 1 || generations != 0 {
		t.Fatal("upload submitted generation or lost receipt")
	}
	confirmed := "视频1向左转身。\n 保留这段编辑！ "
	id, err := c.CreateVideoRewrite(context.Background(), VideoRewriteRequest{Prompt: confirmed, Output: rewriteVideoOutput(), Videos: []VideoReference{{URL: receipt.URL, Duration: 4}}})
	if err != nil || id != "local-confirmed" || prompt != confirmed || generations != 1 {
		t.Fatal("confirmed prompt changed", err)
	}
}

func TestWanUploadFailuresAreDefiniteAndNeverGenerate(t *testing.T) {
	for _, mode := range []string{"policy-error", "upload-error", "redirect", "hostile-host", "public-acl", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			uploads, generations := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/uploads" {
					if mode == "policy-error" {
						http.Error(w, "test-secret", 403)
						return
					}
					p := map[string]any{"upload_host": "http://" + r.Host + "/oss", "upload_dir": "dashscope-instant/test", "policy": "test-secret-policy", "signature": "test-secret-signature", "oss_access_key_id": "test-secret-access", "x_oss_object_acl": "private", "x_oss_forbid_overwrite": "true", "max_file_size_mb": 100}
					if mode == "hostile-host" {
						p["upload_host"] = "https://example.com/steal"
					}
					if mode == "public-acl" {
						p["x_oss_object_acl"] = "public-read"
					}
					if mode == "capacity" {
						p["max_file_size_mb"] = 0
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": p})
					return
				}
				if r.URL.Path == "/oss" {
					uploads++
					if mode == "redirect" {
						w.Header().Set("Location", "/steal")
						w.WriteHeader(307)
						return
					}
					http.Error(w, "test-secret-policy", 503)
					return
				}
				generations++
				t.Error("upload failure caused unexpected request")
			}))
			defer server.Close()
			_, err := configured("video", server.URL).UploadVideoRewriteReference(context.Background(), []byte("video"), ".mp4")
			if err == nil || errors.Is(err, ErrSubmissionUnknown) || strings.Contains(err.Error(), "test-secret") || generations != 0 || uploads > 1 {
				t.Fatal("upload error was unsafe or treated as paid submission", err)
			}
		})
	}
}

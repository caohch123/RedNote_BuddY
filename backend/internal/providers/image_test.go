package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"xhs-workbench/internal/domain"
)

func imageFixture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestImageProviderConfigurationAndRealPayload(t *testing.T) {
	if _, _, err := New(domain.Settings{}).GenerateImage(context.Background(), "图片"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("missing configuration: %v", err)
	}
	picture := imageFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/images/generations" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("invalid image request")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "fixture-model" || body["prompt"] != "清晨的花园" || body["size"] != "1024x1536" || body["n"] != float64(1) {
			t.Errorf("payload: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(picture)}}})
	}))
	defer server.Close()
	data, mime, err := configured("image", server.URL).GenerateImage(context.Background(), "清晨的花园")
	if err != nil || mime != "image/png" || !bytes.Equal(data, picture) {
		t.Fatalf("image: %s %v", mime, err)
	}
}

func TestImageOutputURLNeverReceivesCredentials(t *testing.T) {
	picture := imageFixture(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.png" {
			if r.Header.Get("Authorization") != "" {
				t.Error("credential leaked to output")
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(picture)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"url": server.URL + "/image.png"}}})
	}))
	defer server.Close()
	_, _, err := configured("image", server.URL).GenerateImage(context.Background(), "图片")
	if err != nil {
		t.Fatal(err)
	}
}

func TestImageAmbiguousSubmissionIsNotRepeated(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte("provider failed"))
	}))
	defer server.Close()
	_, _, err := configured("image", server.URL).GenerateImage(context.Background(), "图片")
	if !errors.Is(err, ErrSubmissionUnknown) || posts.Load() != 1 {
		t.Fatalf("submission count=%d error=%v", posts.Load(), err)
	}
}

func TestImageRejectsNonImageAndPrivateOutput(t *testing.T) {
	for _, response := range []string{`{"data":[{"b64_json":"bm90IGFuIGltYWdl"}]}`, `{"data":[{"url":"https://127.0.0.1/private.png"}]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
		_, _, err := configured("image", server.URL).GenerateImage(context.Background(), "图片")
		server.Close()
		if err == nil {
			t.Fatal("unsafe or invalid output accepted")
		}
	}
}

func TestVisionConnectionOnlyQueriesModels(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("vision connection check must authenticate without a paid inference")
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	if err := configured("vision", server.URL).Test(context.Background(), "vision"); err != nil || calls.Load() != 1 {
		t.Fatalf("connection check: count=%d error=%v", calls.Load(), err)
	}
}

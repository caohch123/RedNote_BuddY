package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestBillingChatUsageAndSecretBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"private-output"}}],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":10}},"secret":"test-secret","signed_url":"https://private/?token=hidden"}`))
	}))
	defer server.Close()
	records := []domain.UsageRecord{}
	c := configured("text", server.URL).WithUsage(func(v domain.UsageRecord) { records = append(records, v) })
	if _, err := c.chat(context.Background(), "text", []map[string]any{{"role": "user", "content": "private-input"}}, false); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%d", len(records))
	}
	v := records[0]
	if *v.InputTokens != 120 || *v.OutputTokens != 30 || *v.TotalTokens != 150 || *v.CachedTokens != 40 || *v.ReasoningTokens != 10 || v.Status != "success" {
		t.Fatalf("incorrect usage: %+v", v)
	}
	b, _ := json.Marshal(v)
	for _, secret := range []string{"test-secret", "private-output", "private-input", "signed_url", "token=hidden"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("usage leaked %s", secret)
		}
	}
	if err := c.Test(context.Background(), "text"); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatal("connection checks must not count as model calls")
	}
}

func TestBillingUnknownZeroAndPartialUsage(t *testing.T) {
	tests := []struct {
		name, raw                            string
		input, output, total, cached, reason *int64
	}{
		{"missing", `{}`, nil, nil, nil, nil, nil},
		{"null", `{"usage":{"prompt_tokens":null,"completion_tokens":null,"total_tokens":null}}`, nil, nil, nil, nil, nil},
		{"zero", `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`, usageInt(0), usageInt(0), usageInt(0), nil, nil},
		{"derived", `{"usage":{"input_tokens":25,"output_tokens":5}}`, usageInt(25), usageInt(5), usageInt(30), nil, nil},
		{"total-only", `{"usage":{"total_tokens":80}}`, nil, nil, usageInt(80), nil, nil},
		{"invalid", `{"usage":{"input_tokens":-1,"output_tokens":1.5,"total_tokens":"9"}}`, nil, nil, nil, nil, nil},
		{"inconsistent", `{"usage":{"input_tokens":20,"output_tokens":10,"total_tokens":15}}`, nil, nil, nil, nil, nil},
		{"invalid-subsets", `{"usage":{"input_tokens":10,"output_tokens":2,"prompt_tokens_details":{"cached_tokens":50},"completion_tokens_details":{"reasoning_tokens":30}}}`, usageInt(10), usageInt(2), usageInt(12), nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got domain.UsageRecord
			c := configured("vision", "https://provider.example/path?private=secret").WithUsage(func(v domain.UsageRecord) { got = v })
			c.reportUsage(c.settings.Providers["vision"], usageContext{capability: "vision"}, 200, []byte(tc.raw))
			pairs := [][2]*int64{{got.InputTokens, tc.input}, {got.OutputTokens, tc.output}, {got.TotalTokens, tc.total}, {got.CachedTokens, tc.cached}, {got.ReasoningTokens, tc.reason}}
			for _, pair := range pairs {
				if (pair[0] == nil) != (pair[1] == nil) || (pair[0] != nil && *pair[0] != *pair[1]) {
					t.Fatalf("incorrect nullable usage: %+v", got)
				}
			}
			if got.Provider != "https://provider.example" {
				t.Fatal("provider identity retained path/query")
			}
		})
	}
}

func TestBillingFailedRequestsAndMissingConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"Throttling","message":"test-secret"}}`))
	}))
	defer server.Close()
	var records []domain.UsageRecord
	c := configured("text", server.URL).WithUsage(func(v domain.UsageRecord) { records = append(records, v) })
	if _, err := c.chat(context.Background(), "text", nil, false); err == nil {
		t.Fatal("expected failed request")
	}
	if len(records) != 1 || records[0].Status != "failed" || records[0].TotalTokens != nil {
		t.Fatal("failed requests should retain unknown usage")
	}
	if _, err := c.chat(context.Background(), "ocr", nil, false); err == nil {
		t.Fatal("expected missing configuration")
	}
	if len(records) != 1 {
		t.Fatal("unsubmitted requests must not be recorded")
	}
}

func TestBillingAsyncVideoPollUsesOneIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = w.Write([]byte(`{"output":{"task_id":"task-123","task_status":"PENDING"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":{"task_id":"task-123","task_status":"SUCCEEDED","video_url":"https://example.org/video.mp4"},"usage":{"output_video_duration":5,"SR":720}}`))
	}))
	defer server.Close()
	var records []domain.UsageRecord
	c := configured("video", server.URL).WithUsage(func(v domain.UsageRecord) { records = append(records, v) })
	id, err := c.CreateVideoRewrite(context.Background(), VideoRewriteRequest{Prompt: "test", Output: domain.VideoOutput{Duration: 5, Resolution: "720p", Ratio: "9:16"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if i == 1 {
			p := c.settings.Providers["video"]
			p.APIKey = "rotated-key"
			c.settings.Providers["video"] = p
		}
		if _, err = c.PollVideo(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if len(records) != 3 || records[0].Status != "pending" {
		t.Fatalf("incorrect submission records: %+v", records)
	}
	for _, v := range records {
		if v.ID != records[0].ID || v.TotalTokens != nil {
			t.Fatal("polling must retain the submitted call identity without inventing tokens")
		}
	}
	if records[1].VideoSeconds == nil || *records[1].VideoSeconds != 5 {
		t.Fatal("missing video duration")
	}
}

func TestBillingImageResponseIsRecordedBeforeLocalResultValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"invalid"}],"usage":{"image_count":1}}`))
	}))
	defer server.Close()
	var records []domain.UsageRecord
	c := configured("image", server.URL).WithUsage(func(v domain.UsageRecord) { records = append(records, v) })
	if _, _, err := c.GenerateImage(context.Background(), "test"); err == nil {
		t.Fatal("expected invalid generated image")
	}
	if len(records) != 1 || records[0].ImageCount == nil || *records[0].ImageCount != 1 || records[0].TotalTokens != nil {
		t.Fatalf("image usage was lost or converted to tokens: %+v", records)
	}
}

func usageInt(v int64) *int64 { return &v }

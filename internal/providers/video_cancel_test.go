package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"xhs-workbench/internal/domain"
)

func TestCancelVideoTaskUsesCancelEndpointAndReportsUnsupportedRunningTask(t *testing.T) {
	for _, status := range []int{200, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/tasks/remote-receipt/cancel" || r.Header.Get("Authorization") != "Bearer mock" {
					t.Errorf("unexpected cancel request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					w.Write([]byte(`{"output":{"task_status":"CANCELED"}}`))
				} else {
					w.Write([]byte(`{"code":"UnsupportedOperation","message":"Task is running"}`))
				}
			}))
			defer server.Close()
			client := New(domain.Settings{Providers: map[string]domain.ProviderConfig{"video": {BaseURL: server.URL + "/api/v1", Model: "wan3.0-video", APIKey: "mock"}}})
			err := client.CancelVideoTask(context.Background(), "remote-receipt")
			if (err == nil) != (status == 200) || calls != 1 {
				t.Fatalf("cancel result %v, calls %d", err, calls)
			}
			if err := client.CancelVideoTask(context.Background(), "../invalid"); err == nil || calls != 1 {
				t.Fatal("invalid receipt reached provider")
			}
		})
	}
}

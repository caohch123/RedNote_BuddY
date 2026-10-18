package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestUsageGuardBlocksActualModelRequestWithoutInventingUsage(t *testing.T) {
	requests, records, released := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"total_tokens":5}}`))
	}))
	defer server.Close()
	denied := errors.New("quota reached")
	c := configured("text", server.URL).WithUsage(func(v domain.UsageRecord) { records++ }).WithUsageGuard(func(ctx context.Context, cap string) (func(), error) { return nil, denied })
	if _, err := c.chat(context.Background(), "text", nil, false); !errors.Is(err, denied) {
		t.Fatal("guard failure lost", err)
	}
	if requests != 0 || records != 0 {
		t.Fatal("denied call reached upstream or invented usage", requests, records)
	}
	c.WithUsageGuard(func(ctx context.Context, cap string) (func(), error) {
		if cap != "text" {
			t.Fatal(cap)
		}
		return func() { released++ }, nil
	})
	if _, err := c.chat(context.Background(), "text", nil, false); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || records != 1 || released != 1 {
		t.Fatal("successful call did not release quota", requests, records, released)
	}
}

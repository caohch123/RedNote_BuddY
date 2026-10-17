package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestCollectorOfflineExplainsLocalServiceWithoutExposingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := server.URL
	server.Close()
	client := New(domain.Settings{CollectorURL: base})
	_, _, err := client.Collect(context.Background(), "https://www.xiaohongshu.com/explore/test?xsec_token=private-token", "private-cookie")
	assertCollectorOffline := func(err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "小红书采集服务未连接") || !strings.Contains(err.Error(), "检测采集服务") {
			t.Fatalf("missing local collector recovery guidance: %v", err)
		}
		for _, hidden := range []string{"private-cookie", "private-token", base, "API 地址"} {
			if strings.Contains(err.Error(), hidden) {
				t.Fatal("collector error leaked credentials or gave AI configuration guidance")
			}
		}
	}
	assertCollectorOffline(err)
	assertCollectorOffline(client.Test(context.Background(), "collector"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = client.Collect(ctx, "https://www.xiaohongshu.com/explore/test", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was mislabeled as collector unavailable: %v", err)
	}
}

func TestCollectorHealthRequiresReadinessAndPreservesPlatformErrors(t *testing.T) {
	ready := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/health" {
			if ready {
				_, _ = w.Write([]byte(`{"ok":true,"version":"2.8"}`))
			} else {
				_, _ = w.Write([]byte(`{"ok":false}`))
			}
			return
		}
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"code":"XHS_NOTE_UNAVAILABLE","error":"private-cookie"}`))
	}))
	defer server.Close()
	client := New(domain.Settings{CollectorURL: server.URL})
	if err := client.Test(context.Background(), "collector"); err == nil || !strings.Contains(err.Error(), "尚未就绪") {
		t.Fatalf("unready health reported success: %v", err)
	}
	ready = true
	if err := client.Test(context.Background(), "collector"); err != nil {
		t.Fatal(err)
	}
	_, _, err := client.Collect(context.Background(), "https://www.xiaohongshu.com/explore/test", "private-cookie")
	if err == nil || !strings.Contains(err.Error(), "XHS_NOTE_UNAVAILABLE") || strings.Contains(err.Error(), "未连接") || strings.Contains(err.Error(), "private-cookie") {
		t.Fatalf("platform error was hidden or unsafe: %v", err)
	}
}

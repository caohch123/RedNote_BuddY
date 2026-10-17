package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublisherHelperProcess(t *testing.T) {
	if os.Getenv("XHS_PUBLISHER_TEST_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &request)
		if request.Method == "block" || request.Method == "publish" {
			time.Sleep(5 * time.Second)
			continue
		}
		if request.Method == "malformed" {
			_, _ = os.Stdout.WriteString("Cookie=SECRET_MUST_NOT_ESCAPE\n")
			continue
		}
		if request.Method == "error" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": "submission_unknown", "message": "Cookie=SECRET_MUST_NOT_ESCAPE", "submission_unknown": true}})
		} else {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "result": map[string]any{"available": true, "method": request.Method}})
		}
	}
	os.Exit(0)
}

func testPublisher() *XHSPublisher {
	p := NewXHSPublisher("unused-test-root")
	p.command = func() (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPublisherHelperProcess$")
		cmd.Env = append(os.Environ(), "XHS_PUBLISHER_TEST_HELPER=1")
		hidePublisherWindow(cmd)
		return cmd, nil
	}
	return p
}

func TestPublisherCallsSerializedAndReusable(t *testing.T) {
	p := testPublisher()
	defer p.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result map[string]any
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := p.Call(ctx, "health", map[string]any{}, &result); err != nil {
				t.Error(err)
				return
			}
			if result["available"] != true {
				t.Error("missing helper result")
			}
		}()
	}
	wg.Wait()
}

func TestPublisherNeverReflectsSecretErrors(t *testing.T) {
	p := testPublisher()
	defer p.Close()
	err := p.Call(context.Background(), "error", nil, nil)
	var typed *XHSPublisherError
	if !errors.As(err, &typed) || !typed.SubmissionUnknown || !typed.Unknown {
		t.Fatalf("lost unknown classification: %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "Cookie") {
		t.Fatal("worker leaked secret")
	}
	err = p.Call(context.Background(), "malformed", nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("malformed reply should be safely rejected")
	}
	if err = p.Call(context.Background(), "health", nil, nil); err != nil {
		t.Fatalf("worker should restart for a later explicit call: %v", err)
	}
}

func TestPublisherCancellationClosesWorkerWithoutRetry(t *testing.T) {
	p := testPublisher()
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.Call(ctx, "block", nil, nil)
	var typed *XHSPublisherError
	if !errors.As(err, &typed) || typed.SubmissionUnknown {
		t.Fatalf("bad nonpublish cancellation classification: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("worker did not honor context")
	}
	if err = p.Call(context.Background(), "health", nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	if p.Call(context.Background(), "health", nil, nil) == nil {
		t.Fatal("closed transport accepted a request")
	}
}

func TestPublisherPublishDisconnectConservative(t *testing.T) {
	p := testPublisher()
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := p.Call(ctx, "publish", map[string]any{"id": "fixture"}, nil)
	var typed *XHSPublisherError
	if !errors.As(err, &typed) || !typed.SubmissionUnknown {
		t.Fatalf("publish disconnect must prevent retries: %v", err)
	}
}

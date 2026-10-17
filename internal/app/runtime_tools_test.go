package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"xhs-workbench/internal/domain"
)

type runtimeToolRPC struct{ closes int }

func (r *runtimeToolRPC) Call(context.Context, string, any, any) error { return nil }
func (r *runtimeToolRPC) Close() error                                 { r.closes++; return nil }

func TestRuntimePathSaveClearsToAutoAndRefreshesWorkersWithoutDeletingCredentials(t *testing.T) {
	f := newAPIFixture(t)
	xhs := &runtimeToolRPC{}
	f.app.xhs.rpc, f.app.xhs.loaded = xhs, true
	saved := savedXHSAccount{EncryptedSession: "dpapi:preserved-session", Account: xhsAccount{ID: "saved-account"}}
	if err := f.app.Store.Put("xhs_accounts", "main", saved); err != nil {
		t.Fatal(err)
	}
	settings, _ := f.app.settings()
	settings.NodePath = "previous-node.exe"
	if err := f.app.saveSettings(settings); err != nil {
		t.Fatal(err)
	}
	response := f.call(t, "PATCH", "/settings", map[string]any{"node_path": "", "ffmpeg_path": "", "ffprobe_path": ""}, true)
	config := decodeResponse[domain.Settings](t, response, 200)
	if config.NodePath != "" || config.FFmpegPath != "" || config.FFprobePath != "" {
		t.Fatal("empty path didn't select auto mode")
	}
	if xhs.closes != 1 || f.app.xhs.loaded {
		t.Fatal("old runtime remained active")
	}
	var actual savedXHSAccount
	if err := f.app.Store.Get("xhs_accounts", "main", &actual); err != nil || actual != saved {
		t.Fatal("path change erased saved account", err)
	}
	// Omitting a path in a subsequent unrelated save must preserve it.
	config = decodeResponse[domain.Settings](t, f.call(t, "PATCH", "/settings", map[string]any{"ffmpeg_path": "ffmpeg"}, true), 200)
	config = decodeResponse[domain.Settings](t, f.call(t, "PATCH", "/settings", map[string]any{"providers": map[string]any{}}, true), 200)
	if config.FFmpegPath != "ffmpeg" {
		t.Fatal("omitted path changed configuration")
	}
}

func TestRuntimeNodePathCannotInterruptAccountOperation(t *testing.T) {
	f := newAPIFixture(t)
	saved, _ := f.app.settings()
	f.app.xhs.op.Lock()
	response := f.call(t, "PATCH", "/settings", map[string]any{"node_path": "new-node.exe"}, true)
	f.app.xhs.op.Unlock()
	if response.Code != 409 {
		t.Fatal("active account operation interrupted", response.Code)
	}
	actual, _ := f.app.settings()
	if actual.NodePath != saved.NodePath {
		t.Fatal("rejected path persisted")
	}
}

func TestRuntimeHealthRequiresExecutableVersionAndLiveDatabaseQuery(t *testing.T) {
	f := newAPIFixture(t)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer collector.Close()
	s, _ := f.app.settings()
	s.CollectorURL = collector.URL
	s.FFmpegPath = f.app.Root // Existing directories aren't tools.
	s.FFprobePath = filepath.Join(f.app.Root, "missing-ffprobe.exe")
	s.NodePath, _ = os.Executable() // Existing wrong binary isn't a ready Node.
	if err := f.app.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	report := decodeResponse[struct {
		Dependencies map[string]struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		} `json:"dependencies"`
	}](t, f.call(t, "GET", "/health", nil, false), 200)
	for _, key := range []string{"ffmpeg", "ffprobe", "node"} {
		if report.Dependencies[key].OK || report.Dependencies[key].Message == "" {
			t.Fatal("invalid tool marked ready", key)
		}
	}
	if !report.Dependencies["database"].OK || !report.Dependencies["collector"].OK {
		t.Fatal("live service checks failed")
	}
}

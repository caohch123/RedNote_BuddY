package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

type workspaceDeletionRPC struct{ logout, closed int }

func (r *workspaceDeletionRPC) Call(_ context.Context, method string, _ any, _ any) error {
	if method == "logout" {
		r.logout++
	}
	return nil
}
func (r *workspaceDeletionRPC) Close() error { r.closed++; return nil }

func registryItems(t *testing.T, f apiFixture) []workspaceEntry {
	t.Helper()
	response := decodeResponse[struct {
		Items []workspaceEntry `json:"items"`
		Total int              `json:"total"`
	}](t, f.call(t, "GET", "/workspaces", nil, false), 200)
	if len(response.Items) != response.Total {
		t.Fatal(response)
	}
	return response.Items
}

func TestWorkspaceRegistryDefaultsDeletionAndEmptyRecreation(t *testing.T) {
	f := seedWorkspaceFixture(t)
	items := registryItems(t, f.apiFixture)
	if len(items) != 1 || items[0].ID != "xiaohongshu" {
		t.Fatal(items)
	}
	var settings domain.Settings
	if err := f.app.Store.Get("settings", "main", &settings); err != nil {
		t.Fatal(err)
	}
	settings.Cookie = "dpapi:synthetic-private-cookie"
	settings.Providers["text"] = domain.ProviderConfig{APIKey: "dpapi:synthetic-private-api-key", Model: "preserve-model", BaseURL: "https://example.com"}
	deletionPut(t, f.app, "settings", "main", settings)
	deletionPut(t, f.app, "xhs_accounts", "main", savedXHSAccount{EncryptedSession: "dpapi:synthetic-private-session"})
	rpc := &workspaceDeletionRPC{}
	f.app.xhs.rpc = rpc
	f.app.xhs.session = "synthetic-private-session"
	result := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/workspaces/xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	if result.Deleted["notes"] != 2 || result.Deleted["publications"] != 2 {
		t.Fatal(result)
	}
	if rpc.logout != 1 || rpc.closed != 1 || f.app.xhs.session != "" {
		t.Fatalf("account runtime not cleared: %+v", rpc)
	}
	deletionAssertExists(t, f.app, "xhs_accounts", "main", false)
	// Retired foreign records are never touched by the remaining workspace.
	deletionAssertExists(t, f.app, "notes", f.dy.ID, true)
	deletionAssertExists(t, f.app, "publications", f.dyPub.ID, true)
	items = registryItems(t, f.apiFixture)
	if len(items) != 0 {
		t.Fatal(items)
	}
	var after domain.Settings
	if err := f.app.Store.Get("settings", "main", &after); err != nil {
		t.Fatal(err)
	}
	if after.Cookie != "" || !reflect.DeepEqual(after.Providers, settings.Providers) || after.CollectorURL != settings.CollectorURL {
		t.Fatal("global settings changed or account cookie retained")
	}
	for _, path := range []string{"/collections?platform=xiaohongshu", "/xhs/account", "/xhs/account?platform=xiaohongshu"} {
		if response := f.call(t, "GET", path, nil, false); response.Code != 404 {
			t.Fatalf("removed workspace reachable %s: %d", path, response.Code)
		}
	}
	decodeResponse[workspaceEntry](t, f.call(t, "POST", "/workspaces", map[string]string{"platform": "xiaohongshu"}, true), 201)
	assertWorkspaceIDs(t, f.apiFixture, "/collections?platform=xiaohongshu")
	assertWorkspaceIDs(t, f.apiFixture, "/rewrites?platform=xiaohongshu")
	assertWorkspaceIDs(t, f.apiFixture, "/video-projects?platform=xiaohongshu")
	assertWorkspaceIDs(t, f.apiFixture, "/publications?platform=xiaohongshu")
	if len(registryItems(t, f.apiFixture)) != 1 {
		t.Fatal("recreated workspace absent")
	}
}

func TestWorkspaceRegistryValidationAndDeletingLastWorkspace(t *testing.T) {
	f := newAPIFixture(t)
	for _, scenario := range []struct {
		method, path string
		body         any
		status       int
	}{
		{"POST", "/workspaces", map[string]string{"platform": "weibo"}, 400},
		{"POST", "/workspaces", map[string]string{"platform": "douyin"}, 400},
		{"DELETE", "/workspaces/douyin", map[string]bool{"confirm": true}, 404},
		{"DELETE", "/workspaces/xiaohongshu", map[string]bool{"confirm": false}, 400},
		{"DELETE", "/workspaces/xiaohongshu?platform=douyin", map[string]bool{"confirm": true}, 400},
	} {
		response := f.call(t, scenario.method, scenario.path, scenario.body, true)
		if response.Code != scenario.status {
			t.Fatalf("%s %s: %d want %d", scenario.method, scenario.path, response.Code, scenario.status)
		}
	}
	decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/workspaces/xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	if len(registryItems(t, f)) != 0 {
		t.Fatal("last workspace remains")
	}
	decodeResponse[workspaceEntry](t, f.call(t, "POST", "/workspaces", map[string]string{"platform": "xiaohongshu"}, true), 201)
	if len(registryItems(t, f)) != 1 {
		t.Fatal("cannot recreate from empty registry")
	}
	if response := f.call(t, "POST", "/workspaces", map[string]string{"platform": "xiaohongshu"}, true); response.Code != 409 {
		t.Fatalf("duplicate workspace accepted: %d", response.Code)
	}
}

func TestQueuedDeletionCancelsDescendantJobsAtomically(t *testing.T) {
	f := seedWorkspaceFixture(t)
	deletionPut(t, f.app, "jobs", "queued-collect", domain.Job{ID: "queued-collect", Kind: "collect", EntityID: f.xhs.ID, State: "queued"})
	deletionPut(t, f.app, "jobs", "queued-hidden-publication", domain.Job{ID: "queued-hidden-publication", Kind: "publish_prepare", EntityID: f.crossPub.ID, State: "queued"})
	decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	deletionAssertExists(t, f.app, "jobs", "queued-collect", false)
	deletionAssertExists(t, f.app, "jobs", "queued-hidden-publication", false)
}

func TestDeletionFilesRollbackAndRecovery(t *testing.T) {
	f := newAPIFixture(t)
	for _, mode := range []string{"rollback", "interrupted-before-commit", "interrupted-after-commit"} {
		t.Run(mode, func(t *testing.T) {
			original := filepath.Join("assets", mode+".mp4")
			deletionMakeFile(t, f.app, original)
			staged, err := f.app.stageDeletionFiles([]string{filepath.Join(f.app.Data, original)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(f.app.Data, original)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("original was not staged")
			}
			if mode == "interrupted-after-commit" {
				deletionPut(t, f.app, "deletion_cleanup", staged.ID, staged)
			}
			if mode == "rollback" {
				err = f.app.rollbackDeletionFiles(staged)
			} else {
				err = f.app.recoverDeletionCleanup()
			}
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(filepath.Join(f.app.Data, original))
			if mode == "interrupted-after-commit" {
				if !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal("committed file was restored")
				}
			} else if statErr != nil {
				t.Fatal("uncommitted file not restored", statErr)
			}
			deletionAssertExists(t, f.app, "deletion_cleanup", staged.ID, false)
			dir, _ := f.app.deletionStageDirectory(staged.ID)
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("staging directory remains")
			}
		})
	}
}

func TestDeletionFileFailureRestoresStagedFiles(t *testing.T) {
	f := newAPIFixture(t)
	deletionMakeFile(t, f.app, "assets/first.mp4")
	staged, err := f.app.stageDeletionFiles([]string{filepath.Join(f.app.Data, "assets/first.mp4"), filepath.Join(f.app.Data, "assets/missing.mp4")})
	if err == nil || staged == nil {
		t.Fatal("expected partial staging failure with recovery journal")
	}
	if err = f.app.rollbackDeletionFiles(staged); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.app.Data, "assets/first.mp4")); err != nil {
		t.Fatal("first file not restored", err)
	}
}

func TestDeletionRecoveryRejectsUnsafeJournalWithoutOutsideMutation(t *testing.T) {
	f := newAPIFixture(t)
	outside := filepath.Join(t.TempDir(), "preserve.txt")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	staged := deletionStaging{ID: "unsafe-journal", Files: []deletionFileMove{{Original: outside, Staged: "file"}}}
	dir, err := f.app.deletionStageDirectory(staged.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(staged)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.app.recoverDeletionCleanup(); !errors.Is(err, errUnsafeDeletionPath) {
		t.Fatal("unsafe journal accepted", err)
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "preserve" {
		t.Fatal("outside file changed")
	}
}

func TestAtomicCascadeFailureRestoresFilesAndDatabase(t *testing.T) {
	f := newAPIFixture(t)
	deletionMakeFile(t, f.app, "assets/rollback.mp4")
	deletionPut(t, f.app, "notes", "rollback-note", domain.Note{ID: "rollback-note", Platform: "douyin"})
	var staged *deletionStaging
	abort := errors.New("injected transaction failure")
	err := f.app.Store.Atomic(func(tx *store.Transaction) error {
		var err error
		staged, err = f.app.stageDeletionFiles([]string{filepath.Join(f.app.Data, "assets/rollback.mp4")})
		if err != nil {
			return err
		}
		if err = tx.Delete("notes", "rollback-note"); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if err := f.app.rollbackDeletionFiles(staged); err != nil {
		t.Fatal(err)
	}
	deletionAssertExists(t, f.app, "notes", "rollback-note", true)
	if _, err := os.Stat(filepath.Join(f.app.Data, "assets/rollback.mp4")); err != nil {
		t.Fatal(err)
	}
}

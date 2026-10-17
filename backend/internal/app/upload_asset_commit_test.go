package app

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func uploadCommitContext(platform string) (*gin.Context, *httptest.ResponseRecorder) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set(workspaceContextKey, platform)
	return c, response
}

func TestUploadCommitRechecksOwnerAfterConcurrentDeletionAndRemovesFile(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	asset := domain.Asset{ID: "pending-upload", OwnerID: note.ID, LocalPath: "assets/pending-upload.mp4"}
	deletionMakeFile(t, f.app, asset.LocalPath)
	path := filepath.Join(f.app.Data, asset.LocalPath)
	// The upload has already checked its owner and written its new file, then
	// a cascade commits before the upload reaches its final DB save.
	decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections/"+note.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	c, response := uploadCommitContext("xiaohongshu")
	if f.app.commitUploadedAsset(c, asset, path) || response.Code != 409 {
		t.Fatalf("stale upload committed: %d %s", response.Code, response.Body.String())
	}
	deletionAssertExists(t, f.app, "assets", asset.ID, false)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("untracked upload file remains", err)
	}
}

func TestUploadCommitSerializesOwnerCheckWithDeletionMutex(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	asset := domain.Asset{ID: "locked-upload", OwnerID: note.ID, LocalPath: "assets/locked-upload.png"}
	deletionMakeFile(t, f.app, asset.LocalPath)
	c, response := uploadCommitContext("xiaohongshu")
	f.app.mu.Lock()
	started := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		close(started)
		done <- f.app.commitUploadedAsset(c, asset, filepath.Join(f.app.Data, asset.LocalPath))
	}()
	<-started
	// A deletion already holding the mutex wins; a final upload save must not
	// inspect the old owner or persist an asset before that deletion commits.
	if err := f.app.Store.Atomic(func(tx *store.Transaction) error { return tx.Delete("notes", note.ID) }); err != nil {
		f.app.mu.Unlock()
		t.Fatal(err)
	}
	f.app.mu.Unlock()
	select {
	case committed := <-done:
		if committed || response.Code != 409 {
			t.Fatalf("orphan upload committed: %v HTTP %d", committed, response.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload commit deadlocked")
	}
	deletionAssertExists(t, f.app, "assets", asset.ID, false)
}

func TestUploadCommitRejectsRemovedWorkspaceAndKeepsOtherData(t *testing.T) {
	f := newAPIFixture(t)
	note, original, _ := seedSource(t, f)
	asset := domain.Asset{ID: "removed-workspace-upload", OwnerID: note.ID, LocalPath: "assets/removed-workspace-upload.mp4"}
	deletionMakeFile(t, f.app, asset.LocalPath)
	if err := f.app.Store.Atomic(func(tx *store.Transaction) error { return setWorkspaceRegistry(tx, "xiaohongshu", false) }); err != nil {
		t.Fatal(err)
	}
	c, response := uploadCommitContext("")
	if f.app.commitUploadedAsset(c, asset, filepath.Join(f.app.Data, asset.LocalPath)) || response.Code != 409 {
		t.Fatalf("removed workspace accepted upload: %d %s", response.Code, response.Body.String())
	}
	deletionAssertExists(t, f.app, "assets", asset.ID, false)
	deletionAssertExists(t, f.app, "assets", original.ID, true)
	deletionAssertExists(t, f.app, "notes", note.ID, true)
}

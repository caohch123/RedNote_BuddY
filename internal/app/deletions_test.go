package app

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func deletionPut(t *testing.T, a *App, bucket, id string, value any) {
	t.Helper()
	if err := a.Store.Put(bucket, id, value); err != nil {
		t.Fatal(err)
	}
}
func deletionAssertExists(t *testing.T, a *App, bucket, id string, exists bool) {
	t.Helper()
	var raw json.RawMessage
	err := a.Store.Get(bucket, id, &raw)
	if exists && err != nil {
		t.Fatalf("%s/%s must survive: %v", bucket, id, err)
	}
	if !exists && !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%s/%s must be deleted: %v", bucket, id, err)
	}
}
func deletionMakeFile(t *testing.T, a *App, path string) {
	t.Helper()
	path = filepath.Join(a.Data, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("temporary deletion test data"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCollectionCascadesAndPreservesOtherWorkspace(t *testing.T) {
	f := seedWorkspaceFixture(t)
	owned := []domain.Asset{
		{ID: "rewrite-owned", OwnerID: f.xhsRewrite.ID, LocalPath: "assets/rewrite-owned.wav"},
		{ID: "video-owned", OwnerID: f.xhsVideo.ID, LocalPath: "renders/video-xhs/render/clip.mp4"},
		{ID: "publication-owned", OwnerID: f.xhsPub.ID, LocalPath: "assets/publication-owned.png"},
	}
	for _, asset := range owned {
		deletionMakeFile(t, f.app, asset.LocalPath)
		deletionPut(t, f.app, "assets", asset.ID, asset)
	}
	deletionMakeFile(t, f.app, "renders/video-xhs/render/manifest.json")
	deletionMakeFile(t, f.app, "logs/job-xhs.log")
	result := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	for bucket, ids := range map[string][]string{
		"notes": {f.xhs.ID}, "rewrites": {f.xhsRewrite.ID}, "videos": {f.xhsVideo.ID},
		"publications": {f.xhsPub.ID, f.crossPub.ID}, "assets": {f.xhsAsset.ID, "rewrite-owned", "video-owned", "publication-owned"}, "jobs": {"job-xhs", "job-historical-douyin"},
	} {
		for _, id := range ids {
			deletionAssertExists(t, f.app, bucket, id, false)
		}
	}
	for bucket, ids := range map[string][]string{
		"notes": {f.dy.ID, f.legacy.ID}, "rewrites": {f.dyRewrite.ID}, "videos": {f.dyVideo.ID},
		"publications": {f.dyPub.ID}, "assets": {f.dyAsset.ID}, "jobs": {"job-douyin", "job-asset-douyin", "job-legacy"},
	} {
		for _, id := range ids {
			deletionAssertExists(t, f.app, bucket, id, true)
		}
	}
	if result.Deleted["publications"] != 2 || result.FilesDeleted != 5 {
		t.Fatalf("cascade summary: %+v", result)
	}
	// The other platform's asset has the same LocalPath; keep the shared file.
	if _, err := os.Stat(filepath.Join(f.app.Data, f.xhsAsset.LocalPath)); err != nil {
		t.Fatal("shared physical media removed", err)
	}
	for _, path := range []string{"assets/rewrite-owned.wav", "assets/publication-owned.png", "renders/video-xhs", "logs/job-xhs.log"} {
		if _, err := os.Stat(filepath.Join(f.app.Data, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("file/directory remains %s: %v", path, err)
		}
	}
	deletionAssertExists(t, f.app, "settings", "main", true)
}

func TestDeleteConfirmationCSRFAndWorkspaceBoundary(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, scenario := range []struct {
		path   string
		body   any
		auth   bool
		status int
	}{
		{"/collections/" + f.xhs.ID + "?platform=xiaohongshu", map[string]bool{"confirm": true}, false, 403},
		{"/collections/" + f.xhs.ID + "?platform=xiaohongshu", map[string]bool{"confirm": false}, true, 400},
		{"/collections/" + f.xhs.ID + "?platform=douyin", map[string]bool{"confirm": true}, true, 400},
		{"/collections?platform=douyin", map[string]any{"ids": []string{f.dy.ID, f.xhs.ID}, "confirm": true}, true, 400},
		{"/collections?platform=douyin", map[string]any{"ids": []string{}, "confirm": true}, true, 400},
		{"/collections/" + f.dy.ID + "?platform=xiaohongshu", map[string]bool{"confirm": true}, true, 404},
		{"/collections?platform=xiaohongshu", map[string]any{"ids": []string{f.dy.ID}, "confirm": true}, true, 404},
	} {
		response := f.call(t, "DELETE", scenario.path, scenario.body, scenario.auth)
		if response.Code != scenario.status {
			t.Fatalf("%s: HTTP %d want %d: %s", scenario.path, response.Code, scenario.status, response.Body.String())
		}
		deletionAssertExists(t, f.app, "notes", f.xhs.ID, true)
		deletionAssertExists(t, f.app, "notes", f.dy.ID, true)
	}
}

func TestDeleteActiveDescendantJobsNeverPartiallyMutates(t *testing.T) {
	for _, state := range []string{"running", "submission_unknown"} {
		t.Run(state, func(t *testing.T) {
			f := seedWorkspaceFixture(t)
			deletionPut(t, f.app, "jobs", "busy", domain.Job{ID: "busy", Kind: "publish_prepare", EntityID: f.crossPub.ID, State: state})
			before := map[string][]json.RawMessage{}
			for _, bucket := range deletionBuckets {
				rows, err := f.app.Store.List(bucket)
				if err != nil {
					t.Fatal(err)
				}
				before[bucket] = rows
			}
			for _, path := range []string{"/collections/" + f.xhs.ID + "?platform=xiaohongshu", "/workspaces/xiaohongshu"} {
				response := f.call(t, "DELETE", path, map[string]bool{"confirm": true}, true)
				if response.Code != 409 {
					t.Fatalf("busy deletion: HTTP %d %s", response.Code, response.Body.String())
				}
				for _, bucket := range deletionBuckets {
					after, _ := f.app.Store.List(bucket)
					if !reflect.DeepEqual(before[bucket], after) {
						t.Fatalf("partial mutation of %s", bucket)
					}
				}
				if _, err := os.Stat(filepath.Join(f.app.Data, f.xhsAsset.LocalPath)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDeleteSharedAssetPreservesRecordAndSurvivingHistory(t *testing.T) {
	f := seedWorkspaceFixture(t)
	f.dy.AssetIDs = append(f.dy.AssetIDs, f.xhsAsset.ID)
	deletionPut(t, f.app, "notes", f.dy.ID, f.dy)
	deletionPut(t, f.app, "jobs", "other-history", domain.Job{ID: "other-history", EntityID: f.dy.ID, AssetID: f.xhsAsset.ID, Kind: "ocr", State: "succeeded"})
	response := f.call(t, "DELETE", "/assets/"+f.xhsAsset.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if response.Code != 409 {
		t.Fatalf("in-use asset deletion: %d %s", response.Code, response.Body.String())
	}
	result := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	if result.RetainedAssets != 1 {
		t.Fatalf("retained shared asset: %+v", result)
	}
	deletionAssertExists(t, f.app, "assets", f.xhsAsset.ID, true)
	deletionAssertExists(t, f.app, "jobs", "other-history", true)
	var asset domain.Asset
	if err := f.app.Store.Get("assets", f.xhsAsset.ID, &asset); err != nil || asset.OwnerID != f.dy.ID {
		t.Fatalf("retained owner: %+v %v", asset, err)
	}
	if _, err := os.Stat(filepath.Join(f.app.Data, asset.LocalPath)); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRejectsUnsafePathsBeforeAnyMutation(t *testing.T) {
	for _, path := range []string{"../outside.txt", "workbench.db", "assets/../workbench.db"} {
		t.Run(path, func(t *testing.T) {
			f := seedWorkspaceFixture(t)
			f.xhsAsset.LocalPath = path
			deletionPut(t, f.app, "assets", f.xhsAsset.ID, f.xhsAsset)
			response := f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
			if response.Code != 409 {
				t.Fatalf("unsafe path: %d %s", response.Code, response.Body.String())
			}
			deletionAssertExists(t, f.app, "notes", f.xhs.ID, true)
			deletionAssertExists(t, f.app, "rewrites", f.xhsRewrite.ID, true)
			deletionAssertExists(t, f.app, "assets", f.xhsAsset.ID, true)
		})
	}
}

func TestDeleteRejectsSymlinkAncestorIncludingMissingLeaf(t *testing.T) {
	f := seedWorkspaceFixture(t)
	outside := t.TempDir()
	link := filepath.Join(f.app.Data, "assets", "linked")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Skip("symlink creation unavailable:", err)
		}
		// Directory junctions exercise Windows reparse-point safety without
		// requiring the administrator privilege needed for symbolic links.
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference = 'Stop'; New-Item -ItemType Junction -Path $env:XHS_DELETE_TEST_LINK -Target $env:XHS_DELETE_TEST_TARGET | Out-Null")
		cmd.Env = append(os.Environ(), "XHS_DELETE_TEST_LINK="+link, "XHS_DELETE_TEST_TARGET="+outside)
		if output, junctionErr := cmd.CombinedOutput(); junctionErr != nil {
			t.Skipf("symlink/junction creation unavailable: %v (%s)", junctionErr, output)
		}
	}
	f.xhsAsset.LocalPath = "assets/linked/missing.mp4"
	deletionPut(t, f.app, "assets", f.xhsAsset.ID, f.xhsAsset)
	response := f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if response.Code != 409 {
		t.Fatalf("symlink path: %d %s", response.Code, response.Body.String())
	}
	deletionAssertExists(t, f.app, "notes", f.xhs.ID, true)
}

func TestBatchDeleteIsAtomicAndDeduplicatesIDs(t *testing.T) {
	f := seedWorkspaceFixture(t)
	response := f.call(t, "DELETE", "/collections?platform=xiaohongshu", map[string]any{"ids": []string{f.xhs.ID, "absent"}, "confirm": true}, true)
	if response.Code != 404 {
		t.Fatalf("batch missing ID: %d %s", response.Code, response.Body.String())
	}
	deletionAssertExists(t, f.app, "notes", f.xhs.ID, true)
	result := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections?platform=xiaohongshu", map[string]any{"ids": []string{f.xhs.ID, f.xhs.ID, f.legacy.ID}, "confirm": true}, true), 200)
	if result.Deleted["notes"] != 2 {
		t.Fatal(result.Deleted)
	}
	deletionAssertExists(t, f.app, "notes", f.dy.ID, true)
}

func TestDeleteArtifactOnlyRemovesDescendants(t *testing.T) {
	for _, route := range []string{"rewrites", "video-projects", "publications"} {
		t.Run(route, func(t *testing.T) {
			f := seedWorkspaceFixture(t)
			id := f.xhsRewrite.ID
			if route == "video-projects" {
				id = f.xhsVideo.ID
				f.xhsPub.VideoProjectID = id
				deletionPut(t, f.app, "publications", f.xhsPub.ID, f.xhsPub)
			}
			if route == "publications" {
				id = f.xhsPub.ID
			}
			decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/"+route+"/"+id+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
			deletionAssertExists(t, f.app, "notes", f.xhs.ID, true)
			deletionAssertExists(t, f.app, "assets", f.xhsAsset.ID, true)
			deletionAssertExists(t, f.app, "rewrites", f.xhsRewrite.ID, route != "rewrites")
			deletionAssertExists(t, f.app, "videos", f.xhsVideo.ID, route == "publications")
			deletionAssertExists(t, f.app, "publications", f.xhsPub.ID, false)
			deletionAssertExists(t, f.app, "publications", f.dyPub.ID, true)
		})
	}
}

func TestDeleteUnusedAssetRemovesItsFile(t *testing.T) {
	f := seedWorkspaceFixture(t)
	asset := domain.Asset{ID: "unused", OwnerID: f.xhsRewrite.ID, LocalPath: "assets/unused.mp4"}
	deletionPut(t, f.app, "assets", asset.ID, asset)
	deletionMakeFile(t, f.app, asset.LocalPath)
	result := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/assets/unused?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	if result.Deleted["assets"] != 1 || result.FilesDeleted != 1 {
		t.Fatal(result)
	}
	deletionAssertExists(t, f.app, "rewrites", f.xhsRewrite.ID, true)
	if _, err := os.Stat(filepath.Join(f.app.Data, asset.LocalPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestDeletePreservesImageVoiceSceneReferences(t *testing.T) {
	f := seedWorkspaceFixture(t)
	f.dyVideo.Workflow = "image_voice"
	f.dyVideo.Scenes = []domain.Scene{{ID: "image-scene", ImageAssetID: f.xhsAsset.ID}}
	deletionPut(t, f.app, "videos", f.dyVideo.ID, f.dyVideo)
	result := decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/collections/"+f.xhs.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	if result.RetainedAssets != 1 {
		t.Fatal("scene image reference not retained", result)
	}
	var video domain.VideoProject
	if err := f.app.Store.Get("videos", f.dyVideo.ID, &video); err != nil || !reflect.DeepEqual(video, f.dyVideo) {
		t.Fatal("surviving scene mutated", err)
	}
	var asset domain.Asset
	if err := f.app.Store.Get("assets", f.xhsAsset.ID, &asset); err != nil || asset.OwnerID != f.dyVideo.ID {
		t.Fatalf("scene image not preserved with surviving lineage: %+v %v", asset, err)
	}
}

func TestDeletePreservesForeignLineageDespiteMalformedHistoricalPointers(t *testing.T) {
	f := seedWorkspaceFixture(t)
	f.dyPub.VideoProjectID = f.xhsVideo.ID
	deletionPut(t, f.app, "publications", f.dyPub.ID, f.dyPub)
	job := domain.Job{ID: "foreign-version-history", EntityID: f.dyVideo.ID, SourceVersionID: f.xhsRewrite.ConfirmedVersionID, State: "failed", Kind: "render"}
	deletionPut(t, f.app, "jobs", job.ID, job)
	decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/workspaces/xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	var publication domain.Publication
	if err := f.app.Store.Get("publications", f.dyPub.ID, &publication); err != nil || !reflect.DeepEqual(publication, f.dyPub) {
		t.Fatal("foreign publication lineage was overwritten/deleted", err)
	}
	var survivedJob domain.Job
	if err := f.app.Store.Get("jobs", job.ID, &survivedJob); err != nil || !reflect.DeepEqual(survivedJob, job) {
		t.Fatal("foreign job history was overwritten/deleted", err)
	}
}

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func installStyleFixture(t *testing.T, f apiFixture, entries []styleLibraryEntry, png []byte) {
	t.Helper()
	base := filepath.Join(f.app.Root, "style-library")
	if err := os.MkdirAll(filepath.Join(base, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "images", "reference.png"), png, 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"items": entries})
	if err := os.WriteFile(filepath.Join(base, "catalog.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMediaLibraryIndexesOnlyPersistedOutputsWithinWorkspace(t *testing.T) {
	f := seedWorkspaceFixture(t)
	put := func(bucket, id string, value any) {
		t.Helper()
		if err := f.app.Store.Put(bucket, id, value); err != nil {
			t.Fatal(err)
		}
	}
	makeAsset := func(id, owner, mime string) domain.Asset {
		asset := f.xhsAsset
		asset.ID, asset.OwnerID, asset.MIME, asset.LocalPath = id, owner, mime, filepath.Join("assets", id+".bin")
		if err := os.WriteFile(filepath.Join(f.app.Data, asset.LocalPath), f.png, 0600); err != nil {
			t.Fatal(err)
		}
		put("assets", id, asset)
		return asset
	}
	x := makeAsset("generated-xhs", f.xhsRewrite.ID, "image/png")
	d := makeAsset("generated-dy", f.dyRewrite.ID, "image/png")
	video := makeAsset("render-xhs", f.xhsVideo.ID, "video/mp4")
	oldScene := makeAsset("old-scene-xhs", f.xhsVideo.ID, "image/png")
	audio := makeAsset("voice-xhs", f.xhsVideo.ID, "audio/wav")
	missing := makeAsset("missing-xhs", f.xhsRewrite.ID, "image/png")
	_ = os.Remove(filepath.Join(f.app.Data, missing.LocalPath))
	xr := f.xhsRewrite
	xr.ImageRewrites = []domain.ImageRewrite{{VersionID: xr.ConfirmedVersionID, Revision: 2, Results: []domain.ImageResult{{AssetID: x.ID, Revision: 1}, {AssetID: x.ID, Revision: 1}, {AssetID: missing.ID}, {AssetID: d.ID}}}}
	dr := f.dyRewrite
	dr.ImageRewrites = []domain.ImageRewrite{{Results: []domain.ImageResult{{AssetID: d.ID}}}}
	xv := f.xhsVideo
	xv.Renders = []domain.RenderVersion{{AssetID: video.ID, Revision: 1}}
	xv.Scenes = []domain.Scene{{ImageSource: "generate", ImageAssetID: f.xhsAsset.ID}}
	put("rewrites", xr.ID, xr)
	put("rewrites", dr.ID, dr)
	put("videos", xv.ID, xv)
	put("jobs", "scene-history", domain.Job{ID: "scene-history", Kind: "image_voice", EntityID: xv.ID, State: "succeeded", ResultAssetIDs: []string{oldScene.ID, audio.ID}, Payload: json.RawMessage(`{"revision":1}`)})
	assertWorkspaceIDs(t, f.apiFixture, "/media-library?platform=xiaohongshu", x.ID, video.ID, oldScene.ID)
	// The retired Douyin workspace can no longer be selected at all.
	if r := f.call(t, "GET", "/media-library?platform=douyin", nil, false); r.Code != 400 {
		t.Fatalf("/media-library?platform=douyin returned %d, want 400", r.Code)
	}
	response := f.call(t, "GET", "/media-library?platform=xiaohongshu", nil, false)
	if strings.Contains(response.Body.String(), "local_path") || strings.Contains(response.Body.String(), f.app.Data) {
		t.Fatal("library exposes internal paths")
	}
	result := decodeResponse[struct {
		Items []mediaLibraryItem `json:"items"`
	}](t, response, 200)
	for _, item := range result.Items {
		if item.ID == x.ID && !item.Stale {
			t.Fatal("old result not marked historical")
		}
	}
	// Two projects in one workspace must not share their generated results,
	// even if a malformed historical scheme references the other project's file.
	other := xr
	other.ID = "another-xhs-project"
	otherAsset := makeAsset("another-xhs-image", other.ID, "image/png")
	other.ImageRewrites = []domain.ImageRewrite{{Results: []domain.ImageResult{{AssetID: otherAsset.ID}, {AssetID: x.ID}}}}
	put("rewrites", other.ID, other)
	assertWorkspaceIDs(t, f.apiFixture, "/media-library?platform=xiaohongshu&rewrite_id="+xr.ID, x.ID, video.ID, oldScene.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/media-library?platform=xiaohongshu&rewrite_id="+other.ID, otherAsset.ID)
	if r := f.call(t, "GET", "/media-library?platform=douyin&rewrite_id="+dr.ID, nil, false); r.Code != 400 {
		t.Fatalf("/media-library?platform=douyin returned %d, want 400", r.Code)
	}
	for _, path := range []string{
		"/media-library?platform=xiaohongshu&rewrite_id=" + dr.ID,
		"/media-library?rewrite_id=" + dr.ID,
		"/media-library?platform=xiaohongshu&rewrite_id=missing",
	} {
		if response := f.call(t, "GET", path, nil, false); response.Code != 404 {
			t.Fatalf("%s returned %d, want 404", path, response.Code)
		}
	}
}

func TestStyleCatalogueFiltersUnavailableFilesAndRejectsInvalidManifest(t *testing.T) {
	f := seedWorkspaceFixture(t)
	entry := styleLibraryEntry{ID: "clean-card", Title: "知识卡片", Category: "education", File: "images/reference.png", Tags: []string{"清晰"}, Credit: "测试素材"}
	entries := []styleLibraryEntry{entry, {ID: "outside", Title: "越界", Category: "art", File: "../data/workbench.db"}, {ID: "missing", Title: "缺失", Category: "food", File: "images/missing.png"}}
	installStyleFixture(t, f.apiFixture, entries, f.png)
	assertWorkspaceIDs(t, f.apiFixture, "/style-library?platform=xiaohongshu", entry.ID)
	response := f.call(t, "GET", "/style-library?platform=xiaohongshu", nil, false)
	if strings.Contains(response.Body.String(), `"file"`) || strings.Contains(response.Body.String(), "reference.png") {
		t.Fatal("catalogue leaks file paths")
	}
	file := f.call(t, "GET", "/style-library/clean-card/file?platform=xiaohongshu", nil, false)
	if file.Code != 200 || !bytes.Equal(file.Body.Bytes(), f.png) {
		t.Fatal("catalogue preview unavailable")
	}
	if f.call(t, "GET", "/style-library/outside/file?platform=xiaohongshu", nil, false).Code != 404 {
		t.Fatal("unsafe file served")
	}
	installStyleFixture(t, f.apiFixture, []styleLibraryEntry{entry, entry}, f.png)
	if f.call(t, "GET", "/style-library?platform=xiaohongshu", nil, false).Code != 500 {
		t.Fatal("duplicate catalogue IDs accepted")
	}
}

func TestStyleSelectionCreatesPrivateImmutableReferenceAndCascades(t *testing.T) {
	f := seedWorkspaceFixture(t)
	entry := styleLibraryEntry{ID: "clean-card", Title: "知识卡片", Category: "education", File: "images/reference.png"}
	installStyleFixture(t, f.apiFixture, []styleLibraryEntry{entry}, f.png)
	path := "/rewrites/" + f.xhsRewrite.ID + "/images?platform=xiaohongshu"
	body := map[string]any{"version_id": f.xhsRewrite.ConfirmedVersionID, "source_asset_ids": []string{f.xhsAsset.ID}, "style_library_id": entry.ID}
	if f.call(t, "POST", path, body, false).Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	unit := decodeResponse[domain.ImageRewrite](t, f.call(t, "POST", path, body, true), 201)
	if unit.StyleLibraryID != entry.ID || unit.StyleReferenceID == "" || unit.StyleReferenceTitle != entry.Title {
		t.Fatal("style selection not stored")
	}
	var snapshot domain.Asset
	if err := f.app.Store.Get("assets", unit.StyleReferenceID, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.OwnerID != f.xhsRewrite.ID || snapshot.Kind != "style_reference" || snapshot.SHA256 == "" {
		t.Fatal("reference not private snapshot")
	}
	// Removing the catalogue must not invalidate already created schemes.
	installStyleFixture(t, f.apiFixture, []styleLibraryEntry{}, []byte("replaced"))
	data, _, err := f.app.readVisualImage(f.xhsRewrite.ID, snapshot.ID)
	if err != nil || !bytes.Equal(data, f.png) {
		t.Fatal("catalogue modification altered saved reference", err)
	}
	if f.call(t, "GET", "/assets/"+snapshot.ID+"/file?platform=douyin", nil, false).Code != 400 {
		t.Fatal("snapshot crosses workspace")
	}
	assertWorkspaceIDs(t, f.apiFixture, "/media-library?platform=xiaohongshu")
	body["source_asset_ids"] = []string{snapshot.ID}
	body["style_library_id"] = ""
	if f.call(t, "POST", path, body, true).Code != 400 {
		t.Fatal("style snapshot accepted as source")
	}
	body["source_asset_ids"] = []string{f.xhsAsset.ID}
	body["style_library_id"] = "removed"
	if f.call(t, "POST", path, body, true).Code != 400 {
		t.Fatal("removed reference accepted")
	}
	response := f.call(t, "DELETE", "/rewrites/"+f.xhsRewrite.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(f.app.Data, snapshot.LocalPath)); !os.IsNotExist(err) {
		t.Fatal("reference snapshot not removed with project")
	}
}

func TestProjectLibraryRemovalKeepsHistoryAndPublicationFiles(t *testing.T) {
	f := seedWorkspaceFixture(t)
	asset := f.xhsAsset
	asset.ID, asset.OwnerID, asset.LocalPath = "library-result", f.xhsRewrite.ID, filepath.Join("assets", "library-result.png")
	if err := os.WriteFile(filepath.Join(f.app.Data, asset.LocalPath), f.png, 0600); err != nil {
		t.Fatal(err)
	}
	r := f.xhsRewrite
	r.ImageRewrites = []domain.ImageRewrite{{ID: "scheme", Results: []domain.ImageResult{{AssetID: asset.ID, Prompt: "immutable original"}}}}
	if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
		t.Fatal(err)
	}
	path := "/rewrites/" + r.ID + "/media-library/" + asset.ID
	// Generation has not changed any saved publication selection.
	var before domain.Rewrite
	_ = f.app.Store.Get("rewrites", r.ID, &before)
	for _, v := range before.Versions {
		if has(v.AssetIDs, asset.ID) {
			t.Fatal("unadopted result selected")
		}
	}
	assertWorkspaceIDs(t, f.apiFixture, "/media-library?platform=xiaohongshu&rewrite_id="+r.ID, asset.ID)
	if res := f.call(t, "DELETE", path+"?platform=xiaohongshu", map[string]bool{"confirm": true}, false); res.Code != 403 {
		t.Fatal("CSRF bypass", res.Code)
	}
	if res := f.call(t, "DELETE", path+"?platform=xiaohongshu", map[string]bool{"confirm": false}, true); res.Code != 400 {
		t.Fatal("confirmation bypass", res.Code)
	}
	if res := f.call(t, "DELETE", path+"?platform=douyin", map[string]bool{"confirm": true}, true); res.Code != 400 {
		t.Fatal("workspace leak", res.Code)
	}
	other := r
	other.ID = "other-library-project"
	_ = f.app.Store.Put("rewrites", other.ID, other)
	if res := f.call(t, "DELETE", "/rewrites/"+other.ID+"/media-library/"+asset.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true); res.Code != 404 {
		t.Fatal("project ownership bypass", res.Code)
	}
	for i := 0; i < 2; i++ {
		if res := f.call(t, "DELETE", path+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true); res.Code != 200 {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	assertWorkspaceIDs(t, f.apiFixture, "/media-library?platform=xiaohongshu&rewrite_id="+r.ID)
	var after domain.Rewrite
	_ = f.app.Store.Get("rewrites", r.ID, &after)
	b, _ := json.Marshal(before)
	result, _ := json.Marshal(after)
	if !bytes.Equal(b, result) {
		t.Fatal("removal changed generation or version history")
	}
	file := f.call(t, "GET", "/assets/"+asset.ID+"/file?platform=xiaohongshu", nil, false)
	if file.Code != 200 || !bytes.Equal(file.Body.Bytes(), f.png) {
		t.Fatal("history file lost")
	}
	if res := f.call(t, "DELETE", "/rewrites/"+r.ID+"/media-library/"+f.xhsAsset.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true); res.Code != 404 {
		t.Fatal("original note asset deleted from library")
	}
	version := r.Versions[0]
	body := map[string]any{"title": "新的标题", "body": "正文", "asset_ids": []string{asset.ID}, "parent_version_id": version.ID}
	// A publishing draft may already contain the image before library removal.
	// Its explicit selection must remain saveable, just like saved versions.
	if res := f.call(t, "POST", "/rewrites/"+r.ID+"/versions?platform=xiaohongshu", body, true); res.Code != 201 {
		t.Fatal("existing draft selection invalidated", res.Code, res.Body.String())
	}
	// Already saved content remains editable and publishable after removal.
	r.Versions[0].AssetIDs = []string{asset.ID}
	_ = f.app.Store.Put("rewrites", r.ID, r)
	if res := f.call(t, "POST", "/rewrites/"+r.ID+"/versions?platform=xiaohongshu", body, true); res.Code != 201 {
		t.Fatal("existing selection invalidated", res.Code, res.Body.String())
	}
}

func TestProjectLibrarySceneImageCanBeExplicitlySavedAsPublicationMedia(t *testing.T) {
	f := seedWorkspaceFixture(t)
	asset := f.xhsAsset
	asset.ID, asset.OwnerID, asset.LocalPath = "scene-library-image", f.xhsVideo.ID, filepath.Join("assets", "scene-library.png")
	if err := os.WriteFile(filepath.Join(f.app.Data, asset.LocalPath), f.png, 0600); err != nil {
		t.Fatal(err)
	}
	_ = f.app.Store.Put("assets", asset.ID, asset)
	v := f.xhsVideo
	v.Scenes = []domain.Scene{{ImageSource: "generate", ImageAssetID: asset.ID}}
	_ = f.app.Store.Put("videos", v.ID, v)
	body := map[string]any{"title": "图片发布测试", "body": "原正文", "asset_ids": []string{asset.ID}, "parent_version_id": f.xhsRewrite.Versions[0].ID}
	res := f.call(t, "POST", "/rewrites/"+f.xhsRewrite.ID+"/versions?platform=xiaohongshu", body, true)
	saved := decodeResponse[domain.Version](t, res, 201)
	if len(saved.AssetIDs) != 1 || saved.AssetIDs[0] != asset.ID {
		t.Fatal("library selection not saved")
	}
	res = f.call(t, "POST", "/rewrites/"+f.dyRewrite.ID+"/versions?platform=douyin", body, true)
	if res.Code != 400 {
		t.Fatal("foreign workspace asset accepted", res.Code)
	}
}

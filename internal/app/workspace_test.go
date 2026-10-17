package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"xhs-workbench/internal/domain"
)

type workspaceFixture struct {
	apiFixture
	xhs, dy, legacy         domain.Note
	xhsAsset, dyAsset       domain.Asset
	xhsRewrite, dyRewrite   domain.Rewrite
	xhsVideo, dyVideo       domain.VideoProject
	xhsPub, crossPub, dyPub domain.Publication
	png                     []byte
}

func seedWorkspaceFixture(t *testing.T) workspaceFixture {
	t.Helper()
	f := newAPIFixture(t)
	xhs, xa, png := seedSource(t, f)
	dy := xhs
	dy.ID, dy.Platform, dy.NoteID, dy.URL = "note-douyin", "douyin", "7364387149350702376", "https://www.douyin.com/video/7364387149350702376"
	da := xa
	da.ID, da.OwnerID = "asset-douyin", dy.ID
	dy.AssetIDs = []string{da.ID}
	legacy := xhs
	legacy.ID, legacy.Platform, legacy.AssetIDs = "note-legacy", "", []string{}
	xr := domain.Rewrite{ID: "rewrite-xhs", NoteID: xhs.ID, ConfirmedVersionID: "version-xhs", Versions: []domain.Version{{ID: "version-xhs", Title: "确认标题", Body: "确认文案", Script: "视频口播", Confirmed: true, Topics: []string{"测试"}, AssetIDs: []string{xa.ID}, SourceSnapshot: &xhs}}}
	dr := domain.Rewrite{ID: "rewrite-douyin", NoteID: dy.ID, ConfirmedVersionID: "version-douyin", Versions: []domain.Version{{ID: "version-douyin", Title: "抖音标题", Body: "抖音文案", Script: "视频口播", Confirmed: true, Topics: []string{"测试"}, AssetIDs: []string{da.ID}, SourceSnapshot: &dy}}}
	xv := domain.VideoProject{ID: "video-xhs", RewriteID: xr.ID, VersionID: xr.ConfirmedVersionID, Duration: 30, Revision: 1, Status: "draft", Scenes: []domain.Scene{}, Renders: []domain.RenderVersion{}}
	dv := xv
	dv.ID, dv.RewriteID, dv.VersionID = "video-douyin", dr.ID, dr.ConfirmedVersionID
	xp := domain.Publication{ID: "publication-legacy-xhs", RewriteID: xr.ID, VersionID: xr.ConfirmedVersionID, Title: "小红书发布稿", Body: "发布文案", AssetIDs: []string{xa.ID}, Topics: []string{}, Status: "ready"}
	cp := xp
	cp.ID, cp.TargetPlatform = "publication-historical-douyin", "douyin"
	dp := xp
	dp.ID, dp.TargetPlatform, dp.RewriteID, dp.VersionID, dp.AssetIDs = "publication-native-douyin", "douyin", dr.ID, dr.ConfirmedVersionID, []string{da.ID}
	put := func(bucket, id string, value any) {
		t.Helper()
		if err := f.app.Store.Put(bucket, id, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []domain.Note{dy, legacy} {
		put("notes", n.ID, n)
	}
	put("assets", da.ID, da)
	for _, r := range []domain.Rewrite{xr, dr} {
		put("rewrites", r.ID, r)
	}
	for _, v := range []domain.VideoProject{xv, dv} {
		put("videos", v.ID, v)
	}
	for _, p := range []domain.Publication{xp, cp, dp} {
		put("publications", p.ID, p)
	}
	for _, j := range []domain.Job{
		{ID: "job-xhs", Kind: "rewrite", EntityID: xr.ID, State: "failed", Payload: json.RawMessage(`{"private":"not-public"}`)},
		{ID: "job-douyin", Kind: "render", EntityID: dv.ID, State: "failed"},
		{ID: "job-historical-douyin", Kind: "publish_prepare", EntityID: cp.ID, State: "failed"},
		{ID: "job-asset-douyin", Kind: "ocr", AssetID: da.ID, State: "failed"},
		{ID: "job-legacy", Kind: "collect", EntityID: legacy.ID, State: "failed"},
	} {
		put("jobs", j.ID, j)
	}
	return workspaceFixture{f, xhs, dy, legacy, xa, da, xr, dr, xv, dv, xp, cp, dp, png}
}

func assertWorkspaceIDs(t *testing.T, f apiFixture, path string, expected ...string) {
	t.Helper()
	result := decodeResponse[struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}](t, f.call(t, "GET", path, nil, false), 200)
	ids := []string{}
	for _, item := range result.Items {
		ids = append(ids, item.ID)
	}
	expected = append([]string{}, expected...)
	sort.Strings(ids)
	sort.Strings(expected)
	if !reflect.DeepEqual(ids, expected) || result.Total != len(expected) {
		t.Fatalf("%s: ids %v total %d, want %v", path, ids, result.Total, expected)
	}
}

func TestWorkspaceListsFollowSourceAndPublicationTargetWithoutMigratingLegacy(t *testing.T) {
	f := seedWorkspaceFixture(t)
	assertWorkspaceIDs(t, f.apiFixture, "/collections?platform=xiaohongshu", f.xhs.ID, f.legacy.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/collections", f.xhs.ID, f.legacy.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/rewrites?platform=xiaohongshu", f.xhsRewrite.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/rewrites", f.xhsRewrite.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/video-projects?platform=xiaohongshu")
	assertWorkspaceIDs(t, f.apiFixture, "/publications?platform=xiaohongshu", f.xhsPub.ID)
	// A retired foreign target can no longer resolve a workspace at all.
	assertWorkspaceIDs(t, f.apiFixture, "/publications", f.xhsPub.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/assets?platform=xiaohongshu", f.xhsAsset.ID)
	assertWorkspaceIDs(t, f.apiFixture, "/jobs?platform=xiaohongshu", "job-xhs", "job-legacy")
	// The retired Douyin workspace can no longer be selected at all.
	for _, path := range []string{"/collections?platform=douyin", "/rewrites?platform=douyin", "/publications?platform=douyin", "/assets?platform=douyin", "/jobs?platform=douyin"} {
		if r := f.call(t, "GET", path, nil, false); r.Code != 400 {
			t.Fatalf("%s returned %d, want 400", path, r.Code)
		}
	}
	var legacy domain.Note
	if err := f.app.Store.Get("notes", f.legacy.ID, &legacy); err != nil || legacy.Platform != "" {
		t.Fatal("scope query altered legacy data")
	}
	response := f.call(t, "GET", "/jobs?platform=xiaohongshu", nil, false)
	if bytes.Contains(response.Body.Bytes(), []byte("not-public")) {
		t.Fatal("job list leaked private payload")
	}
	// A foreign snapshot never joins the Xiaohongshu workspace through a
	// rewrite whose live source note was already removed.
	archived := domain.Rewrite{ID: "archived-douyin", NoteID: "removed-note", Versions: []domain.Version{{SourceSnapshot: &f.dy}}}
	if err := f.app.Store.Put("rewrites", archived.ID, archived); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceIDs(t, f.apiFixture, "/rewrites?platform=xiaohongshu", f.xhsRewrite.ID)
}

func TestWorkspaceCrossDetailsAndMutationsCannotTouchOtherWorkspace(t *testing.T) {
	f := seedWorkspaceFixture(t)
	beforeJobs := len(list[domain.Job](f.app, "jobs"))
	for _, entry := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/collections/" + f.xhs.ID, nil},
		{"PATCH", "/collections/" + f.xhs.ID, map[string]any{"tags": []string{"changed"}}},
		{"POST", "/collections/" + f.xhs.ID + "/recollect", nil},
		{"POST", "/collections/" + f.xhs.ID + "/extract", nil},
		{"GET", "/rewrites/" + f.xhsRewrite.ID, nil},
		{"POST", "/rewrites/" + f.xhsRewrite.ID + "/confirm", map[string]string{"version_id": f.xhsRewrite.ConfirmedVersionID}},
		{"POST", "/rewrites/" + f.xhsRewrite.ID + "/generate", map[string]bool{"include_body": true}},
		{"GET", "/video-projects/" + f.xhsVideo.ID, nil},
		{"GET", "/publications/" + f.xhsPub.ID, nil},
		{"PATCH", "/publications/" + f.xhsPub.ID, map[string]string{"status": "published", "published_url": "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f"}},
		{"PATCH", "/assets/" + f.xhsAsset.ID, map[string]int{"position": 19}},
		{"PATCH", "/assets/" + f.xhsAsset.ID + "/text", map[string]string{"text": "changed"}},
		{"GET", "/jobs/job-xhs", nil},
		{"POST", "/jobs/job-xhs/retry", nil},
		{"POST", "/xhs/login", map[string]any{}},
	} {
		// The retired Douyin workspace is rejected before any handler runs, so
		// no cross-workspace detail read or mutation can execute at all.
		r := f.call(t, entry.method, entry.path+"?platform=douyin", entry.body, true)
		if r.Code != 400 {
			t.Fatalf("%s %s: HTTP %d body %s", entry.method, entry.path, r.Code, r.Body.String())
		}
	}
	if len(list[domain.Job](f.app, "jobs")) != beforeJobs {
		t.Fatal("cross-workspace write enqueued a job")
	}
	var note domain.Note
	f.app.Store.Get("notes", f.xhs.ID, &note)
	if !reflect.DeepEqual(note.Tags, f.xhs.Tags) {
		t.Fatal("cross-workspace note was modified")
	}
	var asset domain.Asset
	f.app.Store.Get("assets", f.xhsAsset.ID, &asset)
	if asset.Position != f.xhsAsset.Position || asset.Extraction.Text != f.xhsAsset.Extraction.Text {
		t.Fatal("cross-workspace asset was modified")
	}
	decodeResponse[domain.Note](t, f.call(t, "PATCH", "/collections/"+f.xhs.ID, map[string]any{"tags": []string{"legacy-compatible"}}, true), 200)
	decodeResponse[domain.Asset](t, f.call(t, "PATCH", "/assets/"+f.xhsAsset.ID+"?platform=xiaohongshu", map[string]int{"position": 2}, true), 200)
	if r := f.call(t, "GET", "/platforms/douyin/account?platform=xiaohongshu", nil, false); r.Code != 404 {
		t.Fatal("foreign account endpoint remained visible")
	}
}

func TestClearCompletedJobsOnlyRemovesFinishedJobsInCurrentWorkspace(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, job := range []domain.Job{
		{ID: "job-xhs-running", Kind: "ocr", EntityID: f.xhs.ID, State: "running"},
		{ID: "job-xhs-queued", Kind: "ocr", EntityID: f.xhs.ID, State: "queued"},
		{ID: "job-xhs-unknown", Kind: "image_voice", EntityID: f.xhsVideo.ID, State: "submission_unknown"},
	} {
		if err := f.app.Store.Put("jobs", job.ID, job); err != nil {
			t.Fatal(err)
		}
	}

	result := decodeResponse[struct {
		Deleted int `json:"deleted"`
	}](t, f.call(t, "DELETE", "/jobs/completed?platform=xiaohongshu", nil, true), 200)
	if result.Deleted != 2 {
		t.Fatalf("deleted %d jobs, want 2", result.Deleted)
	}
	assertWorkspaceIDs(t, f.apiFixture, "/jobs?platform=xiaohongshu", "job-xhs-running", "job-xhs-queued", "job-xhs-unknown")
	// Retired foreign jobs stay untouched by the current workspace cleanup.
	assertWorkspaceIDs(t, f.apiFixture, "/jobs", "job-xhs-running", "job-xhs-queued", "job-xhs-unknown")
	deletionAssertExists(t, f.app, "jobs", "job-douyin", true)
	deletionAssertExists(t, f.app, "jobs", "job-asset-douyin", true)

	if r := f.call(t, "DELETE", "/jobs/completed", nil, true); r.Code != 400 {
		t.Fatalf("clear without workspace: HTTP %d, body %s", r.Code, r.Body.String())
	}
}

func TestWorkspaceCollectionSubmissionRejectsForeignLinksBeforeCreatingJobs(t *testing.T) {
	f := newAPIFixture(t)
	const dy = "https://www.douyin.com/video/7364387149350702376"
	const xhs = "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f"
	// Only Xiaohongshu links are collected; a retired platform's link in the
	// same paste is ignored instead of creating foreign work.
	result := decodeResponse[struct {
		JobIDs   []string `json:"job_ids"`
		Rejected []any    `json:"rejected_items"`
	}](t, f.call(t, "POST", "/collections?platform=xiaohongshu", map[string]string{"text": xhs + "\n" + dy}, true), 202)
	if len(result.JobIDs) != 1 || len(result.Rejected) != 0 {
		t.Fatal("mixed paste did not collect exactly the Xiaohongshu link")
	}
	notes := list[domain.Note](f.app, "notes")
	if len(notes) != 1 || notes[0].Platform != "xiaohongshu" {
		t.Fatal("foreign note persisted")
	}
	before := len(list[domain.Job](f.app, "jobs"))
	for _, request := range []struct {
		path string
		body map[string]string
	}{
		{"/collections?platform=xiaohongshu", map[string]string{"text": dy}},
		{"/collections", map[string]string{"text": dy, "platform": "xiaohongshu"}},
		{"/collections", map[string]string{"text": xhs, "platform": "douyin"}},
		{"/collections", map[string]string{"text": xhs, "platform": "invalid"}},
		{"/collections?platform=douyin", map[string]string{"text": xhs}},
	} {
		if r := f.call(t, "POST", request.path, request.body, true); r.Code != 400 {
			t.Fatalf("foreign/conflicting scope returned %d", r.Code)
		}
	}
	if len(list[domain.Job](f.app, "jobs")) != before || len(list[domain.Note](f.app, "notes")) != 1 {
		t.Fatal("rejected URL created records or work")
	}
	decodeResponse[map[string]any](t, f.call(t, "POST", "/collections", map[string]string{"text": xhs, "platform": "xiaohongshu"}, true), 202)
	for _, note := range list[domain.Note](f.app, "notes") {
		if note.Platform == "xiaohongshu" {
			assertWorkspaceIDs(t, f, "/collections?platform=xiaohongshu", note.ID)
		}
	}
}

func TestWorkspaceCreateValidatesSourceTargetAndRejectsUnscopedBypass(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, req := range []struct {
		path   string
		body   any
		status int
	}{
		{"/rewrites?platform=douyin", map[string]string{"note_id": f.xhs.ID}, 400},
		{"/rewrites", map[string]string{"note_id": f.xhs.ID, "platform": "douyin"}, 400},
		{"/rewrites?platform=douyin", map[string]string{"note_id": f.dy.ID, "platform": "xiaohongshu"}, 400},
		{"/video-projects?platform=douyin", map[string]any{"rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID, "workflow": videoRewriteWorkflow}, 400},
		{"/publications?platform=douyin", map[string]string{"rewrite_id": f.dyRewrite.ID, "version_id": f.dyRewrite.ConfirmedVersionID, "target_platform": "xiaohongshu"}, 400},
		{"/publications?platform=douyin", map[string]string{"rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID, "target_platform": "douyin"}, 400},
		{"/publications", map[string]string{"rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID, "target_platform": "douyin"}, 400},
		{"/publications", map[string]string{"rewrite_id": f.dyRewrite.ID, "version_id": f.dyRewrite.ConfirmedVersionID, "target_platform": "xiaohongshu"}, 404},
	} {
		if r := f.call(t, "POST", req.path, req.body, true); r.Code != req.status {
			t.Fatalf("%s HTTP %d expected %d: %s", req.path, r.Code, req.status, r.Body.String())
		}
	}
	if len(list[domain.Rewrite](f.app, "rewrites")) != 2 || len(list[domain.VideoProject](f.app, "videos")) != 2 || len(list[domain.Publication](f.app, "publications")) != 3 {
		t.Fatal("denied creation left records")
	}
	// A retired foreign source note can no longer open a rewrite, scoped or not.
	r := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites", map[string]string{"note_id": f.xhs.ID}, true), 201)
	if r.NoteID != f.xhs.ID {
		t.Fatal("rewrite lost its source note")
	}
	p := decodeResponse[domain.Publication](t, f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]string{"rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID}, true), 201)
	if p.TargetPlatform != "xiaohongshu" {
		t.Fatal("omitted target did not default to active workspace")
	}
	p = decodeResponse[domain.Publication](t, f.call(t, "POST", "/publications", map[string]string{"rewrite_id": f.xhsRewrite.ID, "version_id": f.xhsRewrite.ConfirmedVersionID, "target_platform": "xiaohongshu"}, true), 201)
	if p.TargetPlatform != "xiaohongshu" || p.RewriteID != f.xhsRewrite.ID {
		t.Fatal("same-platform unscoped creation failed")
	}
}

func TestWorkspaceMixedHistoricalPublicationHiddenAndImmutable(t *testing.T) {
	f := seedWorkspaceFixture(t)
	beforeJobs := len(list[domain.Job](f.app, "jobs"))
	for _, scope := range []string{"", "?platform=xiaohongshu"} {
		for _, endpoint := range []struct {
			method, suffix string
			body           any
		}{
			{"GET", "", nil}, {"GET", "/export", nil}, {"POST", "/publish-prepare", map[string]string{"visibility": "private"}},
			{"GET", "/publish-preview", nil}, {"POST", "/publish-preview", map[string]string{"visibility": "private"}},
			{"POST", "/publish", map[string]string{"confirmation_token": "old-confirmation"}}, {"GET", "/publish-status", nil},
			{"PATCH", "", map[string]string{"status": "published", "published_url": "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f"}},
		} {
			r := f.call(t, endpoint.method, "/publications/"+f.crossPub.ID+endpoint.suffix+scope, endpoint.body, true)
			if r.Code != 404 {
				t.Fatalf("mixed publication %s %s: HTTP %d", endpoint.method, endpoint.suffix+scope, r.Code)
			}
		}
		if r := f.call(t, "GET", "/jobs/job-historical-douyin"+scope, nil, false); r.Code != 404 {
			t.Fatal("mixed publication job remained accessible")
		}
	}
	// The retired Douyin workspace is rejected before any handler runs.
	for _, request := range []struct {
		method, suffix string
	}{
		{"GET", ""}, {"GET", "/export"}, {"POST", "/publish-preview"}, {"POST", "/publish"},
	} {
		if r := f.call(t, request.method, "/publications/"+f.crossPub.ID+request.suffix+"?platform=douyin", map[string]string{"visibility": "private"}, true); r.Code != 400 {
			t.Fatalf("mixed publication %s %s?platform=douyin: HTTP %d", request.method, request.suffix, r.Code)
		}
	}
	if len(list[domain.Job](f.app, "jobs")) != beforeJobs {
		t.Fatal("hidden mixed draft created work")
	}
	var saved domain.Publication
	if err := f.app.Store.Get("publications", f.crossPub.ID, &saved); err != nil || !reflect.DeepEqual(saved, f.crossPub) {
		t.Fatal("hidden historical record was deleted or rewritten")
	}
	if r := f.call(t, "GET", "/assets/"+f.xhsAsset.ID+"/file?platform=douyin", nil, false); r.Code != 400 {
		t.Fatal("publication target reached the retired workspace")
	}
	asset := f.call(t, "GET", "/assets/"+f.xhsAsset.ID+"/file?platform=xiaohongshu", nil, false)
	if asset.Code != 200 || !bytes.Equal(asset.Body.Bytes(), f.png) {
		t.Fatal("native source asset was hidden or changed")
	}
	if r := f.call(t, "GET", "/rewrites/"+f.xhsRewrite.ID+"?platform=douyin", nil, false); r.Code != 400 {
		t.Fatal("publication reference exposed foreign rewrite editor")
	}
	if r := f.call(t, "PATCH", "/assets/"+f.xhsAsset.ID+"/text?platform=douyin", map[string]string{"text": "cross edit"}, true); r.Code != 400 {
		t.Fatal("publication reference allowed changing original OCR")
	}
}

func TestWorkspacePublicationRejectsForeignAssetAndVideoOwners(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, scenario := range []string{"asset", "video", "missing-owner"} {
		t.Run(scenario, func(t *testing.T) {
			p := f.dyPub
			p.ID = "mixed-" + scenario
			switch scenario {
			case "asset":
				p.AssetIDs = []string{f.xhsAsset.ID}
			case "video":
				p.VideoProjectID = f.xhsVideo.ID
			case "missing-owner":
				asset := f.dyAsset
				asset.ID = "asset-orphan"
				asset.OwnerID = "unknown-owner"
				f.app.Store.Put("assets", asset.ID, asset)
				p.AssetIDs = []string{asset.ID}
			}
			if err := f.app.Store.Put("publications", p.ID, p); err != nil {
				t.Fatal(err)
			}
			for _, scope := range []string{"", "?platform=xiaohongshu"} {
				if r := f.call(t, "GET", "/publications/"+p.ID+scope, nil, false); r.Code != 404 {
					t.Fatalf("mixed %s visible %s", scenario, scope)
				}
			}
			if r := f.call(t, "GET", "/publications/"+p.ID+"?platform=douyin", nil, false); r.Code != 400 {
				t.Fatalf("mixed %s reached the retired workspace", scenario)
			}
		})
	}
	assertWorkspaceIDs(t, f.apiFixture, "/publications?platform=xiaohongshu", f.xhsPub.ID)
	// A confirmed version with an inconsistent historical asset selection must
	// not bypass isolation through the original API without a platform query.
	r := f.dyRewrite
	r.Versions[0].AssetIDs = []string{f.xhsAsset.ID}
	if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
		t.Fatal(err)
	}
	before := len(list[domain.Publication](f.app, "publications"))
	response := f.call(t, "POST", "/publications", map[string]string{"rewrite_id": r.ID, "version_id": r.Versions[0].ID, "target_platform": "douyin"}, true)
	if response.Code != 400 || len(list[domain.Publication](f.app, "publications")) != before {
		t.Fatal("unscoped creation accepted a foreign asset")
	}
}

func TestWorkspacePublicationOwnedAssetsUseVerifiedSourceWithoutCycles(t *testing.T) {
	f := seedWorkspaceFixture(t)
	for _, p := range []domain.Publication{f.xhsPub, f.crossPub} {
		asset := f.xhsAsset
		asset.ID = "owned-" + p.ID
		asset.OwnerID = p.ID
		p.AssetIDs = []string{asset.ID}
		if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
			t.Fatal(err)
		}
		if err := f.app.Store.Put("publications", p.ID, p); err != nil {
			t.Fatal(err)
		}
		expected := 200
		if p.ID == f.crossPub.ID {
			expected = 404
		}
		if response := f.call(t, "GET", "/publications/"+p.ID+"?platform=xiaohongshu", nil, false); response.Code != expected {
			t.Fatalf("publication-owned media scope: HTTP %d want %d", response.Code, expected)
		}
		if response := f.call(t, "GET", "/assets/"+asset.ID+"/file?platform=xiaohongshu", nil, false); response.Code != expected {
			t.Fatalf("asset owner cycle or mixed source: HTTP %d want %d", response.Code, expected)
		}
	}
	// The retired Douyin workspace is rejected before any handler runs.
	if r := f.call(t, "GET", "/publications/"+f.xhsPub.ID+"?platform=douyin", nil, false); r.Code != 400 {
		t.Fatal("retired workspace remained selectable")
	}
}

func TestWorkspaceForeignOwnerUploadDoesNotCreateFileOrAsset(t *testing.T) {
	f := seedWorkspaceFixture(t)
	before, err := os.ReadDir(filepath.Join(f.app.Data, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("owner_id", f.xhsRewrite.ID); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("file", "uploaded.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(f.png); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/assets?platform=douyin", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", f.app.CSRF)
	r := httptest.NewRecorder()
	f.router.ServeHTTP(r, req)
	if r.Code != 400 {
		t.Fatalf("foreign owner upload HTTP %d: %s", r.Code, r.Body.String())
	}
	after, err := os.ReadDir(filepath.Join(f.app.Data, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) || len(list[domain.Asset](f.app, "assets")) != 2 {
		t.Fatal("foreign upload persisted a file or asset")
	}
}

func TestWorkspaceExtractionRejectsForeignAssetsBeforePartialJobs(t *testing.T) {
	f := seedWorkspaceFixture(t)
	// Preserve inconsistent old references without allowing cross-platform work.
	note := f.dy
	note.AssetIDs = append(note.AssetIDs, f.xhsAsset.ID)
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	before := len(list[domain.Job](f.app, "jobs"))
	r := f.call(t, "POST", "/collections/"+note.ID+"/extract?platform=douyin", map[string]any{"asset_ids": note.AssetIDs}, true)
	if r.Code != 400 || len(list[domain.Job](f.app, "jobs")) != before {
		t.Fatal("read-only exception allowed extraction jobs or partial writes")
	}
}

func TestWorkspaceRejectsInvalidOrAmbiguousScope(t *testing.T) {
	f := newAPIFixture(t)
	for _, path := range []string{"/collections?platform=weibo", "/collections?platform=douyin", "/rewrites?platform=douyin&platform=xiaohongshu", "/jobs?platform=douyin&platform=douyin"} {
		if r := f.call(t, "GET", path, nil, false); r.Code != 400 {
			t.Fatalf("%s returned %d", path, r.Code)
		}
	}
	assertWorkspaceIDs(t, f, "/rewrites")
}

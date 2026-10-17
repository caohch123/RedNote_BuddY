package app

import (
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestVideoV2SchemeOrderIsStableAcrossDetailInitializationAndEdits(t *testing.T) {
	f := seedWorkspaceFixture(t)
	first := domain.Asset{ID: "source-first", OwnerID: f.xhs.ID, Kind: "video", Position: 0, Status: "pending"}
	second := domain.Asset{ID: "source-second", OwnerID: f.xhs.ID, Kind: "video", Position: 2, Status: "pending"}
	for _, asset := range []domain.Asset{second, first} {
		if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
			t.Fatal(err)
		}
	}
	// Media positions, rather than query/update order, define source scheme order.
	f.xhs.AssetIDs = []string{second.ID, first.ID}
	if err := f.app.Store.Put("notes", f.xhs.ID, f.xhs); err != nil {
		t.Fatal(err)
	}
	makeProject := func(id, source, created string) domain.VideoProject {
		state := newVideoRewriteState(source)
		state.InitialSourceAssetID = source
		return domain.VideoProject{ID: id, RewriteID: f.xhsRewrite.ID, Workflow: videoRewriteWorkflow, Revision: 1, VideoRewrite: state, CreatedAt: created, UpdatedAt: created}
	}
	firstProject := makeProject("scheme-first", first.ID, "2026-10-14T01:00:00Z")
	firstProject.VideoRewrite.PrimaryAssetID = ""
	firstProject.VideoRewrite.PurposeSources = map[string]string{}
	secondProject := makeProject("scheme-second", second.ID, "2026-10-14T01:01:00Z")
	manualOld := makeProject("manual-old", "", "2026-10-14T00:00:00Z")
	manualNew := makeProject("manual-new", "", "2026-10-14T02:00:00Z")
	for _, project := range []domain.VideoProject{manualNew, secondProject, firstProject, manualOld} {
		if err := f.app.Store.Put("videos", project.ID, project); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{firstProject.ID, secondProject.ID, manualOld.ID, manualNew.ID}
	assertOrder := func(projects []domain.VideoProject, expected []string) {
		t.Helper()
		ids := []string{}
		for _, project := range projects {
			ids = append(ids, project.ID)
		}
		if !reflect.DeepEqual(ids, expected) {
			t.Fatalf("scheme order %v, want %v", ids, expected)
		}
	}
	for iteration := 0; iteration < 2; iteration++ {
		initialized := decodeResponse[[]domain.VideoProject](t, f.call(t, "POST", "/rewrites/"+f.xhsRewrite.ID+"/videos/initialize?platform=xiaohongshu", nil, true), 200)
		assertOrder(initialized, want)
		if initialized[0].VideoRewrite.PrimaryAssetID != "" {
			t.Fatal("initialization restored a user-removed primary")
		}
		detail := decodeResponse[struct {
			Videos []domain.VideoProject `json:"videos"`
		}](t, f.call(t, "GET", "/rewrites/"+f.xhsRewrite.ID+"?platform=xiaohongshu", nil, false), 200)
		assertOrder(detail.Videos, want)
		secondProject.UpdatedAt = "2027-01-01T00:00:00Z"
		if err := f.app.Store.Put("videos", secondProject.ID, secondProject); err != nil {
			t.Fatal(err)
		}
	}
}

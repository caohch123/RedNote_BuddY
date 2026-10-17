package app

import (
	"fmt"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestCreatingRewriteInitializesOneImageSchemePerReadyNoteImage(t *testing.T) {
	fixture := newAPIFixture(t)
	note, first, _ := seedSource(t, fixture)
	second := first
	second.ID, second.Name, second.Position = "asset-second-image", "second.png", 1
	if err := fixture.app.Store.Put("assets", second.ID, second); err != nil {
		t.Fatal(err)
	}
	nonImage := first
	nonImage.ID, nonImage.Kind, nonImage.MIME = "asset-video", "video", "video/mp4"
	if err := fixture.app.Store.Put("assets", nonImage.ID, nonImage); err != nil {
		t.Fatal(err)
	}
	unready := first
	unready.ID, unready.Status = "asset-unready", "failed"
	if err := fixture.app.Store.Put("assets", unready.ID, unready); err != nil {
		t.Fatal(err)
	}
	styleReference := first
	styleReference.ID, styleReference.Kind = "asset-style-reference", "style_reference"
	if err := fixture.app.Store.Put("assets", styleReference.ID, styleReference); err != nil {
		t.Fatal(err)
	}
	note.AssetIDs = []string{first.ID, second.ID, second.ID, nonImage.ID, unready.ID, styleReference.ID}
	if err := fixture.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}

	rewrite := decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 201)
	if len(rewrite.ImageRewrites) != 2 {
		t.Fatalf("got %d initial schemes, want one for each of 2 ready images", len(rewrite.ImageRewrites))
	}
	wantIDs := []string{first.ID, second.ID}
	for i, unit := range rewrite.ImageRewrites {
		if unit.WorkflowVersion != 2 || unit.VersionID != rewrite.Versions[0].ID || unit.PrimaryAssetID != wantIDs[i] || !reflect.DeepEqual(unit.SourceAssetIDs, []string{wantIDs[i]}) {
			t.Fatalf("scheme %d is not initialized for its note image: %+v", i, unit)
		}
		if unit.Status != "draft" || unit.Revision != 1 || len(unit.Results) != 0 || len(unit.Batches) != 0 {
			t.Fatalf("scheme %d unexpectedly started work: %+v", i, unit)
		}
		if len(unit.FeatureControls) != len(imageFlexibleFeatureNames) {
			t.Fatalf("scheme %d has incomplete default feature controls", i)
		}
	}
	if jobs := list[domain.Job](fixture.app, "jobs"); len(jobs) != 0 {
		t.Fatalf("initializing schemes must not enqueue generation work: %+v", jobs)
	}

	reloaded := decodeResponse[struct {
		Rewrite domain.Rewrite `json:"rewrite"`
	}](t, fixture.call(t, "GET", "/rewrites/"+rewrite.ID+"?platform=xiaohongshu", nil, false), 200)
	if len(reloaded.Rewrite.ImageRewrites) != 2 {
		t.Fatalf("reading rewrite changed or duplicated initialized schemes: %d", len(reloaded.Rewrite.ImageRewrites))
	}
}

func TestCreatingRewriteWithoutReadyImagesCreatesNoImageSchemes(t *testing.T) {
	fixture := newAPIFixture(t)
	note, asset, _ := seedSource(t, fixture)
	asset.Status = "failed"
	if err := fixture.app.Store.Put("assets", asset.ID, asset); err != nil {
		t.Fatal(err)
	}

	rewrite := decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 201)
	if len(rewrite.ImageRewrites) != 0 {
		t.Fatalf("expected no initial schemes without ready images, got %d", len(rewrite.ImageRewrites))
	}
}

func TestInitializeExistingImageSchemesIsOrderedAndIdempotent(t *testing.T) {
	f := newAPIFixture(t)
	note, source, _ := seedSource(t, f)
	note.AssetIDs = nil
	for i := 0; i < 14; i++ {
		asset := source
		asset.ID, asset.Position = fmt.Sprintf("original-%02d", i), i
		if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
			t.Fatal(err)
		}
		note.AssetIDs = append(note.AssetIDs, asset.ID)
	}
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	r := domain.Rewrite{ID: "existing-empty", NoteID: note.ID, Title: note.Title, UpdatedAt: "before", Versions: []domain.Version{{ID: "original-copy", Number: 1, Body: "保留文案", AssetIDs: []string{note.AssetIDs[3]}}}}
	if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
		t.Fatal(err)
	}
	path := "/rewrites/" + r.ID + "/images/initialize?platform=xiaohongshu"
	units := decodeResponse[[]domain.ImageRewrite](t, f.call(t, "POST", path, nil, true), 200)
	if len(units) != 14 {
		t.Fatalf("got %d schemes, want 14", len(units))
	}
	for i, unit := range units {
		if unit.PrimaryAssetID != note.AssetIDs[i] || unit.VersionID != r.Versions[0].ID || unit.Revision != 1 || unit.Status != "draft" || len(unit.Results)+len(unit.Analyses)+len(unit.Batches) != 0 {
			t.Fatalf("unexpected initialized scheme %d: %+v", i, unit)
		}
	}
	repeated := decodeResponse[[]domain.ImageRewrite](t, f.call(t, "POST", path, nil, true), 200)
	if !reflect.DeepEqual(repeated, units) {
		t.Fatal("retry replaced or duplicated initialized schemes")
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", r.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Versions, r.Versions) {
		t.Fatal("initialization modified copy or publishing selections")
	}
	if len(list[domain.Job](f.app, "jobs")) != 0 {
		t.Fatal("initialization enqueued model work")
	}
	// Even an edited scheme must be preserved, without creating missing schemes.
	saved.ImageRewrites = saved.ImageRewrites[:1]
	saved.ImageRewrites[0].Prompt, saved.ImageRewrites[0].Revision = "用户编辑", 7
	if err := f.app.Store.Put("rewrites", saved.ID, saved); err != nil {
		t.Fatal(err)
	}
	existing := decodeResponse[[]domain.ImageRewrite](t, f.call(t, "POST", path, nil, true), 200)
	if !reflect.DeepEqual(existing, saved.ImageRewrites) {
		t.Fatal("existing manual schemes were modified")
	}
}

func TestInitializeEmptyImagesCanRetryWhenImagesBecomeReady(t *testing.T) {
	f := newAPIFixture(t)
	note, asset, _ := seedSource(t, f)
	asset.Status = "pending"
	if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
		t.Fatal(err)
	}
	r := domain.Rewrite{ID: "pending-images", NoteID: note.ID, UpdatedAt: "before", Versions: []domain.Version{{ID: "source-version"}}}
	if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
		t.Fatal(err)
	}
	path := "/rewrites/" + r.ID + "/images/initialize?platform=xiaohongshu"
	if units := decodeResponse[[]domain.ImageRewrite](t, f.call(t, "POST", path, nil, true), 200); len(units) != 0 {
		t.Fatal("pending image received a scheme")
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", r.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.UpdatedAt != r.UpdatedAt {
		t.Fatal("empty attempt changed the project")
	}
	asset.Status = "ready"
	if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
		t.Fatal(err)
	}
	if units := decodeResponse[[]domain.ImageRewrite](t, f.call(t, "POST", path, nil, true), 200); len(units) != 1 || units[0].PrimaryAssetID != asset.ID {
		t.Fatal("ready image was not initialized on retry")
	}
}

func TestInitializeImagesRespectsWorkspaceAndCSRF(t *testing.T) {
	f := seedWorkspaceFixture(t)
	path := "/rewrites/" + f.xhsRewrite.ID + "/images/initialize"
	// The retired Douyin workspace is rejected before any handler runs.
	if response := f.call(t, "POST", path+"?platform=douyin", nil, true); response.Code != 400 {
		t.Fatalf("cross workspace status %d", response.Code)
	}
	if response := f.call(t, "POST", path+"?platform=xiaohongshu", nil, false); response.Code != 403 {
		t.Fatalf("missing CSRF status %d", response.Code)
	}
	if response := f.call(t, "POST", "/rewrites/"+f.dyRewrite.ID+"/images/initialize?platform=xiaohongshu", nil, true); response.Code != 404 {
		t.Fatalf("foreign project initialization status %d", response.Code)
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.ImageRewrites) != 0 {
		t.Fatal("rejected request modified project")
	}
}

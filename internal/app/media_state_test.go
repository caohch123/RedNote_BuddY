package app

import (
	"context"
	"testing"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
)

func TestDownloadCompletionPreservesConcurrentManualExtraction(t *testing.T) {
	fixture := newAPIFixture(t)
	_, asset, _ := seedSource(t, fixture)
	var downloaded domain.Asset
	if e := fixture.app.Store.Get("assets", asset.ID, &downloaded); e != nil {
		t.Fatal(e)
	}
	// The download holds an older copy while the user saves corrected text.
	decodeResponse[domain.Asset](t, fixture.call(t, "PATCH", "/assets/"+asset.ID+"/text", map[string]any{"text": "人工修正的文字", "segments": []domain.Segment{{Text: "人工修正", Start: 0, End: 1}}}, true), 200)
	downloaded.Status = "ready"
	downloaded.Duration = 3
	if e := fixture.app.saveMediaAsset(context.Background(), "download-fixture", &downloaded); e != nil {
		t.Fatal(e)
	}
	var saved domain.Asset
	if e := fixture.app.Store.Get("assets", asset.ID, &saved); e != nil {
		t.Fatal(e)
	}
	if saved.Status != "ready" || saved.Duration != 3 || saved.Extraction.Text != "人工修正的文字" || !saved.Extraction.Edited || len(saved.Extraction.Segments) != 1 {
		t.Fatalf("download completion lost concurrent manual edits: %+v", saved)
	}
}

func TestExtractionRetryKeepsManualTextUsable(t *testing.T) {
	f := newAPIFixture(t)
	note, v, _ := seedSource(t, f)
	v.Extraction = domain.Extraction{Status: "succeeded", Text: "本人修正的转写", Edited: true, Segments: []domain.Segment{{Text: "本人修正", Start: 0, End: 1}}}
	f.app.Store.Put("assets", v.ID, v)
	j := domain.Job{ID: "manual-retry", Kind: "ocr", AssetID: v.ID, EntityID: note.ID}
	f.app.Store.Put("jobs", j.ID, j)
	s, _ := f.app.settings()
	if err := f.app.extractJob(context.Background(), &j, providers.New(s), s); err != nil {
		t.Fatal(err)
	}
	var current domain.Asset
	f.app.Store.Get("assets", v.ID, &current)
	if current.Extraction.Status != "succeeded" || !current.Extraction.Edited || current.Extraction.Text != v.Extraction.Text || len(current.Extraction.Segments) != 1 {
		t.Fatalf("manual extraction changed: %+v", current.Extraction)
	}
}

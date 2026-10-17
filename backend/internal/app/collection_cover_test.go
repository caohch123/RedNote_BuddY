package app

import (
	"os"
	"path/filepath"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestCollectionCoverOnlyExposesReadyLocalImage(t *testing.T) {
	f := newAPIFixture(t)
	note, later, data := seedSource(t, f)
	first := domain.Asset{ID: "first-cover", OwnerID: note.ID, Kind: "image", MIME: "image/png", Status: "downloading", LocalPath: filepath.Join("assets", "first-cover.png")}
	note.AssetIDs = []string{first.ID, later.ID}
	note.CoverAssetID = first.ID
	note.Status, note.MediaStatus = "collecting", "running"
	if err := f.app.Store.Put("assets", first.ID, first); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	getList := func() domain.Note {
		result := decodeResponse[struct {
			Items []domain.Note `json:"items"`
		}](t, f.call(t, "GET", "/collections?platform=xiaohongshu", nil, false), 200)
		if len(result.Items) != 1 {
			t.Fatalf("unexpected notes: %+v", result.Items)
		}
		return result.Items[0]
	}
	if got := getList().CoverAssetID; got != later.ID {
		t.Fatalf("pending first image hid available fallback: %q", got)
	}
	// A missing local file is never advertised, even if a stale record says ready.
	later.LocalPath = filepath.Join("assets", "missing-image.png")
	if err := f.app.Store.Put("assets", later.ID, later); err != nil {
		t.Fatal(err)
	}
	if got := getList().CoverAssetID; got != "" {
		t.Fatalf("unavailable cover exposed: %q", got)
	}
	if err := os.WriteFile(filepath.Join(f.app.Data, first.LocalPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	first.Status = "ready"
	if err := f.app.Store.Put("assets", first.ID, first); err != nil {
		t.Fatal(err)
	}
	if got := getList().CoverAssetID; got != first.ID {
		t.Fatalf("downloaded cover not exposed: %q", got)
	}
	detail := decodeResponse[struct {
		Note domain.Note `json:"note"`
	}](t, f.call(t, "GET", "/collections/"+note.ID+"?platform=xiaohongshu", nil, false), 200)
	if detail.Note.CoverAssetID != first.ID {
		t.Fatalf("detail did not use available cover: %q", detail.Note.CoverAssetID)
	}
}

package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestMediaSelectionPersistsThroughVersionPublicationAndExport(t *testing.T) {
	for _, keepOne := range []bool{true, false} {
		name := "none"
		if keepOne {
			name = "one"
		}
		t.Run(name, func(t *testing.T) {
			f := newAPIFixture(t)
			note, first, _ := seedSource(t, f)
			second := first
			second.ID, second.Position = "second-image", 1
			if err := f.app.Store.Put("assets", second.ID, second); err != nil {
				t.Fatal(err)
			}
			note.AssetIDs = []string{first.ID, second.ID}
			if err := f.app.Store.Put("notes", note.ID, note); err != nil {
				t.Fatal(err)
			}
			rewrite := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 201)
			original := rewrite.Versions[0]
			// The same original and result can be referenced by several image schemes.
			rewrite.ImageRewrites = []domain.ImageRewrite{
				{ID: "scheme-one", SourceAssetIDs: []string{first.ID}, Results: []domain.ImageResult{{AssetID: second.ID}}},
				{ID: "scheme-two", SourceAssetIDs: []string{first.ID}, Results: []domain.ImageResult{{AssetID: second.ID}}},
			}
			if err := f.app.Store.Put("rewrites", rewrite.ID, rewrite); err != nil {
				t.Fatal(err)
			}
			type selectionDetail struct {
				Rewrite domain.Rewrite `json:"rewrite"`
				Assets  []domain.Asset `json:"assets"`
			}
			detail := decodeResponse[selectionDetail](t, f.call(t, "GET", "/rewrites/"+rewrite.ID+"?platform=xiaohongshu", nil, false), 200)
			if len(detail.Assets) != 2 || detail.Assets[0].ID != first.ID || detail.Assets[1].ID != second.ID {
				t.Fatal("candidate assets must be unique and retain their order")
			}
			selected := []string{}
			if keepOne {
				selected = append(selected, second.ID)
			}
			version := decodeResponse[domain.Version](t, f.call(t, "POST", "/rewrites/"+rewrite.ID+"/versions?platform=xiaohongshu", domain.Version{Title: "取消素材后的稿件", Body: "保留完整正文。", AssetIDs: selected, ParentVersionID: original.ID}, true), 201)
			if !reflect.DeepEqual(version.AssetIDs, selected) {
				t.Fatal("saving restored canceled assets")
			}
			detail = decodeResponse[selectionDetail](t, f.call(t, "GET", "/rewrites/"+rewrite.ID+"?platform=xiaohongshu", nil, false), 200)
			if !reflect.DeepEqual(detail.Rewrite.Versions[0].AssetIDs, original.AssetIDs) || !reflect.DeepEqual(detail.Rewrite.Versions[1].AssetIDs, selected) {
				t.Fatal("reloading changed selected assets or historical version")
			}
			decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites/"+rewrite.ID+"/confirm?platform=xiaohongshu", map[string]string{"version_id": version.ID}, true), 200)
			body := map[string]any{"rewrite_id": rewrite.ID, "version_id": version.ID, "asset_ids": original.AssetIDs, "target_platform": "xiaohongshu"}
			if response := f.call(t, "POST", "/publications?platform=xiaohongshu", body, true); response.Code != 409 {
				t.Fatal("outdated media selection must not create a publication")
			}
			if len(list[domain.Publication](f.app, "publications")) != 0 {
				t.Fatal("rejected selection still created a publication")
			}
			body["asset_ids"] = selected
			publication := decodeResponse[domain.Publication](t, f.call(t, "POST", "/publications?platform=xiaohongshu", body, true), 201)
			if !reflect.DeepEqual(publication.AssetIDs, selected) {
				t.Fatal("publication restored canceled assets")
			}
			exported := f.call(t, "GET", "/publications/"+publication.ID+"/export?platform=xiaohongshu", nil, true)
			if exported.Code != 200 {
				t.Fatalf("export failed: %s", exported.Body.String())
			}
			archive, err := zip.NewReader(bytes.NewReader(exported.Body.Bytes()), int64(exported.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				Assets []struct {
					ID string `json:"asset_id"`
				} `json:"assets"`
			}
			found := false
			for _, file := range archive.File {
				if file.Name != "manifest.json" {
					continue
				}
				reader, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				err = json.NewDecoder(reader).Decode(&manifest)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				found = true
			}
			if !found || len(manifest.Assets) != len(selected) {
				t.Fatal("export includes canceled media")
			}
			for i, asset := range manifest.Assets {
				if asset.ID != selected[i] {
					t.Fatal("export selection differs from version")
				}
			}
		})
	}
}

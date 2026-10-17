package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestIndependentImageUsesUneditedSourceAndKeepsPublishingTitleLimit(t *testing.T) {
	f := seedWorkspaceFixture(t)
	r := f.xhsRewrite
	r.Versions[0].Title = strings.Repeat("原", 25)
	r.Versions[0].Script = ""
	_ = f.app.Store.Put("rewrites", r.ID, r)
	result := f.xhsAsset
	result.ID, result.OwnerID, result.LocalPath = "independent-image", r.ID, "assets/independent-image.png"
	_ = os.WriteFile(filepath.Join(f.app.Data, result.LocalPath), f.png, 0600)
	_ = f.app.Store.Put("assets", result.ID, result)
	unit := domain.ImageRewrite{ID: "independent-scheme", VersionID: r.Versions[0].ID, SourceAssetIDs: []string{f.xhsAsset.ID}, Revision: 1, Results: []domain.ImageResult{{AssetID: result.ID, Revision: 1}}}
	r.ImageRewrites = []domain.ImageRewrite{unit}
	_ = f.app.Store.Put("rewrites", r.ID, r)
	v := decodeResponse[domain.Version](t, f.call(t, "POST", "/rewrites/"+r.ID+"/images/"+unit.ID+"/apply?platform=xiaohongshu", map[string]string{"asset_id": result.ID, "version_id": r.Versions[0].ID}, true), 201)
	if v.Title != r.Versions[0].Title || v.Body != r.Versions[0].Body || v.Script != "" || len(v.AssetIDs) != 1 || v.AssetIDs[0] != result.ID {
		t.Fatal("image-only operation changed source text or required a copy edit")
	}
	if resp := f.call(t, "POST", "/rewrites/"+r.ID+"/confirm?platform=xiaohongshu", map[string]string{"version_id": v.ID}, true); resp.Code != 400 {
		t.Fatal("image independence bypassed publishing title limit", resp.Code)
	}
	_ = f.app.Store.Get("rewrites", r.ID, &r)
	r.Versions[1].Confirmed = true
	_ = f.app.Store.Put("rewrites", r.ID, r)
	if resp := f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]string{"rewrite_id": r.ID, "version_id": v.ID}, true); resp.Code != 400 {
		t.Fatal("publication accepted an overlong title", resp.Code)
	}
}

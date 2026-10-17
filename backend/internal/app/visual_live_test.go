package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

// Explicit opt-in: at most one vision and one reference-image call.
// Read the live DPAPI settings without recovering or changing the live queue.
// No API key or upstream response body enters the test output or report.
func TestVisualLiveProviderSmoke(t *testing.T) {
	if os.Getenv("XHS_LIVE_VISUAL") != "1" {
		t.Skip("requires explicit opt-in; uses configured paid services")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "data", "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{Store: st, Root: root, Data: filepath.Join(root, "data")}
	settings, err := a.settings()
	if err != nil {
		t.Fatal("cannot read protected settings")
	}
	var source domain.Asset
	var data []byte
	for _, asset := range list[domain.Asset](a, "assets") {
		if asset.Size > 8<<20 || (asset.Kind != "image" && asset.Kind != "cover") || asset.Status != "ready" {
			continue
		}
		platform, ok := a.recordWorkspacePlatform("assets", asset.ID, map[string]bool{})
		if !ok || platform != "xiaohongshu" {
			continue
		}
		path, e := a.assetPath(asset)
		if e != nil {
			continue
		}
		picture, e := os.ReadFile(path)
		if e != nil || len(picture) > 10<<20 {
			continue
		}
		source, data = asset, picture
		break
	}
	if len(data) == 0 {
		t.Fatal("no ready source image for the live smoke test")
	}
	out := filepath.Join(a.Data, "verification", "visual-rewrite-20261009")
	onlyImage := os.Getenv("XHS_LIVE_VISUAL_IMAGE_ONLY") == "1"
	if base := os.Getenv("XHS_LIVE_VISUAL_IMAGE_BASE"); base != "" {
		p := settings.Providers["image"]
		p.BaseURL = base
		settings.Providers["image"] = p
	}
	if onlyImage {
		out = filepath.Join(a.Data, "verification", "visual-image-workspace-20261009")
	}
	if err = os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"source_asset_id": source.ID, "checked_at": time.Now().UTC().Format(time.RFC3339), "results": map[string]string{}}
	results := report["results"].(map[string]string)
	defer func() {
		body, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(filepath.Join(out, "result.json"), body, 0600)
	}()
	client := providers.New(settings)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if !onlyImage {
		features, err := client.AnalyzeImage(ctx, data)
		if err != nil {
			results["vision"] = a.safeError(err)
			t.Errorf("vision: %s", a.safeError(err))
		} else {
			results["vision"] = "passed"
			report["features"] = features
			t.Log("vision: passed")
		}

	}
	// A failed vision call still permits an independent protocol/access check,
	// using a fixed benign prompt rather than inventing feature extraction.
	image, mime, err := client.RewriteImage(ctx, "参考图仅参考构图；创作一张新的学习计划主题配图：暖白背景、蓝色线条、桌面上的笔记本与日历，简洁手绘，无文字。", [][]byte{data}, "3:4")
	if err != nil {
		results["image"] = a.safeError(err)
		t.Errorf("image: %s", a.safeError(err))
		return
	}
	_, ext := mediaType(mime)
	if ext == "" {
		t.Fatal("unsupported image output")
	}
	path := filepath.Join(out, "reference-example"+ext)
	if err = os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	results["image"] = "passed"
	report["image_file"] = filepath.Base(path)
	t.Log("image: passed")
}

package app

import (
	"image"
	"os"
	"strings"
	"xhs-workbench/internal/domain"
)

// Check source ownership even when an integration omits the platform query.
func (a *App) visualImageAllowed(rewriteID string, asset domain.Asset) bool {
	if asset.Status != "ready" || (asset.Kind != "image" && asset.Kind != "cover") || !strings.HasPrefix(asset.MIME, "image/") {
		return false
	}
	owner, found := a.recordWorkspacePlatform("rewrites", rewriteID, map[string]bool{})
	platform, valid := a.recordWorkspacePlatform("assets", asset.ID, map[string]bool{})
	if !found || !valid || owner != platform {
		return false
	}
	path, err := a.assetPath(asset)
	if err != nil {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	return err == nil && config.Width > 0 && config.Height > 0
}

func providerComplete(p domain.ProviderConfig) bool {
	return strings.TrimSpace(p.APIKey) != "" && strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.Model) != ""
}

package app

import (
	"log"
	"os"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

type mediaLibraryItem struct {
	ID             string       `json:"id"`
	Asset          domain.Asset `json:"asset"`
	RewriteID      string       `json:"rewrite_id"`
	ProjectTitle   string       `json:"project_title"`
	VersionID      string       `json:"version_id"`
	Origin         string       `json:"origin"`
	Revision       int          `json:"revision"`
	Model          string       `json:"model,omitempty"`
	CoverAssetID   string       `json:"cover_asset_id,omitempty"`
	PreviewAssetID string       `json:"preview_asset_id,omitempty"`
	CreatedAt      string       `json:"created_at"`
	Stale          bool         `json:"stale"`
}

// Index persisted outputs rather than copying files. Source downloads, style
// references, audio and unfinished jobs never masquerade as generated results.
func (a *App) routesMediaLibrary(g *gin.RouterGroup) {
	// Library removal is separate from physical asset deletion. Immutable
	// generation history and saved publications may still need the same file.
	g.DELETE("/rewrites/:id/media-library/:asset_id", func(c *gin.Context) {
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if !bind(c, &body) {
			return
		}
		if !body.Confirm {
			fail(c, 400, "请确认从素材库删除")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var asset domain.Asset
		if !a.get(c, "assets", c.Param("asset_id"), &asset) {
			return
		}
		isLibraryAsset, err := a.isProjectLibraryAsset(r, asset)
		if err != nil {
			log.Printf("list jobs for media library validation: %v", err)
			fail(c, 500, "读取素材库失败")
			return
		}
		if !isLibraryAsset {
			fail(c, 404, "当前项目素材库没有此素材")
			return
		}
		err = a.Store.Atomic(func(tx *store.Transaction) error {
			if err := tx.Get("assets", asset.ID, &asset); err != nil {
				return err
			}
			if asset.LibraryDeletedAt == "" {
				asset.LibraryDeletedAt = now()
			}
			return tx.Put("assets", asset.ID, asset)
		})
		if err != nil {
			fail(c, 500, "删除失败，请稍后重试")
			return
		}
		c.JSON(200, gin.H{"asset_id": asset.ID, "library_deleted_at": asset.LibraryDeletedAt, "history_preserved": true})
	})
	g.GET("/media-library", func(c *gin.Context) {
		a.mu.Lock()
		defer a.mu.Unlock()
		platform := workspacePlatform(c)
		if platform == "" {
			platform = "xiaohongshu"
		}
		rewriteID := strings.TrimSpace(c.Query("rewrite_id"))
		if rewriteID != "" {
			var rewrite domain.Rewrite
			if !a.get(c, "rewrites", rewriteID, &rewrite) {
				return
			}
			if p, ok := a.recordWorkspacePlatform("rewrites", rewriteID, map[string]bool{}); !ok || p != platform {
				c.JSON(404, gin.H{"error": "仿写项目不存在"})
				return
			}
		}
		items := []mediaLibraryItem{}
		seen := map[string]bool{}
		rewrites := map[string]domain.Rewrite{}
		add := func(r domain.Rewrite, item mediaLibraryItem) {
			if seen[item.ID] || item.ID == "" {
				return
			}
			var asset domain.Asset
			if a.Store.Get("assets", item.ID, &asset) != nil || asset.Status != "ready" || asset.Kind == "style_reference" || asset.LibraryDeletedAt != "" {
				return
			}
			if asset.OwnerID != r.ID {
				var owner domain.VideoProject
				if a.Store.Get("videos", asset.OwnerID, &owner) != nil || owner.RewriteID != r.ID {
					return
				}
			}
			p, ok := a.recordWorkspacePlatform("assets", asset.ID, map[string]bool{})
			if !ok || p != platform || (!strings.HasPrefix(asset.MIME, "image/") && !strings.HasPrefix(asset.MIME, "video/")) {
				return
			}
			path, err := a.assetPath(asset)
			if err != nil {
				return
			}
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				return
			}
			seen[item.ID] = true
			item.Asset = publicCollectionAsset(asset)
			item.RewriteID, item.ProjectTitle = r.ID, r.Title
			if item.CreatedAt == "" {
				item.CreatedAt = asset.CreatedAt
			}
			items = append(items, item)
		}
		rewriteItems, err := readList[domain.Rewrite](a, "rewrites")
		if err != nil {
			log.Printf("list rewrites: %v", err)
			fail(c, 500, "读取素材库失败")
			return
		}
		for _, r := range rewriteItems {
			if rewriteID != "" && r.ID != rewriteID {
				continue
			}
			p, ok := a.recordWorkspacePlatform("rewrites", r.ID, map[string]bool{})
			if !ok || p != platform {
				continue
			}
			rewrites[r.ID] = r
			for _, scheme := range r.ImageRewrites {
				for _, result := range scheme.Results {
					add(r, mediaLibraryItem{ID: result.AssetID, VersionID: scheme.VersionID, Origin: "image_rewrite", Revision: result.Revision, Model: result.Model, CreatedAt: result.CreatedAt, Stale: result.Revision != scheme.Revision})
				}
			}
		}
		videos := map[string]domain.VideoProject{}
		assetItems, err := readList[domain.Asset](a, "assets")
		if err != nil {
			log.Printf("list assets: %v", err)
			fail(c, 500, "读取素材库失败")
			return
		}
		for _, asset := range assetItems {
			if asset.SourceID != "upload" {
				continue
			}
			if r, ok := rewrites[asset.OwnerID]; ok {
				add(r, mediaLibraryItem{ID: asset.ID, Origin: "upload", CreatedAt: asset.CreatedAt})
			}
		}
		videoItems, err := readList[domain.VideoProject](a, "videos")
		if err != nil {
			log.Printf("list videos: %v", err)
			fail(c, 500, "读取素材库失败")
			return
		}
		for _, video := range videoItems {
			r, ok := rewrites[video.RewriteID]
			if !ok {
				continue
			}
			videos[video.ID] = video
			if video.VideoRewrite != nil {
				for _, result := range video.VideoRewrite.Results {
					add(r, mediaLibraryItem{ID: result.AssetID, VersionID: video.VersionID, Origin: "video_rewrite", Revision: result.Snapshot.Revision, Model: result.Snapshot.Model, CoverAssetID: result.CoverAssetID, PreviewAssetID: result.PreviewAssetID, CreatedAt: result.CreatedAt, Stale: result.Snapshot.Revision != video.Revision})
				}
			}
			for _, scene := range video.Scenes {
				if scene.ImageSource == "generate" {
					var asset domain.Asset
					if a.Store.Get("assets", scene.ImageAssetID, &asset) == nil && asset.OwnerID == video.ID {
						add(r, mediaLibraryItem{ID: scene.ImageAssetID, VersionID: video.VersionID, Origin: "video_image", Revision: video.Revision, Stale: video.Stale})
					}
				}
			}
			for _, render := range video.Renders {
				cover := ""
				if p, ok := a.recordWorkspacePlatform("assets", render.CoverAssetID, map[string]bool{}); ok && p == platform {
					cover = render.CoverAssetID
				}
				add(r, mediaLibraryItem{ID: render.AssetID, VersionID: video.VersionID, Origin: "video_render", Revision: render.Revision, CoverAssetID: cover, CreatedAt: render.CreatedAt, Stale: video.Stale || render.Revision != video.Revision})
			}
		}
		// Replacing a video scene keeps its previous generated image in the job
		// history. Index those files too, while excluding voice-only outputs.
		jobItems, err := readList[domain.Job](a, "jobs")
		if err != nil {
			log.Printf("list jobs: %v", err)
			fail(c, 500, "读取素材库失败")
			return
		}
		for _, job := range jobItems {
			if job.Kind != "image_voice" {
				continue
			}
			video, ok := videos[job.EntityID]
			if !ok {
				continue
			}
			for _, id := range job.ResultAssetIDs {
				var asset domain.Asset
				if a.Store.Get("assets", id, &asset) != nil || !strings.HasPrefix(asset.MIME, "image/") {
					continue
				}
				versionID := job.SourceVersionID
				if versionID == "" {
					versionID = video.VersionID
				}
				add(rewrites[video.RewriteID], mediaLibraryItem{ID: id, VersionID: versionID, Origin: "video_image", Revision: jobRevision(&job), CreatedAt: asset.CreatedAt, Stale: video.Stale || jobRevision(&job) != video.Revision})
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt == items[j].CreatedAt {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt > items[j].CreatedAt
		})
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
}

func (a *App) isProjectLibraryAsset(r domain.Rewrite, asset domain.Asset) (bool, error) {
	if asset.Kind == "style_reference" || (!strings.HasPrefix(asset.MIME, "image/") && !strings.HasPrefix(asset.MIME, "video/")) {
		return false, nil
	}
	if asset.OwnerID == r.ID {
		if asset.SourceID == "upload" {
			return true, nil
		}
		for _, scheme := range r.ImageRewrites {
			for _, result := range scheme.Results {
				if result.AssetID == asset.ID {
					return true, nil
				}
			}
		}
		return false, nil
	}
	var video domain.VideoProject
	if a.Store.Get("videos", asset.OwnerID, &video) != nil || video.RewriteID != r.ID {
		return false, nil
	}
	if video.VideoRewrite != nil {
		for _, result := range video.VideoRewrite.Results {
			if result.AssetID == asset.ID {
				return true, nil
			}
		}
	}
	for _, scene := range video.Scenes {
		if scene.ImageSource == "generate" && scene.ImageAssetID == asset.ID {
			return true, nil
		}
	}
	for _, render := range video.Renders {
		if render.AssetID == asset.ID {
			return true, nil
		}
	}
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		return false, err
	}
	for _, job := range jobs {
		if job.Kind == "image_voice" && job.EntityID == video.ID && has(job.ResultAssetIDs, asset.ID) && strings.HasPrefix(asset.MIME, "image/") {
			return true, nil
		}
	}
	return false, nil
}

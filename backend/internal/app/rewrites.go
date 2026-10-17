package app

import (
	"errors"
	"log"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func (a *App) routesRewrites(g *gin.RouterGroup) {
	a.routesImageRewrites(g)
	g.POST("/rewrites/:id/images/initialize", func(c *gin.Context) {
		a.mu.Lock()
		defer a.mu.Unlock()
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var n domain.Note
		if !a.getRewriteSource(c, r, &n) {
			return
		}
		if noteWorkspacePlatform(n) != "xiaohongshu" {
			fail(c, 400, "当前平台不支持图片仿写")
			return
		}
		// Existing schemes may contain manual edits or historical results. Only
		// initialize empty projects, and return the persisted IDs on retries.
		if len(r.ImageRewrites) > 0 {
			c.JSON(200, r.ImageRewrites)
			return
		}
		if len(r.Versions) == 0 {
			fail(c, 409, "仿写项目缺少文案版本")
			return
		}
		units, err := a.initialImageRewrites(n, r.Versions[0].ID)
		if err != nil {
			log.Printf("initialize existing image rewrites: %v", err)
			fail(c, 500, "初始化图片仿写方案失败")
			return
		}
		if len(units) == 0 {
			c.JSON(200, units)
			return
		}
		r.ImageRewrites, r.UpdatedAt = units, now()
		if a.put(c, "rewrites", r.ID, r) {
			c.JSON(200, units)
		}
	})
	g.GET("/rewrites", func(c *gin.Context) {
		rewrites, err := readList[domain.Rewrite](a, "rewrites")
		if err != nil {
			log.Printf("list rewrites: %v", err)
			fail(c, 500, "读取仿写项目失败")
			return
		}
		items := []domain.Rewrite{}
		for _, r := range rewrites {
			if a.workspaceAllows(c, "rewrites", r.ID, true) {
				items = append(items, r)
			}
		}
		// Pinning changes list placement, not the project's content edit time.
		// Use the saved content timestamp so unpinning restores its recency.
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Pinned != items[j].Pinned {
				return items[i].Pinned
			}
			return items[i].UpdatedAt > items[j].UpdatedAt
		})
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
	g.POST("/rewrites", func(c *gin.Context) {
		var b struct {
			NoteID   string `json:"note_id"`
			Platform string `json:"platform"`
			Blank    bool   `json:"blank"`
		}
		if !bind(c, &b) {
			return
		}
		if _, ok := bindWorkspacePlatform(c, b.Platform); !ok {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if b.Blank {
			if strings.TrimSpace(b.NoteID) != "" {
				fail(c, 400, "空白项目不能关联来源笔记")
				return
			}
			stamp := now()
			v := domain.Version{ID: ID(), Number: 1, Topics: []string{}, AssetIDs: []string{}, CreatedAt: stamp}
			controls := make(map[string]domain.ImageFeatureControl, len(imageFlexibleFeatureNames))
			for _, key := range imageFlexibleFeatureNames {
				controls[key] = domain.ImageFeatureControl{Mode: "auto"}
			}
			unit := domain.ImageRewrite{ID: ID(), VersionID: v.ID, WorkflowVersion: 2, EditIntent: domain.ImageEditIntent{Mode: "flexible"},
				SourceAssetIDs: []string{}, References: []domain.ImageReference{}, FeatureSources: map[string]string{}, FeatureControls: controls,
				Output: domain.ImageOutput{Ratio: "3:4", Clarity: "standard", Count: 1}, Style: domain.ImageStyle{Ratio: "3:4"}, TextMode: "none",
				Revision: 1, Status: "draft", Analyses: []domain.ImageAnalysis{}, Proposals: []domain.ImageProposal{}, Results: []domain.ImageResult{}, Batches: []domain.ImageBatch{}, CreatedAt: stamp, UpdatedAt: stamp}
			r := domain.Rewrite{ID: ID(), Blank: true, Title: "未命名项目", Versions: []domain.Version{v}, ImageRewrites: []domain.ImageRewrite{unit}, CreatedAt: stamp, UpdatedAt: stamp,
				Options: domain.RewriteOptions{IncludeBody: true, Tone: "自然分享", Length: "适中", ExtractionAssetIDs: []string{}}}
			if err := a.initializeVideoRewritesLocked(r, blankRewriteSource()); err != nil {
				log.Printf("create blank rewrite: %v", err)
				fail(c, 500, "创建空白项目失败")
				return
			}
			c.JSON(201, r)
			return
		}
		var n domain.Note
		if !a.get(c, "notes", b.NoteID, &n) {
			return
		}
		existing, err := readList[domain.Rewrite](a, "rewrites")
		if err != nil {
			log.Printf("read rewrites before adding: %v", err)
			fail(c, 500, "读取仿写项目失败")
			return
		}
		var latest domain.Rewrite
		count := 0
		for _, item := range existing {
			if item.NoteID != n.ID {
				continue
			}
			count++
			if latest.ID == "" || item.UpdatedAt > latest.UpdatedAt {
				latest = item
			}
		}
		if count >= 2 {
			c.JSON(200, latest)
			return
		}
		if n.MetadataStatus != "succeeded" {
			fail(c, 409, "请先完成笔记元数据采集")
			return
		}
		v := domain.Version{ID: ID(), Number: 1, Title: n.Title, Body: n.Body, Topics: n.Topics, Script: "", AssetIDs: append([]string{}, n.AssetIDs...), CreatedAt: now(), SourceSnapshot: &n}
		imageRewrites, err := a.initialImageRewrites(n, v.ID)
		if err != nil {
			log.Printf("initialize image rewrites: %v", err)
			fail(c, 500, "初始化图片仿写方案失败")
			return
		}
		r := domain.Rewrite{ID: ID(), NoteID: n.ID, Title: n.Title, Versions: []domain.Version{v}, ImageRewrites: imageRewrites, CreatedAt: now(), UpdatedAt: now(), Options: domain.RewriteOptions{IncludeBody: true, Tone: "自然分享", Length: "适中"}}
		if err := a.initializeVideoRewritesLocked(r, n); err != nil {
			log.Printf("initialize video rewrites: %v", err)
			fail(c, 500, "初始化视频仿写失败，请重试添加")
			return
		}
		c.JSON(201, r)
	})
	g.GET("/rewrites/:id", func(c *gin.Context) {
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var n domain.Note
		if isBlankRewrite(r) {
			n = blankRewriteSource()
		} else {
			a.Store.Get("notes", r.NoteID, &n)
		}
		ids := append([]string{}, n.AssetIDs...)
		for _, item := range r.ImageRewrites {
			ids = append(ids, item.SourceAssetIDs...)
			if item.StyleReferenceID != "" && !has(ids, item.StyleReferenceID) {
				ids = append(ids, item.StyleReferenceID)
			}
			for _, result := range item.Results {
				if !has(ids, result.AssetID) {
					ids = append(ids, result.AssetID)
				}
			}
		}
		for _, v := range r.Versions {
			for _, id := range v.AssetIDs {
				if !has(ids, id) {
					ids = append(ids, id)
				}
			}
		}
		assets, err := readList[domain.Asset](a, "assets")
		if err != nil {
			log.Printf("list assets: %v", err)
			fail(c, 500, "读取素材列表失败")
			return
		}
		for _, asset := range assets {
			if asset.OwnerID == r.ID && !has(ids, asset.ID) {
				ids = append(ids, asset.ID)
			}
		}
		videos := []domain.VideoProject{}
		projects, err := readList[domain.VideoProject](a, "videos")
		if err != nil {
			log.Printf("list videos: %v", err)
			fail(c, 500, "读取制作项目失败")
			return
		}
		for _, v := range projects {
			if v.RewriteID == r.ID && v.Workflow == videoRewriteWorkflow {
				videos = append(videos, v)
			}
		}
		sortVideoRewriteProjects(videos, a.assetsByIDs(n.AssetIDs))
		c.JSON(200, gin.H{"rewrite": r, "note": n, "assets": a.workspaceAssetsByIDs(c, ids), "videos": videos})
	})
	g.PATCH("/rewrites/:id", func(c *gin.Context) {
		var b struct {
			Title  *string `json:"title"`
			Pinned *bool   `json:"pinned"`
		}
		if !bind(c, &b) {
			return
		}
		if b.Title == nil && b.Pinned == nil {
			fail(c, 400, "请提供项目名称或置顶状态")
			return
		}
		if b.Title != nil {
			title := strings.TrimSpace(*b.Title)
			if title == "" || utf8.RuneCountInString(title) > 100 {
				fail(c, 400, "项目名称不能为空，且不能超过 100 个字符")
				return
			}
			b.Title = &title
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		if b.Title != nil {
			r.Title, r.TitleCustomized, r.UpdatedAt = *b.Title, true, now()
		}
		if b.Pinned != nil {
			r.Pinned = *b.Pinned
		}
		if a.put(c, "rewrites", r.ID, r) {
			c.JSON(200, r)
		}
	})
	g.POST("/rewrites/:id/generate", func(c *gin.Context) {
		var opts domain.RewriteOptions
		if !bind(c, &opts) {
			return
		}
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var n domain.Note
		if !a.getRewriteSource(c, r, &n) {
			return
		}
		if isBlankRewrite(r) && strings.TrimSpace(opts.Instructions) == "" {
			fail(c, 400, "请填写补充要求后生成文案")
			return
		}
		for _, id := range opts.ExtractionAssetIDs {
			if !has(n.AssetIDs, id) && (n.CoverAssetID == "" || id != n.CoverAssetID) {
				fail(c, 400, "提取内容不属于来源笔记")
				return
			}
		}
		opts, _, _ = a.rewriteInputs(n, opts)
		j, e := a.enqueue("rewrite", r.ID, "", "", opts)
		async(c, j, e)
	})
	g.POST("/rewrites/:id/versions", func(c *gin.Context) {
		var v domain.Version
		if !bind(c, &v) {
			return
		}
		if strings.TrimSpace(v.Title) == "" {
			fail(c, 400, "标题不能为空")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var n domain.Note
		if !a.getRewriteSource(c, r, &n) {
			return
		}
		if err := domain.ValidateNoteTitle(noteWorkspacePlatform(n), v.Title); err != nil {
			fail(c, 400, err.Error())
			return
		}
		if !a.validVersionAssets(c, r, n, v.AssetIDs) {
			return
		}
		v.ID = ID()
		v.SourceSnapshot = &n
		v.SourceExtractions = nil
		if len(r.Versions) > 0 {
			prior := r.Versions[len(r.Versions)-1]
			if v.ParentVersionID != "" {
				var ok bool
				prior, ok = getVersion(r, v.ParentVersionID)
				if !ok {
					fail(c, 400, "编辑来源版本不存在")
					return
				}
			} else {
				v.ParentVersionID = prior.ID
			}
			if prior.SourceSnapshot != nil {
				v.SourceSnapshot = prior.SourceSnapshot
			}
			v.SourceExtractions = prior.SourceExtractions
		}
		if isBlankRewrite(r) {
			v.SourceSnapshot, v.SourceExtractions = nil, nil
		}
		v.Number = len(r.Versions) + 1
		v.CreatedAt = now()
		v.Confirmed = false
		if v.Topics == nil {
			v.Topics = []string{}
		}
		if v.AssetIDs == nil {
			v.AssetIDs = []string{}
		}
		r.Versions = append(r.Versions, v)
		if !r.TitleCustomized {
			r.Title = v.Title
		}
		r.UpdatedAt = now()
		if !a.put(c, "rewrites", r.ID, r) {
			return
		}
		c.JSON(201, v)
	})
	g.POST("/rewrites/:id/confirm", func(c *gin.Context) {
		var b struct {
			VersionID string `json:"version_id"`
		}
		if !bind(c, &b) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var n domain.Note
		if !a.getRewriteSource(c, r, &n) {
			return
		}
		if v, ok := getVersion(r, b.VersionID); ok {
			if err := domain.ValidateNoteTitle(noteWorkspacePlatform(n), v.Title); err != nil {
				fail(c, 400, err.Error())
				return
			}
		}
		found := false
		for i := range r.Versions {
			if r.Versions[i].ID == b.VersionID {
				r.Versions[i].Confirmed = true
				found = true
			}
		}
		if !found {
			fail(c, 404, "版本不存在")
			return
		}
		r.ConfirmedVersionID = b.VersionID
		r.UpdatedAt = now()
		if a.put(c, "rewrites", r.ID, r) {
			c.JSON(200, r)
		}
	})
}

func isBlankRewrite(r domain.Rewrite) bool { return r.Blank && r.NoteID == "" }

func blankRewriteSource() domain.Note {
	return domain.Note{Platform: "xiaohongshu", Topics: []string{}, Tags: []string{}, AssetIDs: []string{}, MetadataStatus: "succeeded", MediaStatus: "succeeded"}
}

func (a *App) getRewriteSource(c *gin.Context, r domain.Rewrite, n *domain.Note) bool {
	if isBlankRewrite(r) {
		*n = blankRewriteSource()
		return true
	}
	return a.get(c, "notes", r.NoteID, n)
}

func (a *App) initialImageRewrites(n domain.Note, versionID string) ([]domain.ImageRewrite, error) {
	if noteWorkspacePlatform(n) != "xiaohongshu" {
		return []domain.ImageRewrite{}, nil
	}
	units := make([]domain.ImageRewrite, 0, len(n.AssetIDs))
	seen := make(map[string]bool, len(n.AssetIDs))
	for _, id := range n.AssetIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var asset domain.Asset
		if err := a.Store.Get("assets", id, &asset); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if asset.OwnerID != n.ID || asset.Status != "ready" || asset.LibraryDeletedAt != "" || asset.Kind == "style_reference" || !strings.HasPrefix(asset.MIME, "image/") {
			continue
		}
		features := make(map[string]string, len(imageFlexibleFeatureNames))
		controls := make(map[string]domain.ImageFeatureControl, len(imageFlexibleFeatureNames))
		for _, key := range imageFlexibleFeatureNames {
			features[key] = id
			controls[key] = domain.ImageFeatureControl{Mode: "image", AssetID: id}
		}
		units = append(units, domain.ImageRewrite{
			ID: ID(), VersionID: versionID, WorkflowVersion: 2, PrimaryAssetID: id,
			EditIntent:     domain.ImageEditIntent{Mode: "flexible"},
			SourceAssetIDs: []string{id}, FeatureSources: features, FeatureControls: controls,
			Output: domain.ImageOutput{Ratio: "3:4", Clarity: "standard", Count: 1},
			Style:  domain.ImageStyle{Ratio: "3:4"}, TextMode: "none",
			Revision: 1, Status: "draft", Analyses: []domain.ImageAnalysis{},
			Results: []domain.ImageResult{}, Batches: []domain.ImageBatch{},
			CreatedAt: now(), UpdatedAt: now(),
		})
	}
	return units, nil
}

// Resolve the current source at execution time so old selection settings and
// queued jobs cannot omit the full body or newly completed extraction text.
func (a *App) rewriteInputs(note domain.Note, opts domain.RewriteOptions) (domain.RewriteOptions, []string, map[string]domain.Extraction) {
	opts.IncludeBody = true
	opts.ExtractionAssetIDs = []string{}
	texts := []string{}
	extractions := map[string]domain.Extraction{}
	ids := append([]string{}, note.AssetIDs...)
	if note.CoverAssetID != "" && !has(ids, note.CoverAssetID) {
		ids = append(ids, note.CoverAssetID)
	}
	for _, id := range ids {
		if _, exists := extractions[id]; exists {
			continue
		}
		var asset domain.Asset
		if a.Store.Get("assets", id, &asset) != nil || (asset.Extraction.Status != "succeeded" && !asset.Extraction.Edited) {
			continue
		}
		text := asset.Extraction.Text
		if strings.TrimSpace(text) == "" {
			parts := []string{}
			for _, segment := range asset.Extraction.Segments {
				if strings.TrimSpace(segment.Text) != "" {
					parts = append(parts, segment.Text)
				}
			}
			text = strings.Join(parts, "\n")
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		opts.ExtractionAssetIDs = append(opts.ExtractionAssetIDs, id)
		texts = append(texts, text)
		extractions[id] = asset.Extraction
	}
	return opts, texts, extractions
}
func (a *App) validVersionAssets(c *gin.Context, r domain.Rewrite, n domain.Note, ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			fail(c, 400, "素材不能重复")
			return false
		}
		seen[id] = true
		var asset domain.Asset
		if !a.get(c, "assets", id, &asset) {
			return false
		}
		if asset.Kind == "style_reference" {
			fail(c, 400, "风格参考快照不能作为来源或发布素材")
			return false
		}
		allowed := asset.OwnerID == r.ID || asset.OwnerID == n.ID
		// Generated scene images are owned by a video project, but can be
		// explicitly selected from this rewrite's material library.
		libraryAsset, err := a.isProjectLibraryAsset(r, asset)
		if err != nil {
			log.Printf("list jobs for media library validation: %v", err)
			fail(c, 500, "读取素材库失败")
			return false
		}
		allowed = allowed || libraryAsset
		for _, v := range r.Versions {
			allowed = allowed || has(v.AssetIDs, id)
		}
		if !allowed {
			fail(c, 400, "素材不属于当前仿写项目")
			return false
		}
	}
	return true
}
func getVersion(r domain.Rewrite, id string) (domain.Version, bool) {
	for _, v := range r.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return domain.Version{}, false
}

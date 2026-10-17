package app

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"xhs-workbench/internal/domain"
)

func (a *App) routesPublications(g *gin.RouterGroup) {
	g.GET("/publications", func(c *gin.Context) {
		items, err := readList[domain.Publication](a, "publications")
		if err != nil {
			log.Printf("list publications: %v", err)
			fail(c, 500, "读取发布记录失败")
			return
		}
		filtered := []domain.Publication{}
		for _, p := range items {
			if !a.workspaceAllows(c, "publications", p.ID, true) {
				continue
			}
			p.TargetPlatform = publicationPlatform(p)
			if c.Query("platform") == "" || c.Query("platform") == p.TargetPlatform {
				filtered = append(filtered, p)
			}
		}
		items = filtered
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
	g.GET("/publications/:id", func(c *gin.Context) {
		var p domain.Publication
		if a.get(c, "publications", c.Param("id"), &p) {
			p.TargetPlatform = publicationPlatform(p)
			c.JSON(200, p)
		}
	})
	g.POST("/publications", a.createPublication)
	g.GET("/publications/:id/export", a.exportPublication)
	g.PATCH("/publications/:id", func(c *gin.Context) {
		var b struct {
			Status       string `json:"status"`
			PublishedURL string `json:"published_url"`
			PublishedAt  string `json:"published_at"`
		}
		if !bind(c, &b) {
			return
		}
		if b.Status != "published" {
			fail(c, 400, "仅支持手动记录已发布状态")
			return
		}
		var existing domain.Publication
		if !a.get(c, "publications", c.Param("id"), &existing) {
			return
		}
		if !validPublicationURL(b.PublishedURL, publicationPlatform(existing)) {
			fail(c, 400, "请填写发布稿目标平台的有效作品链接")
			return
		}
		if b.PublishedAt == "" {
			b.PublishedAt = now()
		}
		if _, e := time.Parse(time.RFC3339, b.PublishedAt); e != nil {
			fail(c, 400, "发布时间格式无效")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var p domain.Publication
		if a.get(c, "publications", c.Param("id"), &p) {
			if p.Status == "publishing" || p.Status == "preparing_publish" {
				fail(c, 409, "发布正在执行，请等待任务结果")
				return
			}
			p.Status = "published"
			p.PublishedURL = b.PublishedURL
			p.PublishedAt = b.PublishedAt
			p.PublishError = ""
			if p.PublishMethod == "" {
				p.PublishMethod = "manual"
			}
			if a.put(c, "publications", p.ID, p) {
				c.JSON(200, p)
			}
		}
	})
}
func (a *App) createPublication(c *gin.Context) {
	var b struct {
		RewriteID      string    `json:"rewrite_id"`
		VersionID      string    `json:"version_id"`
		VideoProjectID string    `json:"video_project_id"`
		TargetPlatform string    `json:"target_platform"`
		AssetIDs       *[]string `json:"asset_ids"`
	}
	if !bind(c, &b) {
		return
	}
	if b.TargetPlatform == "" {
		b.TargetPlatform = workspacePlatform(c)
		if b.TargetPlatform == "" {
			b.TargetPlatform = "xiaohongshu"
		}
	}
	if b.TargetPlatform != "xiaohongshu" {
		fail(c, 400, "目标平台请选择小红书")
		return
	}
	if scope := workspacePlatform(c); scope != "" && b.TargetPlatform != scope {
		fail(c, 400, "发布稿目标平台与当前工作区不一致")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var r domain.Rewrite
	if !a.get(c, "rewrites", b.RewriteID, &r) {
		return
	}
	v, ok := getVersion(r, b.VersionID)
	if !ok || !v.Confirmed {
		fail(c, 409, "请先确认此仿写版本")
		return
	}
	if err := domain.ValidateNoteTitle(b.TargetPlatform, v.Title); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if b.AssetIDs != nil && !slices.Equal(*b.AssetIDs, v.AssetIDs) {
		fail(c, 409, "发布素材与确认版本不一致，请保存并确认当前素材选择")
		return
	}
	p := domain.Publication{ID: ID(), RewriteID: r.ID, VersionID: v.ID, Title: v.Title, Body: v.Body, Topics: append([]string{}, v.Topics...), AssetIDs: append([]string{}, v.AssetIDs...), CreatedAt: now(), Status: "ready", VideoProjectID: b.VideoProjectID}
	p.TargetPlatform = b.TargetPlatform
	if b.VideoProjectID != "" {
		fail(c, 409, "请从素材库加入视频或采用视频，再保存并确认发布素材")
		return
	}
	if len(p.AssetIDs) == 0 {
		p.Status = "preparing"
	}
	for _, id := range p.AssetIDs {
		var asset domain.Asset
		if a.Store.Get("assets", id, &asset) != nil || asset.Status != "ready" {
			p.Status = "preparing"
		}
	}
	if _, valid := a.publicationWorkspacePlatform(p, map[string]bool{}); !valid {
		fail(c, 400, "发布稿的来源、视频和素材必须全部属于目标平台，不能跨工作区创建")
		return
	}
	if a.put(c, "publications", p.ID, p) {
		c.JSON(201, p)
	}
}
func (a *App) exportPublication(c *gin.Context) {
	if c.GetHeader("X-CSRF-Token") != a.CSRF {
		fail(c, 403, "请从工作台下载发布包")
		return
	}
	var p domain.Publication
	if !a.get(c, "publications", c.Param("id"), &p) {
		return
	}
	type media struct {
		asset      domain.Asset
		path, name string
	}
	files := []media{}
	for i, id := range p.AssetIDs {
		var asset domain.Asset
		if !a.get(c, "assets", id, &asset) {
			return
		}
		if asset.Status != "ready" {
			fail(c, 409, "发布素材尚未保存，请重新准备")
			return
		}
		path, e := a.assetPath(asset)
		if e != nil {
			fail(c, 409, "发布素材缺失，请重新准备")
			return
		}
		files = append(files, media{asset, path, fmt.Sprintf("%02d_%s%s", i+1, asset.Kind, filepath.Ext(path))})
	}
	tmp, e := os.CreateTemp(filepath.Join(a.Data, "exports"), "publish-*.zip")
	if e != nil {
		fail(c, 500, "无法创建发布包")
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	zw := zip.NewWriter(tmp)
	addText := func(name, text string) error {
		w, e := zw.Create(name)
		if e != nil {
			return e
		}
		_, e = io.WriteString(w, text)
		return e
	}
	topics := []string{}
	for _, t := range p.Topics {
		topics = append(topics, "#"+strings.TrimPrefix(t, "#"))
	}
	e = addText("文案.txt", p.Title+"\n\n"+p.Body+"\n\n"+strings.Join(topics, " "))
	if e == nil {
		e = addText("标题.txt", p.Title)
	}
	manifest := []map[string]any{}
	for _, f := range files {
		if e != nil {
			break
		}
		header := &zip.FileHeader{Name: f.name, Method: zip.Store}
		var w io.Writer
		w, e = zw.CreateHeader(header)
		if e == nil {
			var in *os.File
			in, e = os.Open(f.path)
			if e == nil {
				_, e = io.Copy(w, in)
				in.Close()
			}
		}
		manifest = append(manifest, map[string]any{"file": f.name, "asset_id": f.asset.ID, "kind": f.asset.Kind, "sha256": f.asset.SHA256})
	}
	if e == nil {
		b, _ := json.MarshalIndent(map[string]any{"publication_id": p.ID, "target_platform": publicationPlatform(p), "rewrite_version_id": p.VersionID, "video_revision": p.VideoRevision, "title": p.Title, "topics": p.Topics, "assets": manifest}, "", "  ")
		e = addText("manifest.json", string(b))
	}
	if ce := zw.Close(); e == nil {
		e = ce
	}
	if ce := tmp.Close(); e == nil {
		e = ce
	}
	if e != nil {
		fail(c, 500, "创建发布包失败")
		return
	}
	a.mu.Lock()
	var current domain.Publication
	if a.Store.Get("publications", p.ID, &current) == nil && (current.Status == "ready" || current.Status == "preparing" || current.Status == "exported") {
		current.Status = "exported"
		a.Store.Put("publications", p.ID, current)
	}
	a.mu.Unlock()
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="publication-%s.zip"`, p.ID))
	c.File(tmpPath)
}

func publicationPlatform(p domain.Publication) string {
	if p.TargetPlatform == "" {
		return "xiaohongshu"
	}
	return p.TargetPlatform
}

func validPublicationURL(raw, platform string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" {
		return false
	}
	if platform != "xiaohongshu" {
		return false
	}
	if u.Hostname() == "xhslink.com" || u.Hostname() == "xhslink.cn" {
		return len(strings.Trim(u.Path, "/")) > 0
	}
	if u.Hostname() != "www.xiaohongshu.com" && u.Hostname() != "xiaohongshu.com" {
		return false
	}
	return strings.HasPrefix(u.Path, "/explore/") && xhsNoteIDPattern.MatchString(strings.TrimPrefix(u.Path, "/explore/")) || strings.HasPrefix(u.Path, "/discovery/item/") && xhsNoteIDPattern.MatchString(strings.TrimPrefix(u.Path, "/discovery/item/"))
}

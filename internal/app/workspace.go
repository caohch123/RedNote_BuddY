package app

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"xhs-workbench/internal/domain"
)

const workspaceContextKey = "workspace_platform"

func validWorkspacePlatform(platform string) bool {
	return platform == "xiaohongshu"
}
func workspacePlatform(c *gin.Context) string { return c.GetString(workspaceContextKey) }
func noteWorkspacePlatform(n domain.Note) string {
	if n.Platform == "" {
		return "xiaohongshu"
	}
	return n.Platform
}

// Scope applies to actual source ownership. A publication's target label never
// grants another workspace access to its source or media, even for old drafts.
func (a *App) workspaceMiddleware(c *gin.Context) {
	values := c.Request.URL.Query()["platform"]
	if len(values) > 1 {
		fail(c, 400, "工作区平台参数只能指定一次")
		return
	}
	platform := c.Query("platform")
	if platform != "" && !validWorkspacePlatform(platform) {
		fail(c, 400, "工作区平台请选择小红书")
		return
	}
	if platform != "" {
		c.Set(workspaceContextKey, platform)
	}
	path := strings.TrimPrefix(c.FullPath(), "/api/v1/")
	parts := strings.Split(path, "/")
	// Registry management stays reachable after deleting the last workspace.
	// Scoped requests and account routes cannot reopen a removed workspace.
	registryPlatform := platform
	if registryPlatform == "" && parts[0] == "xhs" {
		registryPlatform = "xiaohongshu"
	}
	if parts[0] != "workspaces" && registryPlatform != "" {
		active, err := workspaceRegistryActive(a.Store, registryPlatform)
		if err != nil {
			fail(c, 500, "读取工作区失败")
			return
		}
		if !active {
			fail(c, 404, "工作区已删除，请先添加工作区")
			return
		}
	}
	bucket := ""
	switch parts[0] {
	case "collections":
		bucket = "notes"
	case "rewrites":
		bucket = "rewrites"
	case "video-projects":
		bucket = "videos"
	case "publications":
		bucket = "publications"
	case "assets":
		bucket = "assets"
	case "jobs":
		bucket = "jobs"
	case "xhs":
		if platform != "" && platform != "xiaohongshu" {
			fail(c, 404, "当前工作区没有此账号入口")
			return
		}
	}
	if bucket != "" && c.Param("id") != "" && !a.workspaceAllows(c, bucket, c.Param("id"), c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) {
		fail(c, 404, "当前工作区没有此记录")
		return
	}
	c.Next()
}

// Body-only scope is supported for integrations that submit without a query.
// Two explicit, contradictory choices must never silently pick a workspace.
func bindWorkspacePlatform(c *gin.Context, body string) (string, bool) {
	scope := workspacePlatform(c)
	if body != "" && !validWorkspacePlatform(body) {
		fail(c, 400, "工作区平台请选择小红书")
		return "", false
	}
	if scope != "" && body != "" && scope != body {
		fail(c, 400, "请求平台与当前工作区不一致")
		return "", false
	}
	if scope == "" && body != "" {
		scope = body
		c.Set(workspaceContextKey, scope)
	}
	return scope, true
}

func (a *App) workspaceAllows(c *gin.Context, bucket, id string, _ bool) bool {
	scope := workspacePlatform(c)
	platform, found := a.recordWorkspacePlatform(bucket, id, map[string]bool{})
	if !found || !validWorkspacePlatform(platform) {
		return false
	}
	return scope == "" || platform == scope
}

func workspaceAncestors(seen map[string]bool) map[string]bool {
	out := make(map[string]bool, len(seen))
	for key, value := range seen {
		out[key] = value
	}
	return out
}

// Validate every branch independently: the same source can legitimately own
// both a rewrite and several selected assets. Ancestors still prevent cycles.
func (a *App) publicationWorkspacePlatform(p domain.Publication, seen map[string]bool) (string, bool) {
	target := publicationPlatform(p)
	if !validWorkspacePlatform(target) {
		return "", false
	}
	source, found := a.recordWorkspacePlatform("rewrites", p.RewriteID, workspaceAncestors(seen))
	if !found || source != target {
		return "", false
	}
	if p.VideoProjectID != "" {
		var video domain.VideoProject
		if a.Store.Get("videos", p.VideoProjectID, &video) != nil || video.RewriteID != p.RewriteID {
			return "", false
		}
		platform, found := a.recordWorkspacePlatform("videos", video.ID, workspaceAncestors(seen))
		if !found || platform != target {
			return "", false
		}
	}
	for _, id := range p.AssetIDs {
		var asset domain.Asset
		if a.Store.Get("assets", id, &asset) != nil {
			return "", false
		}
		// Publication-owned generated media uses this verified source lineage.
		// Public upload/edit routes cannot change an existing asset's owner.
		if p.ID != "" && asset.OwnerID == p.ID {
			continue
		}
		platform, found := a.recordWorkspacePlatform("assets", id, workspaceAncestors(seen))
		if !found || platform != target {
			return "", false
		}
	}
	return target, true
}

func (a *App) ownerWorkspacePlatform(id string, seen map[string]bool) (string, bool) {
	for _, bucket := range []string{"notes", "rewrites", "videos", "publications", "assets"} {
		if p, found := a.recordWorkspacePlatform(bucket, id, seen); found {
			return p, true
		}
	}
	return "", false
}

func (a *App) recordWorkspacePlatform(bucket, id string, seen map[string]bool) (string, bool) {
	key := bucket + ":" + id
	if id == "" || seen[key] {
		return "", false
	}
	seen[key] = true
	switch bucket {
	case "notes":
		var n domain.Note
		if a.Store.Get(bucket, id, &n) != nil {
			return "", false
		}
		return noteWorkspacePlatform(n), true
	case "rewrites":
		var r domain.Rewrite
		if a.Store.Get(bucket, id, &r) != nil {
			return "", false
		}
		if p, found := a.recordWorkspacePlatform("notes", r.NoteID, seen); found {
			return p, true
		}
		for _, v := range r.Versions {
			if v.SourceSnapshot != nil {
				return noteWorkspacePlatform(*v.SourceSnapshot), true
			}
		}
		return "xiaohongshu", true
	case "videos":
		var v domain.VideoProject
		if a.Store.Get(bucket, id, &v) != nil {
			return "", false
		}
		if p, found := a.recordWorkspacePlatform("rewrites", v.RewriteID, seen); found {
			return p, true
		}
		return "xiaohongshu", true
	case "publications":
		var p domain.Publication
		if a.Store.Get(bucket, id, &p) != nil {
			return "", false
		}
		return a.publicationWorkspacePlatform(p, seen)
	case "assets":
		var asset domain.Asset
		if a.Store.Get(bucket, id, &asset) != nil {
			return "", false
		}
		if p, found := a.ownerWorkspacePlatform(asset.OwnerID, seen); found {
			return p, true
		}
		return "", false
	case "jobs":
		var j domain.Job
		if a.Store.Get(bucket, id, &j) != nil {
			return "", false
		}
		return a.jobWorkspacePlatform(j, seen)
	}
	return "", false
}

func (a *App) jobWorkspacePlatform(j domain.Job, seen map[string]bool) (string, bool) {
	if j.Kind == "publish" || j.Kind == "publish_prepare" {
		return a.recordWorkspacePlatform("publications", j.EntityID, seen)
	} else if p, found := a.ownerWorkspacePlatform(j.EntityID, seen); found {
		return p, true
	}
	if p, found := a.recordWorkspacePlatform("assets", j.AssetID, seen); found {
		return p, true
	}
	return "xiaohongshu", true
}

func (a *App) workspaceAssetsByIDs(c *gin.Context, ids []string) []domain.Asset {
	allowed := []string{}
	for _, id := range ids {
		if a.workspaceAllows(c, "assets", id, true) {
			allowed = append(allowed, id)
		}
	}
	return a.assetsByIDs(allowed)
}

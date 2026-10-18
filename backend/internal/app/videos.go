package app

import (
	"github.com/gin-gonic/gin"
	"log"
	"xhs-workbench/internal/domain"
)

func (a *App) routesVideos(g *gin.RouterGroup) {
	a.routesVideoRewrites(g)
	g.GET("/video-projects", func(c *gin.Context) {
		videos, err := readList[domain.VideoProject](a, "videos")
		if err != nil {
			log.Printf("list videos: %v", err)
			fail(c, 500, "读取视频方案失败")
			return
		}
		items := []domain.VideoProject{}
		for _, v := range videos {
			if v.Workflow == videoRewriteWorkflow && a.workspaceAllows(c, "videos", v.ID, true) {
				items = append(items, publicVideoProject(v))
			}
		}
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
	g.GET("/video-projects/:id", func(c *gin.Context) {
		if v, ok := a.activeVideoRewrite(c); ok {
			c.JSON(200, publicVideoProject(v))
		}
	})
	g.POST("/video-projects", func(c *gin.Context) {
		var b struct {
			Workflow       string `json:"workflow"`
			PrimaryAssetID string `json:"primary_asset_id"`
			RewriteID      string `json:"rewrite_id"`
			VersionID      string `json:"version_id"`
		}
		if !bind(c, &b) {
			return
		}
		if b.Workflow != videoRewriteWorkflow {
			fail(c, 410, "旧视频制作流程已移除，请使用视频仿写工作台")
			return
		}
		a.createVideoRewrite(c, b.RewriteID, b.VersionID, b.PrimaryAssetID)
	})
	g.PATCH("/video-projects/:id", func(c *gin.Context) {
		if _, ok := a.activeVideoRewrite(c); ok {
			a.editVideoRewrite(c)
		}
	})
	g.POST("/video-projects/:id/generate", func(c *gin.Context) {
		if _, ok := a.activeVideoRewrite(c); ok {
			a.generateVideoRewrite(c)
		}
	})
}

func (a *App) activeVideoRewrite(c *gin.Context) (domain.VideoProject, bool) {
	var v domain.VideoProject
	if !a.get(c, "videos", c.Param("id"), &v) {
		return v, false
	}
	if v.Workflow != videoRewriteWorkflow || v.VideoRewrite == nil {
		fail(c, 410, "旧视频制作流程已移除，请使用视频仿写工作台")
		return v, false
	}
	return v, true
}

func (a *App) videoBusy(id string) bool {
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		log.Printf("list jobs for video busy state: %v", err)
		return true
	}
	for _, j := range jobs {
		if j.EntityID == id && (j.Kind == "video_script" || j.Kind == "video_rewrite") && (j.State == "queued" || j.State == "running" || j.State == "submission_unknown") {
			return true
		}
	}
	return false
}

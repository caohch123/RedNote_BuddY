package app

import (
	"context"
	"github.com/gin-gonic/gin"
	"strings"
	"time"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
)

func (a *App) routesSettings(g *gin.RouterGroup) {
	g.GET("/settings", func(c *gin.Context) {
		s, e := a.settings()
		if e != nil {
			fail(c, 500, "无法解密设置，请使用原 Windows 用户启动")
			return
		}
		c.JSON(200, masked(s))
	})
	g.PATCH("/settings", func(c *gin.Context) {
		var b struct {
			domain.Settings
			FFmpegPath  *string `json:"ffmpeg_path"`
			FFprobePath *string `json:"ffprobe_path"`
			NodePath    *string `json:"node_path"`
		}
		if !bind(c, &b) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		s, e := a.settings()
		if e != nil {
			fail(c, 500, "设置解密失败")
			return
		}
		nodeChanged := b.NodePath != nil && strings.TrimSpace(*b.NodePath) != s.NodePath
		if nodeChanged {
			if a.xhs != nil {
				if !a.xhs.op.TryLock() {
					fail(c, 409, "账号组件正在使用，请完成当前操作后再更改 Node.js 路径")
					return
				}
				defer a.xhs.op.Unlock()
			}
		}
		if b.CollectorURL != "" {
			if e = validateEndpoint(b.CollectorURL, true); e != nil {
				fail(c, 400, e.Error())
				return
			}
			s.CollectorURL = strings.TrimRight(b.CollectorURL, "/")
		}
		if b.Cookie != "" {
			if b.Cookie == "__CLEAR__" {
				s.Cookie = ""
			} else {
				s.Cookie = b.Cookie
			}
		}
		for k, p := range b.Providers {
			old, ok := s.Providers[k]
			if !ok {
				fail(c, 400, "未知模型能力")
				return
			}
			if p.BaseURL != "" {
				if e = validateEndpoint(p.BaseURL, false); e != nil {
					fail(c, 400, e.Error())
					return
				}
				old.BaseURL = strings.TrimRight(p.BaseURL, "/")
			}
			if p.Model != "" {
				old.Model = p.Model
			}
			if p.APIKey != "" {
				if p.APIKey == "__CLEAR__" {
					old.APIKey = ""
				} else {
					old.APIKey = p.APIKey
				}
			}
			if k == "video" {
				if !providers.WanVideoModel(old.Model) {
					fail(c, 400, "视频生成请使用 wan3.0-video 或 wan3.0-video-prime")
					return
				}
				if old.BaseURL != "" {
					native, err := providers.WanVideoBase(old)
					if err != nil {
						fail(c, 400, err.Error())
						return
					}
					old.BaseURL = native
				}
			}
			old.WorkspaceID = p.WorkspaceID
			s.Providers[k] = old
		}
		if b.FFmpegPath != nil {
			s.FFmpegPath = strings.TrimSpace(*b.FFmpegPath)
		}
		if b.FFprobePath != nil {
			s.FFprobePath = strings.TrimSpace(*b.FFprobePath)
		}
		if b.NodePath != nil {
			s.NodePath = strings.TrimSpace(*b.NodePath)
		}
		if e = a.saveSettings(s); e != nil {
			fail(c, 500, "保存密钥失败")
			return
		}
		if nodeChanged {
			a.refreshNodeRuntimes()
		}
		c.JSON(200, masked(s))
	})
	g.POST("/settings/test", func(c *gin.Context) {
		var b struct {
			Capability string `json:"capability"`
		}
		if !bind(c, &b) {
			return
		}
		s, e := a.settings()
		if e != nil {
			fail(c, 500, "设置读取失败")
			return
		}
		if b.Capability != "collector" {
			if _, ok := s.Providers[b.Capability]; !ok {
				fail(c, 400, "未知能力")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		e = providers.New(s).Test(ctx, b.Capability)
		if e != nil {
			c.JSON(200, gin.H{"ok": false, "message": a.safeError(e)})
			return
		}
		message := "服务连接及配置验证成功"
		if b.Capability == "image" || b.Capability == "vision" {
			message = "服务连接与密钥验证成功；尚未执行生成或分析，所选模型权限和额度需在实际调用时验证"
		}
		if b.Capability == "video" {
			message = "视频服务连接与密钥验证成功，可查询任务；尚未验证所选模型已开通或有调用权限"
		}
		c.JSON(200, gin.H{"ok": true, "message": message})
	})
}

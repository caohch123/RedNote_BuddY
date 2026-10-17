package app

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

type workspaceEntry struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Name     string `json:"name"`
}
type workspaceRegistry struct {
	Platforms []string `json:"platforms"`
}
type workspaceRegistryReader interface {
	Get(string, string, any) error
}

func readWorkspaceRegistry(reader workspaceRegistryReader) (workspaceRegistry, error) {
	var registry workspaceRegistry
	err := reader.Get("workspace_registry", "main", &registry)
	if errors.Is(err, store.ErrNotFound) {
		return workspaceRegistry{Platforms: []string{"xiaohongshu"}}, nil
	}
	return registry, err
}

func workspaceRegistryActive(reader workspaceRegistryReader, platform string) (bool, error) {
	registry, err := readWorkspaceRegistry(reader)
	return has(registry.Platforms, platform), err
}

func setWorkspaceRegistry(tx *store.Transaction, platform string, active bool) error {
	registry, err := readWorkspaceRegistry(tx)
	if err != nil {
		return err
	}
	platforms := []string{}
	for _, p := range []string{"xiaohongshu"} {
		if p == platform {
			if active {
				platforms = append(platforms, p)
			}
		} else if has(registry.Platforms, p) {
			platforms = append(platforms, p)
		}
	}
	return tx.Put("workspace_registry", "main", workspaceRegistry{Platforms: platforms})
}

func workspaceRegistryEntries(registry workspaceRegistry) []workspaceEntry {
	items := []workspaceEntry{}
	for _, p := range []string{"xiaohongshu"} {
		if !has(registry.Platforms, p) {
			continue
		}
		items = append(items, workspaceEntry{ID: p, Platform: p, Name: "小红书"})
	}
	return items
}

func (a *App) routesWorkspaceRegistry(g *gin.RouterGroup) {
	g.GET("/workspaces", func(c *gin.Context) {
		registry, err := readWorkspaceRegistry(a.Store)
		if err != nil {
			fail(c, 500, "读取工作区失败")
			return
		}
		items := workspaceRegistryEntries(registry)
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
	g.POST("/workspaces", func(c *gin.Context) {
		var body struct {
			Platform string `json:"platform"`
		}
		if !bind(c, &body) {
			return
		}
		if !validWorkspacePlatform(body.Platform) {
			fail(c, 400, "工作区平台请选择小红书")
			return
		}
		if scope := workspacePlatform(c); scope != "" && scope != body.Platform {
			fail(c, 400, "请求平台与工作区不一致")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		err := a.Store.Atomic(func(tx *store.Transaction) error {
			active, err := workspaceRegistryActive(tx, body.Platform)
			if err != nil {
				return err
			}
			if active {
				return store.ErrConflict
			}
			return setWorkspaceRegistry(tx, body.Platform, true)
		})
		if errors.Is(err, store.ErrConflict) {
			fail(c, 409, "该平台工作区已经存在")
			return
		}
		if err != nil {
			fail(c, 500, "创建工作区失败")
			return
		}
		c.JSON(201, workspaceRegistryEntries(workspaceRegistry{Platforms: []string{body.Platform}})[0])
	})
	g.DELETE("/workspaces/:id", func(c *gin.Context) {
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if !bind(c, &body) {
			return
		}
		if !body.Confirm {
			fail(c, 400, "请再次确认删除工作区后提交")
			return
		}
		platform := c.Param("id")
		if !validWorkspacePlatform(platform) {
			fail(c, 404, "工作区不存在")
			return
		}
		if scope := workspacePlatform(c); scope != "" && scope != platform {
			fail(c, 404, "当前工作区没有此记录")
			return
		}
		a.deleteLocalRecords(c, "", nil, platform)
	})
}

// The operation mutex remains held through logout and local credential reset.
// Close terminates the runtime even if logout fails or its browser has stopped.
func (a *App) clearDeletedWorkspaceAccount(c *gin.Context, platform string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	// Only the Xiaohongshu workspace remains, so the platform check is a guard
	// against a stale caller rather than a dispatch.
	if platform != "xiaohongshu" {
		return
	}
	var result map[string]any
	x := a.xhs
	x.mu.Lock()
	active := x.session != "" || len(x.logins) > 0 || x.status.Status == "logged_in"
	x.mu.Unlock()
	if active {
		_ = x.rpc.Call(ctx, "logout", map[string]any{}, &result)
	}
	_ = x.rpc.Close()
	x.mu.Lock()
	x.rpc = providers.NewXHSPublisher(a.Root, a.nodeToolPath)
	x.session = ""
	x.generation = ID()
	x.tokens = map[string]xhsPublishToken{}
	x.logins = map[string]*xhsLogin{}
	x.loaded = true
	x.status = xhsAccountStatus{Status: "logged_out", Message: "工作区已删除"}
	x.mu.Unlock()
}

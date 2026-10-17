package app

import (
	"errors"
	"os"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func (a *App) removeUnstoredUpload(path string) {
	checked, err := a.checkedMediaDeletionPath(path)
	if err == nil {
		_ = os.Remove(checked)
	}
}

// Uploads perform potentially slow IO/probing before taking the mutex. Check
// ownership again at commit so a simultaneous cascade cannot leave an orphan
// record or its untracked file after removing that owner/workspace.
func (a *App) commitUploadedAsset(c *gin.Context, asset domain.Asset, path string) (committed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	defer func() {
		if !committed {
			a.removeUnstoredUpload(path)
		}
	}()
	exists := false
	for _, bucket := range []string{"notes", "rewrites", "videos"} {
		var value map[string]any
		err := a.Store.Get(bucket, asset.OwnerID, &value)
		if err == nil {
			exists = true
			break
		}
		if !errors.Is(err, store.ErrNotFound) {
			fail(c, 500, "读取素材所属内容失败")
			return false
		}
	}
	if !exists {
		fail(c, 409, "素材所属内容已删除，请重新选择所属内容")
		return false
	}
	platform, found := a.ownerWorkspacePlatform(asset.OwnerID, map[string]bool{})
	if !found || !validWorkspacePlatform(platform) {
		fail(c, 409, "无法确认素材所属工作区")
		return false
	}
	if scope := workspacePlatform(c); scope != "" && scope != platform {
		fail(c, 404, "素材所属内容不在当前工作区")
		return false
	}
	active, err := workspaceRegistryActive(a.Store, platform)
	if err != nil {
		fail(c, 500, "读取工作区失败")
		return false
	}
	if !active {
		fail(c, 409, "素材所属工作区已删除，请重新添加工作区")
		return false
	}
	return a.put(c, "assets", asset.ID, asset)
}

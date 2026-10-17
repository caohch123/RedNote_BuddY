package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
)

var styleKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
var styleCategories = []struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}{
	{"lifestyle", "生活方式"}, {"fashion", "穿搭美妆"}, {"food", "美食餐饮"}, {"travel", "旅行户外"},
	{"home", "家居设计"}, {"education", "教育知识"}, {"technology", "科技数码"}, {"wellness", "健康运动"},
	{"business", "商业产品"}, {"art", "艺术文化"},
}

type styleLibraryEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Credit   string   `json:"credit"`
	File     string   `json:"file,omitempty"`
}

func (a *App) styleFile(name string) (string, error) {
	base := filepath.Join(a.Root, "style-library")
	path := filepath.Join(base, filepath.FromSlash(name))
	if name == "" || filepath.IsAbs(name) || !within(base, path) {
		return "", errors.New("参考图片路径不允许")
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil || !within(a.Root, realBase) {
		return "", errors.New("参考图库不可用")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !within(realBase, real) {
		return "", errors.New("参考图片不可用")
	}
	return real, nil
}

func (a *App) styleCatalogue() ([]styleLibraryEntry, error) {
	path, err := a.styleFile("catalog.json")
	if err != nil {
		// A fresh installation can start without a catalogue.
		if _, e := os.Stat(filepath.Join(a.Root, "style-library", "catalog.json")); os.IsNotExist(e) {
			return []styleLibraryEntry{}, nil
		}
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var catalog struct {
		Items []styleLibraryEntry `json:"items"`
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &catalog) != nil || len(catalog.Items) > 2000 {
		return nil, errors.New("风格图库配置无效")
	}
	seen := map[string]bool{}
	for _, entry := range catalog.Items {
		validCategory := false
		for _, category := range styleCategories {
			validCategory = validCategory || category.ID == entry.Category
		}
		if !styleKey.MatchString(entry.ID) || seen[entry.ID] || !validCategory || strings.TrimSpace(entry.Title) == "" {
			return nil, errors.New("风格图库条目无效或重复")
		}
		seen[entry.ID] = true
	}
	if catalog.Items == nil {
		catalog.Items = []styleLibraryEntry{}
	}
	return catalog.Items, nil
}

func (a *App) readStyleImage(entry styleLibraryEntry) ([]byte, string, image.Config, error) {
	path, err := a.styleFile(entry.File)
	if err != nil {
		return nil, "", image.Config{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", image.Config{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
		return nil, "", image.Config{}, errors.New("参考图片须为 10 MB 以内的本地图片")
	}
	data, err := io.ReadAll(io.LimitReader(f, 10<<20+1))
	if err != nil || len(data) > 10<<20 {
		return nil, "", image.Config{}, errors.New("读取参考图片失败")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	mime := http.DetectContentType(data)
	if err != nil || config.Width < 1 || config.Height < 1 || (mime != "image/png" && mime != "image/jpeg") {
		return nil, "", config, errors.New("参考图片须为 PNG 或 JPEG")
	}
	return data, mime, config, nil
}

func (a *App) findStyle(id string) (styleLibraryEntry, error) {
	items, err := a.styleCatalogue()
	if err != nil {
		return styleLibraryEntry{}, err
	}
	for _, entry := range items {
		if entry.ID == id {
			return entry, nil
		}
	}
	return styleLibraryEntry{}, errors.New("风格参考图尚未上架或已移除，请重新选择")
}

// Freeze a selected catalogue image in the project's data directory. Changes
// to the product catalogue cannot silently alter an existing paid task.
func (a *App) snapshotStyle(rewriteID, id string) (domain.Asset, styleLibraryEntry, error) {
	entry, err := a.findStyle(id)
	if err != nil {
		return domain.Asset{}, entry, err
	}
	data, mime, config, err := a.readStyleImage(entry)
	if err != nil {
		return domain.Asset{}, entry, err
	}
	ext := ".png"
	if mime == "image/jpeg" {
		ext = ".jpg"
	}
	assetID := ID()
	rel := filepath.Join("assets", assetID+ext)
	if err = os.WriteFile(filepath.Join(a.Data, rel), data, 0600); err != nil {
		a.removeUnstoredUpload(filepath.Join(a.Data, rel))
		return domain.Asset{}, entry, err
	}
	hash := sha256.Sum256(data)
	return domain.Asset{ID: assetID, OwnerID: rewriteID, Kind: "style_reference", SourceID: "style-library:" + id, LocalPath: rel, MIME: mime, Status: "ready", Width: config.Width, Height: config.Height, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data)), CreatedAt: now(), Extraction: domain.Extraction{Status: "not_applicable", Segments: []domain.Segment{}}}, entry, nil
}

func (a *App) routesStyleLibrary(g *gin.RouterGroup) {
	g.GET("/style-library", func(c *gin.Context) {
		catalog, err := a.styleCatalogue()
		if err != nil {
			fail(c, 500, "风格参考库暂不可用，请检查图库配置")
			return
		}
		items := []styleLibraryEntry{}
		for _, entry := range catalog {
			if _, _, _, err := a.readStyleImage(entry); err != nil {
				continue
			}
			entry.File = ""
			items = append(items, entry)
		}
		c.JSON(200, gin.H{"items": items, "total": len(items), "categories": styleCategories})
	})
	g.GET("/style-library/:id/file", func(c *gin.Context) {
		entry, err := a.findStyle(c.Param("id"))
		if err != nil {
			fail(c, 404, "参考图片不存在")
			return
		}
		data, mime, _, err := a.readStyleImage(entry)
		if err != nil {
			fail(c, 404, "参考图片不可用")
			return
		}
		c.Data(200, mime, data)
	})
}

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"xhs-workbench/internal/domain"
)

var linksRE = regexp.MustCompile(`https?://(?:www\.)?(?:xiaohongshu\.com|xhslink\.(?:com|cn))/[^\s<>"'，。；！？、【】《》]+`)

func extractURLs(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, u := range linksRE.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".,;)）]")
		if !seen[u] {
			out = append(out, u)
			seen[u] = true
		}
	}
	return out
}

// Every accepted link belongs to the single Xiaohongshu workspace.
func collectionLinkIdentity(raw string) (string, string) {
	u, e := url.Parse(raw)
	if e == nil && xhsInputHost(u.Hostname()) {
		return "xiaohongshu", ""
	}
	return "xiaohongshu", ""
}

func xhsInputHost(host string) bool {
	switch strings.ToLower(host) {
	case "xiaohongshu.com", "www.xiaohongshu.com", "xhslink.com", "xhslink.cn", "www.xhslink.com", "www.xhslink.cn":
		return true
	}
	return false
}

// Public collection responses never leak signed source URLs or local paths.
func publicCollectionAsset(v domain.Asset) domain.Asset {
	v.LocalPath = ""
	return v
}

func (a *App) routesCollections(g *gin.RouterGroup) {
	g.GET("/collections", a.collectionsList)
	g.POST("/collections", a.collectionsCreate)
	g.GET("/collections/:id", func(c *gin.Context) {
		var n domain.Note
		if !a.get(c, "notes", c.Param("id"), &n) {
			return
		}
		n.CoverAssetID = a.readyCoverAssetID(n)
		c.JSON(200, gin.H{"note": n, "assets": a.workspaceAssetsByIDs(c, n.AssetIDs)})
	})
	g.PATCH("/collections/:id", func(c *gin.Context) {
		var b struct {
			Tags []string `json:"tags"`
		}
		if !bind(c, &b) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var n domain.Note
		if a.get(c, "notes", c.Param("id"), &n) {
			n.Tags = b.Tags
			if a.put(c, "notes", n.ID, n) {
				c.JSON(200, n)
			}
		}
	})
	g.POST("/collections/:id/recollect", func(c *gin.Context) {
		var n domain.Note
		if a.get(c, "notes", c.Param("id"), &n) {
			j, e := a.enqueue("collect", n.ID, "", "", map[string]string{"url": n.URL})
			async(c, j, e)
		}
	})
	g.POST("/collections/:id/extract", func(c *gin.Context) {
		var b struct {
			AssetIDs []string `json:"asset_ids"`
		}
		if c.Request.ContentLength > 0 && !bind(c, &b) {
			return
		}
		var n domain.Note
		if !a.get(c, "notes", c.Param("id"), &n) {
			return
		}
		ids := b.AssetIDs
		if len(ids) == 0 {
			ids = n.AssetIDs
		}
		for _, id := range ids {
			if !has(n.AssetIDs, id) {
				fail(c, 400, "素材不属于当前笔记")
				return
			}
			if !a.workspaceAllows(c, "assets", id, false) {
				fail(c, 404, "素材不属于当前工作区")
				return
			}
		}
		jobs := []string{}
		for _, id := range ids {
			var asset domain.Asset
			if a.Store.Get("assets", id, &asset) != nil {
				continue
			}
			if asset.Status != "ready" {
				j, e := a.enqueue("collect", n.ID, "", "", map[string]string{"url": n.URL})
				if e == nil {
					jobs = append(jobs, j.ID)
				}
				continue
			}
			kind := "ocr"
			if asset.Kind == "video" {
				kind = "asr"
			} else if asset.Kind != "image" && asset.Kind != "cover" {
				continue
			}
			j, e := a.enqueue(kind, n.ID, id, "", nil)
			if e == nil {
				jobs = append(jobs, j.ID)
			}
		}
		first := ""
		if len(jobs) > 0 {
			first = jobs[0]
		}
		c.JSON(202, gin.H{"job_ids": jobs, "job_id": first})
	})
	g.GET("/assets", func(c *gin.Context) {
		assets, err := readList[domain.Asset](a, "assets")
		if err != nil {
			log.Printf("list assets: %v", err)
			fail(c, 500, "读取素材列表失败")
			return
		}
		items := []domain.Asset{}
		for _, v := range assets {
			if !a.workspaceAllows(c, "assets", v.ID, true) {
				continue
			}
			if owner := c.Query("owner_id"); owner != "" && v.OwnerID != owner {
				continue
			}
			v = publicCollectionAsset(v)
			items = append(items, v)
		}
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
	g.POST("/assets", a.uploadAsset)
	g.GET("/assets/:id/file", a.serveAsset)
	g.PATCH("/assets/:id/text", func(c *gin.Context) {
		var b struct {
			Text     string           `json:"text"`
			Segments []domain.Segment `json:"segments"`
		}
		if !bind(c, &b) {
			return
		}
		for _, s := range b.Segments {
			if s.Start < 0 || s.End < s.Start {
				fail(c, 400, "时间段无效")
				return
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var v domain.Asset
		if !a.get(c, "assets", c.Param("id"), &v) {
			return
		}
		v.Extraction = domain.Extraction{Text: b.Text, Segments: b.Segments, Edited: true, Status: "succeeded"}
		if a.put(c, "assets", v.ID, v) {
			v = publicCollectionAsset(v)
			c.JSON(200, v)
		}
	})
	g.PATCH("/assets/:id", func(c *gin.Context) {
		var b struct {
			Position int `json:"position"`
		}
		if !bind(c, &b) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		var v domain.Asset
		if a.get(c, "assets", c.Param("id"), &v) {
			v.Position = b.Position
			if a.put(c, "assets", v.ID, v) {
				v = publicCollectionAsset(v)
				c.JSON(200, v)
			}
		}
	})
}
func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func (a *App) assetsByIDs(ids []string) []domain.Asset {
	out := []domain.Asset{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var v domain.Asset
		if a.Store.Get("assets", id, &v) == nil {
			v = publicCollectionAsset(v)
			out = append(out, v)
		}
	}
	return out
}
func (a *App) collectionsCreate(c *gin.Context) {
	var b struct {
		Text     string `json:"text"`
		Platform string `json:"platform"`
	}
	if !bind(c, &b) {
		return
	}
	scope, ok := bindWorkspacePlatform(c, b.Platform)
	if !ok {
		return
	}
	urls := extractURLs(b.Text)
	if len(urls) == 0 {
		fail(c, 400, "未找到小红书作品链接，请粘贴完整 URL 或分享文字")
		return
	}
	if len(urls) > 50 {
		fail(c, 400, "单次最多采集 50 条链接")
		return
	}
	jobs, existing := []string{}, []string{}
	rejected := []map[string]string{}
	for _, u := range urls {
		platform, identity := collectionLinkIdentity(u)
		if active, e := workspaceRegistryActive(a.Store, platform); e != nil || !active {
			rejected = append(rejected, map[string]string{"url": u, "error": "采集工作区已删除，请先添加工作区"})
			continue
		}
		if scope != "" && platform != scope {
			rejected = append(rejected, map[string]string{"url": u, "error": "作品链接不属于当前工作区"})
			continue
		}
		a.mu.Lock()
		var n domain.Note
		found := false
		notes, err := readList[domain.Note](a, "notes")
		if err != nil {
			a.mu.Unlock()
			log.Printf("list notes: %v", err)
			fail(c, 500, "读取采集列表失败")
			return
		}
		for _, v := range notes {
			if noteWorkspacePlatform(v) == platform && (v.URL == u || (identity != "" && v.NoteID == identity)) {
				n = v
				found = true
				break
			}
		}
		if !found {
			n = domain.Note{ID: ID(), Platform: platform, NoteID: identity, URL: u, Title: "等待采集", Topics: []string{}, Tags: []string{}, AssetIDs: []string{}, Status: "queued", MetadataStatus: "queued", MediaStatus: "pending", ExtractionStatus: "pending", CollectedAt: now()}
		}
		if found && n.MetadataStatus == "succeeded" {
			a.mu.Unlock()
			existing = append(existing, n.ID)
			continue
		}
		j, e := a.enqueueCollectionLocked(n, map[string]string{"url": u})
		a.mu.Unlock()
		if e != nil {
			fail(c, 500, "创建采集任务失败")
			return
		}
		jobs = append(jobs, j.ID)
	}
	first := ""
	if len(jobs) > 0 {
		first = jobs[0]
	}
	if len(jobs) == 0 && len(existing) == 0 && len(rejected) > 0 {
		c.JSON(400, gin.H{"error": "没有可采集的作品链接", "rejected_items": rejected})
		return
	}
	c.JSON(202, gin.H{"job_id": first, "job_ids": jobs, "existing_ids": existing, "rejected_items": rejected})
}
func (a *App) collectionsList(c *gin.Context) {
	items := []domain.Note{}
	q := strings.ToLower(c.Query("q"))
	notes, err := readList[domain.Note](a, "notes")
	if err != nil {
		log.Printf("list notes: %v", err)
		fail(c, 500, "读取采集列表失败")
		return
	}
	for _, v := range notes {
		if !validWorkspacePlatform(noteWorkspacePlatform(v)) {
			continue
		}
		if p := workspacePlatform(c); p != "" && noteWorkspacePlatform(v) != p {
			continue
		}
		v.Platform = noteWorkspacePlatform(v)
		v.CoverAssetID = a.readyCoverAssetID(v)
		if q != "" && !strings.Contains(strings.ToLower(v.Title+" "+v.Body+" "+v.Author.Name), q) {
			continue
		}
		if s := c.Query("status"); s != "" && v.Status != s {
			continue
		}
		if t := c.Query("tag"); t != "" && !has(v.Tags, t) {
			continue
		}
		items = append(items, v)
	}
	key := c.DefaultQuery("sort", "collected_at")
	asc := c.Query("order") == "asc"
	val := func(p *int64) int64 {
		if p == nil {
			return -1
		}
		return *p
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !asc {
			i, j = j, i
		}
		var less bool
		switch key {
		case "title":
			less = items[i].Title < items[j].Title
		case "likes":
			less = val(items[i].Likes) < val(items[j].Likes)
		case "collects":
			less = val(items[i].Collects) < val(items[j].Collects)
		case "comments":
			less = val(items[i].Comments) < val(items[j].Comments)
		default:
			less = items[i].CollectedAt < items[j].CollectedAt
		}
		return less
	})
	total := len(items)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	if page > max(1, (total+size-1)/size) {
		c.JSON(200, gin.H{"items": []domain.Note{}, "total": total})
		return
	}
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	c.JSON(200, gin.H{"items": items[start:end], "total": total})
}

// A note is visible before its media finishes downloading. Only expose a
// locally available image as its cover, and fall back when the first image
// failed but a later image succeeded.
func (a *App) readyCoverAssetID(n domain.Note) string {
	seen := map[string]bool{}
	for _, id := range append([]string{n.CoverAssetID}, n.AssetIDs...) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var asset domain.Asset
		if a.Store.Get("assets", id, &asset) != nil || asset.OwnerID != n.ID || asset.Status != "ready" ||
			(asset.Kind != "image" && asset.Kind != "cover") || !strings.HasPrefix(asset.MIME, "image/") {
			continue
		}
		if path, err := a.assetPath(asset); err == nil {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return id
			}
		}
	}
	return ""
}
func (a *App) serveAsset(c *gin.Context) {
	var v domain.Asset
	if !a.get(c, "assets", c.Param("id"), &v) {
		return
	}
	p, e := a.assetPath(v)
	if e != nil {
		fail(c, 404, "素材尚未下载或不可用")
		return
	}
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(p)))
	}
	c.Header("Content-Type", v.MIME)
	c.Header("Cache-Control", "private, max-age=3600")
	http.ServeFile(c.Writer, c.Request, p)
}
func (a *App) uploadAsset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<20)
	if e := c.Request.ParseMultipartForm(8 << 20); e != nil {
		fail(c, 400, "上传失败，单文件上限为 512 MB")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	f, header, e := c.Request.FormFile("file")
	if e != nil {
		fail(c, 400, "请选择媒体文件")
		return
	}
	defer f.Close()
	owner := c.PostForm("owner_id")
	if owner == "" {
		fail(c, 400, "需要素材所属项目")
		return
	}
	exists := false
	for _, bucket := range []string{"notes", "rewrites", "videos"} {
		var value map[string]any
		if a.Store.Get(bucket, owner, &value) == nil {
			exists = true
		}
	}
	if !exists {
		fail(c, 400, "素材所属项目不存在")
		return
	}
	platform, found := a.ownerWorkspacePlatform(owner, map[string]bool{})
	if scope := workspacePlatform(c); !found || !validWorkspacePlatform(platform) || (scope != "" && platform != scope) {
		fail(c, 404, "素材所属项目不在当前工作区")
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mime := http.DetectContentType(head[:n])
	if mime == "application/octet-stream" && strings.EqualFold(filepath.Ext(header.Filename), ".heic") {
		mime = "image/heic"
	}
	kind, ext := mediaType(mime)
	if kind == "" || kind == "subtitle" {
		fail(c, 400, "仅支持图片、视频和音频文件")
		return
	}
	assets, err := readList[domain.Asset](a, "assets")
	if err != nil {
		log.Printf("list assets: %v", err)
		fail(c, 500, "读取素材列表失败")
		return
	}
	v := domain.Asset{ID: ID(), Name: filepath.Base(header.Filename), OwnerID: owner, SourceID: "upload", Kind: kind, MIME: mime, Status: "ready", CreatedAt: now(), Extraction: domain.Extraction{Status: "pending", Segments: []domain.Segment{}}, Position: len(assets)}
	path := filepath.Join(a.Data, "assets", v.ID+ext)
	if _, e = a.checkedMediaDeletionPath(path); e != nil {
		fail(c, 500, "素材保存路径未通过安全检查")
		return
	}
	out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		fail(c, 500, "无法创建素材文件")
		return
	}
	hash := sha256.New()
	writer := io.MultiWriter(out, hash)
	_, e = writer.Write(head[:n])
	var count int64
	if e == nil {
		count, e = io.Copy(writer, io.LimitReader(f, (512<<20)-int64(n)+1))
	}
	out.Close()
	if e != nil || count+int64(n) > 512<<20 {
		a.removeUnstoredUpload(path)
		fail(c, 400, "素材过大或写入失败")
		return
	}
	v.LocalPath, _ = filepath.Rel(a.Data, path)
	v.Size = count + int64(n)
	v.SHA256 = hex.EncodeToString(hash.Sum(nil))
	a.probeAsset(&v)
	if !a.commitUploadedAsset(c, v, path) {
		return
	}
	v.LocalPath = ""
	c.JSON(201, v)
}
func mediaType(mime string) (string, string) {
	mime = strings.Split(mime, ";")[0]
	switch mime {
	case "image/jpeg":
		return "image", ".jpg"
	case "image/png":
		return "image", ".png"
	case "image/webp":
		return "image", ".webp"
	case "image/gif":
		return "image", ".gif"
	case "image/heic", "image/heif":
		return "image", ".heic"
	case "video/mp4":
		return "video", ".mp4"
	case "video/webm":
		return "video", ".webm"
	case "video/quicktime":
		return "video", ".mov"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "audio", ".wav"
	case "audio/mpeg":
		return "audio", ".mp3"
	case "audio/ogg":
		return "audio", ".ogg"
	case "text/plain", "application/x-subrip":
		return "subtitle", ".srt"
	}
	if strings.HasPrefix(mime, "video/") {
		return "video", ".mp4"
	}
	return "", ""
}

var errChanged = errors.New("内容已修改，本次任务结果保留在历史中，请重新制作当前版本")

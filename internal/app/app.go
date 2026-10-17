package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
	"xhs-workbench/internal/tooling"
)

type App struct {
	Store                  *store.Store
	Root, Data, Addr, CSRF string
	mu                     sync.Mutex
	ctx                    context.Context
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	xhs                    *xhsState
	lifecycleMu            sync.Mutex
	closing                bool
	jobRuns                map[string]*jobRun
}

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339) }
func New(root, data, addr string) (*App, error) {
	root, _ = filepath.Abs(root)
	if !filepath.IsAbs(data) {
		data = filepath.Join(root, data)
	}
	data, _ = filepath.Abs(data)
	for _, dir := range []string{data, filepath.Join(data, "assets"), filepath.Join(data, "exports"), filepath.Join(data, "logs")} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			return nil, e
		}
	}
	st, e := store.Open(filepath.Join(data, "workbench.db"))
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{Store: st, Root: root, Data: data, Addr: addr, CSRF: ID(), ctx: ctx, cancel: cancel}
	a.xhs = newXHSState(root, a.nodeToolPath)
	var saved domain.Settings
	if errors.Is(st.Get("settings", "main", &saved), store.ErrNotFound) {
		if e = st.Put("settings", "main", a.defaults()); e != nil {
			st.Close()
			return nil, e
		}
	}
	// Restore interrupted work before serving requests or starting workers.
	if e = st.Recover(); e != nil {
		st.Close()
		return nil, e
	}
	if e = a.recoverDeletionCleanup(); e != nil {
		st.Close()
		return nil, e
	}
	if e = a.recoverCollectionStates(); e != nil {
		st.Close()
		return nil, e
	}
	if e = a.recoverClearedJobs(); e != nil {
		st.Close()
		return nil, e
	}
	return a, nil
}
func (a *App) Close() error {
	a.lifecycleMu.Lock()
	a.closing = true
	a.cancel()
	a.lifecycleMu.Unlock()
	a.wg.Wait()
	if a.xhs != nil {
		a.xhs.rpc.Close()
	}
	return a.Store.Close()
}

// Register background work before Close begins its Wait. HTTP shutdown usually
// drains routes first, but tests and error exits may close the app concurrently.
func (a *App) background(fn func()) bool {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.closing || a.ctx.Err() != nil {
		return false
	}
	a.wg.Add(1)
	go func() { defer a.wg.Done(); fn() }()
	return true
}
func (a *App) defaults() domain.Settings {
	pathOr := func(name string) string {
		path, _ := tooling.Resolve(a.Root, "", name)
		return path
	}
	return domain.Settings{CollectorURL: "http://127.0.0.1:5556", Providers: map[string]domain.ProviderConfig{
		"text":   {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus"},
		"vision": {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3-vl-plus"},
		"ocr":    {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.5-ocr"},
		"asr":    {BaseURL: "wss://dashscope.aliyuncs.com/api-ws/v1/inference", Model: "paraformer-realtime-v2"},

		"image": {},
		"video": {Model: "wan3.0-video"},
	}, FFmpegPath: pathOr("ffmpeg"), FFprobePath: pathOr("ffprobe"), NodePath: pathOr("node")}
}
func (a *App) settings() (domain.Settings, error) {
	var s domain.Settings
	e := a.Store.Get("settings", "main", &s)
	if e != nil {
		return s, e
	}
	if s.Providers == nil {
		s.Providers = map[string]domain.ProviderConfig{}
	}
	if _, ok := s.Providers["image"]; !ok {
		s.Providers["image"] = domain.ProviderConfig{}
	}
	if _, ok := s.Providers["vision"]; !ok {
		s.Providers["vision"] = domain.ProviderConfig{BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3-vl-plus"}
	}
	delete(s.Providers, "tts")
	if p, exists := s.Providers["video"]; !exists || !providers.WanVideoModel(p.Model) || strings.Contains(strings.ToLower(p.BaseURL), "volces.com") {
		// Never carry an Ark key into the replacement provider. Reuse only a known workspace address, not another capability's key.
		next := domain.ProviderConfig{Model: "wan3.0-video"}
		for _, key := range []string{"image", "vision", "text", "ocr"} {
			u, err := url.Parse(s.Providers[key].BaseURL)
			if err == nil && u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".maas.aliyuncs.com") {
				u.Path = "/api/v1"
				u.RawQuery = ""
				u.Fragment = ""
				next.BaseURL = u.String()
				break
			}
		}
		s.Providers["video"] = next
	}
	decode := func(v string) (string, error) {
		if v == "" {
			return "", nil
		}
		if !strings.HasPrefix(v, "dpapi:") {
			return "", errors.New("invalid encrypted secret")
		}
		return cryptSecret(strings.TrimPrefix(v, "dpapi:"), false)
	}
	if s.Cookie, e = decode(s.Cookie); e != nil {
		return s, e
	}
	for k, p := range s.Providers {
		p.APIKey, e = decode(p.APIKey)
		if e != nil {
			return s, e
		}
		p.Configured = p.APIKey != ""
		s.Providers[k] = p
	}
	return s, nil
}
func (a *App) saveSettings(s domain.Settings) error {
	delete(s.Providers, "tts")
	encode := func(v string) (string, error) {
		if v == "" {
			return "", nil
		}
		b, e := cryptSecret(v, true)
		return "dpapi:" + b, e
	}
	var e error
	s.Cookie, e = encode(s.Cookie)
	if e != nil {
		return e
	}
	for k, p := range s.Providers {
		p.APIKey, e = encode(p.APIKey)
		if e != nil {
			return e
		}
		p.KeyMask = ""
		p.Configured = false
		s.Providers[k] = p
	}
	return a.Store.Put("settings", "main", s)
}
func masked(s domain.Settings) domain.Settings {
	s.CookieConfigured = s.Cookie != ""
	if s.CookieConfigured {
		s.CookieMask = "已保存（仅本机可解密）"
	}
	s.Cookie = ""
	for k, p := range s.Providers {
		p.Configured = p.APIKey != ""
		if p.Configured {
			p.KeyMask = "••••••••"
		}
		p.APIKey = ""
		s.Providers[k] = p
	}
	return s
}
func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg, "code": http.StatusText(status)})
}
func bind(c *gin.Context, v any) bool {
	if e := c.ShouldBindJSON(v); e != nil {
		fail(c, 400, "请求格式无效")
		return false
	}
	return true
}
func (a *App) get(c *gin.Context, bucket, id string, v any) bool {
	if !a.workspaceAllows(c, bucket, id, c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) {
		fail(c, 404, "当前工作区没有此记录")
		return false
	}
	e := a.Store.Get(bucket, id, v)
	if e == nil {
		return true
	}
	if errors.Is(e, store.ErrNotFound) {
		fail(c, 404, "记录不存在")
	} else {
		fail(c, 500, "读取数据失败")
	}
	return false
}
func (a *App) put(c *gin.Context, bucket, id string, v any) bool {
	if e := a.Store.Put(bucket, id, v); e != nil {
		log.Printf("save %s: %v", bucket, e)
		fail(c, 500, "保存数据失败")
		return false
	}
	return true
}
func readList[T any](a *App, bucket string) ([]T, error) {
	rows, err := a.Store.List(bucket)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", bucket, err)
	}
	items, err := store.Decode[T](rows)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", bucket, err)
	}
	return items, nil
}
func (a *App) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	r.Use(func(c *gin.Context) {
		host := c.Request.Host
		if host != a.Addr && host != "localhost:"+strings.Split(a.Addr, ":")[1] {
			fail(c, 403, "仅允许本机工作台访问")
			return
		}
		if o := c.GetHeader("Origin"); o != "" && o != "http://"+a.Addr && o != "http://localhost:"+strings.Split(a.Addr, ":")[1] {
			fail(c, 403, "来源不允许")
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			if c.GetHeader("X-CSRF-Token") != a.CSRF {
				fail(c, 403, "请刷新页面后重试")
				return
			}
		}
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/assets") {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
		}
		c.Next()
	})
	g := r.Group("/api/v1")
	g.Use(a.workspaceMiddleware)
	g.GET("/session", func(c *gin.Context) { c.JSON(200, gin.H{"csrf_token": a.CSRF}) })
	g.GET("/health", a.health)
	a.routesWorkspaceRegistry(g)
	a.routesDeletions(g)
	a.routesCollections(g)
	a.routesMediaLibrary(g)
	a.routesStyleLibrary(g)
	a.routesRewrites(g)
	a.routesVideos(g)
	a.routesPublications(g)
	a.routesXHS(g)
	a.routesSettings(g)
	g.GET("/jobs", func(c *gin.Context) {
		jobs, err := readList[domain.Job](a, "jobs")
		if err != nil {
			log.Printf("list jobs: %v", err)
			fail(c, 500, "读取任务列表失败")
			return
		}
		items := []domain.Job{}
		for _, j := range jobs {
			if a.workspaceAllows(c, "jobs", j.ID, true) {
				j.Payload = nil
				items = append(items, j)
			}
		}
		c.JSON(200, gin.H{"items": items, "total": len(items)})
	})
	g.GET("/jobs/:id", func(c *gin.Context) {
		var j domain.Job
		if a.get(c, "jobs", c.Param("id"), &j) {
			j.Payload = nil
			c.JSON(200, j)
		}
	})
	g.DELETE("/jobs/completed", a.clearCompletedJobs)
	g.DELETE("/jobs", a.clearJobs)
	g.POST("/jobs/:id/retry", a.retryJob)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			fail(c, 404, "接口不存在")
			return
		}
		dist := filepath.Join(a.Root, "web", "dist")
		name := filepath.Join(dist, filepath.FromSlash(strings.TrimPrefix(c.Request.URL.Path, "/")))
		if within(dist, name) {
			if fi, e := os.Stat(name); e == nil && !fi.IsDir() {
				http.ServeFile(c.Writer, c.Request, name)
				return
			}
		}
		c.File(filepath.Join(dist, "index.html"))
	})
	return r
}
func within(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}
func (a *App) assetPath(v domain.Asset) (string, error) {
	p := v.LocalPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(a.Data, p)
	}
	p, e := filepath.Abs(p)
	if e != nil || !within(a.Data, p) {
		return "", errors.New("素材路径不允许")
	}
	real, e := filepath.EvalSymlinks(p)
	if e != nil || !within(a.Data, real) {
		return "", errors.New("素材文件不可用")
	}
	return real, nil
}
func (a *App) safeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	s, e := a.settings()
	if e == nil {
		if s.Cookie != "" {
			msg = strings.ReplaceAll(msg, s.Cookie, "[redacted]")
		}
		for _, p := range s.Providers {
			if p.APIKey != "" {
				msg = strings.ReplaceAll(msg, p.APIKey, "[redacted]")
			}
		}
	}
	return trimMessage(msg, 1000)
}
func (a *App) health(c *gin.Context) {
	s, e := a.settings()
	if e != nil {
		fail(c, 500, "密钥解密失败，请检查当前 Windows 用户")
		return
	}
	checks := map[string]any{}
	type toolStatus struct {
		key    string
		result tooling.CheckResult
	}
	toolResults := make(chan toolStatus, 3)
	for key, path := range map[string]string{"ffmpeg": s.FFmpegPath, "ffprobe": s.FFprobePath, "node": s.NodePath} {
		go func(key, path string) {
			toolResults <- toolStatus{key, tooling.Check(c.Request.Context(), a.Root, path, key)}
		}(key, path)
	}
	for range 3 {
		value := <-toolResults
		checks[value.key] = value.result
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(s.CollectorURL, "/")+"/openapi.json", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	ok := err == nil
	if resp != nil {
		ok = resp.StatusCode == 200
		resp.Body.Close()
	}
	checks["collector"] = gin.H{"ok": ok}
	databaseContext, databaseCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	databaseErr := a.Store.Ping(databaseContext)
	databaseCancel()
	checks["database"] = gin.H{"ok": databaseErr == nil}

	c.JSON(200, gin.H{"ok": true, "dependencies": checks})
}
func validateEndpoint(raw string, local bool) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("服务地址无效")
	}
	if local {
		if u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
			return errors.New("采集服务必须位于本机")
		}
	} else if u.Scheme != "https" && u.Scheme != "wss" {
		return errors.New("AI 服务必须使用 HTTPS 或 WSS")
	}
	return nil
}
func (a *App) enqueue(kind, entity, asset, scene string, payload any) (domain.Job, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enqueueLocked(kind, entity, asset, scene, payload)
}

func (a *App) enqueueLocked(kind, entity, asset, scene string, payload any) (domain.Job, error) {
	if retiredVideoJob(kind) {
		return domain.Job{}, errors.New("旧视频制作流程已移除")
	}
	platform, found := a.ownerWorkspacePlatform(entity, map[string]bool{})
	if !found {
		return domain.Job{}, errors.New("任务所属记录已删除或不存在")
	}
	if active, e := workspaceRegistryActive(a.Store, platform); e != nil || !active {
		return domain.Job{}, errors.New("任务所属工作区已删除")
	}
	if kind == "collect" {
		var n domain.Note
		if e := a.Store.Get("notes", entity, &n); e != nil {
			return domain.Job{}, e
		}
		return a.enqueueCollectionLocked(n, payload)
	}
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		return domain.Job{}, err
	}
	for _, j := range jobs {
		if j.Kind == kind && j.EntityID == entity && j.AssetID == asset && j.SceneID == scene && (j.State == "queued" || j.State == "running" || j.State == "submission_unknown") {
			return j, nil
		}
	}
	if (kind == "image_analyze" || kind == "image_rewrite") && a.imageRewriteBusy(entity, scene) {
		return domain.Job{}, errors.New("图片方案已有任务执行中")
	}
	b, e := json.Marshal(payload)
	if e != nil {
		return domain.Job{}, e
	}
	j := domain.Job{ID: ID(), Kind: kind, EntityID: entity, AssetID: asset, SceneID: scene, State: "queued", Phase: "queued", CreatedAt: now(), UpdatedAt: now(), Payload: b}
	return j, a.Store.Put("jobs", j.ID, j)
}
func async(c *gin.Context, j domain.Job, e error) {
	if e != nil {
		fail(c, 500, "创建任务失败")
		return
	}
	c.JSON(202, gin.H{"job_id": j.ID})
}
func (a *App) retryJob(c *gin.Context) {
	var body struct {
		RemoteTaskID string `json:"remote_task_id"`
	}
	if c.Request.ContentLength > 0 && !bind(c, &body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var j domain.Job
	if !a.get(c, "jobs", c.Param("id"), &j) {
		return
	}
	if retiredVideoJob(j.Kind) {
		fail(c, 410, "旧视频制作任务已停用，不能重试")
		return
	}
	if j.State == "running" || j.State == "queued" {
		fail(c, 409, "任务正在执行")
		return
	}
	if j.Kind == "publish" {
		fail(c, 409, "发布任务不能直接重试。明确失败后请重新准备并确认；结果不明时先到对应平台核实，避免重复发布")
		return
	}
	if j.Kind == "publish_prepare" {
		fail(c, 409, "发布准备已失效，请从发布稿重新准备并确认")
		return
	}
	if j.State == "succeeded" {
		fail(c, 409, "任务已经完成，请从项目创建新的制作任务")
		return
	}
	if a.jobIsImageV2(&j) {
		fail(c, 409, "请在图片仿写方案中重试分析、编排或恢复批次，避免重复生成")
		return
	}
	if j.Kind == "video_rewrite" || j.Kind == "video_script" {
		fail(c, 409, "请在视频仿写工作台更新脚本或恢复对应生成任务，避免重复提交")
		return
	}
	if j.Kind == "image_rewrite" && j.State == "submission_unknown" {
		fail(c, 409, "图片生成提交结果不明，请先到图片服务控制台核实，再从图片仿写页面处理")
		return
	}
	if j.Kind == "image_analyze" || j.Kind == "image_rewrite" {
		var r domain.Rewrite
		if a.Store.Get("rewrites", j.EntityID, &r) != nil {
			fail(c, 404, "仿写项目已删除")
			return
		}
		item, _, ok := getImageRewrite(r, j.SceneID)
		if !ok || item.Revision != jobRevision(&j) {
			fail(c, 409, "图片方案已改变，请从图片仿写页面提交新任务")
			return
		}
		if a.imageRewriteBusy(r.ID, item.ID) {
			fail(c, 409, "该图片方案已有任务执行中")
			return
		}
	}
	if body.RemoteTaskID != "" {
		fail(c, 400, "请在视频仿写工作台恢复生成任务")
		return
	}
	if j.State == "submission_unknown" {
		fail(c, 409, "提交结果待核实，禁止直接重复提交")
		return
	}
	if len(j.ProviderSnapshot) > 0 {
		j.ProviderSnapshotHistory = append(j.ProviderSnapshotHistory, j.ProviderSnapshot)
		j.ProviderSnapshot = nil
	}
	j.State = "queued"
	j.Phase = "queued"
	j.Error = ""
	j.UpdatedAt = now()
	if j.Kind == "collect" {
		var n domain.Note
		if !a.get(c, "notes", j.EntityID, &n) {
			return
		}
		n.Status, n.Error = "queued", ""
		if n.MetadataStatus != "succeeded" {
			n.MetadataStatus = "queued"
			if n.Title == "" || strings.HasPrefix(n.Title, "采集失败") {
				n.Title = "等待采集"
			}
		}
		err := a.Store.Atomic(func(tx *store.Transaction) error {
			if err := tx.Put("notes", n.ID, n); err != nil {
				return err
			}
			return tx.Put("jobs", j.ID, j)
		})
		if err != nil {
			fail(c, 500, "恢复采集任务失败")
			return
		}
		c.JSON(202, gin.H{"job_id": j.ID})
	} else if a.put(c, "jobs", j.ID, j) {
		c.JSON(202, gin.H{"job_id": j.ID})
	}
}
func (a *App) progress(j *domain.Job, phase string, p int, msg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	j.Phase = phase
	j.Progress = p
	j.Message = msg
	j.UpdatedAt = now()
	if e := a.Store.Put("jobs", j.ID, j); e != nil && !errors.Is(e, store.ErrJobCleared) {
		log.Printf("job progress: %v", e)
	}
}
func objectError(prefix string, e error) error { return fmt.Errorf("%s: %w", prefix, e) }

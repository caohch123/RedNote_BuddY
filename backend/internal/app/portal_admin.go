package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

type adminUser struct {
	Account
	DailyRequests int `json:"daily_requests"`
}

type adminUsageUser struct {
	UserID            string  `json:"user_id"`
	Username          string  `json:"username"`
	DisplayName       string  `json:"display_name"`
	Requests          int     `json:"requests"`
	Tokens            int64   `json:"tokens"`
	EstimatedCNY      float64 `json:"estimated_cny"`
	DailyRequests     int     `json:"daily_requests"`
	DailyRequestLimit int     `json:"daily_request_limit"`
}

type adminJob struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Phase     string `json:"phase"`
	Progress  int    `json:"progress"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Attempts  int    `json:"attempts"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
}

type adminModelSettings struct {
	Providers    map[string]domain.ProviderConfig `json:"providers"`
	CollectorURL string                           `json:"collector_url"`
	FFmpegPath   string                           `json:"ffmpeg_path"`
	FFprobePath  string                           `json:"ffprobe_path"`
	NodePath     string                           `json:"node_path"`
}

func (p *Portal) routesAdmin(g *gin.RouterGroup) {
	g.GET("/users", p.adminUsers)
	g.POST("/users", p.adminCreateUser)
	g.PATCH("/users/:id", p.adminUpdateUser)
	g.POST("/users/:id/reset-password", p.adminResetPassword)
	g.GET("/overview", p.adminOverview)
	g.GET("/jobs", p.adminJobs)
	g.POST("/users/:id/jobs/:job/retry", p.adminRetryJob)
	g.POST("/users/:id/jobs/:job/cancel", p.adminCancelJob)
	g.GET("/models", p.adminModels)
	g.PATCH("/models", p.adminUpdateModels)
	g.GET("/audit", p.adminAudit)
}

func (p *Portal) adminUsers(c *gin.Context) {
	accounts, err := p.accounts()
	if err != nil {
		fail(c, 500, "读取账号列表失败")
		return
	}
	items := make([]adminUser, 0, len(accounts))
	for _, account := range accounts {
		count, e := p.dailyRequests(account.ID)
		if e != nil {
			fail(c, 500, "读取账号用量失败")
			return
		}
		items = append(items, adminUser{Account: account.Account, DailyRequests: count})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Username < items[j].Username })
	c.JSON(200, gin.H{"items": items, "total": len(items)})
}

func (p *Portal) adminCreateUser(c *gin.Context) {
	var body struct {
		Username          string `json:"username"`
		DisplayName       string `json:"display_name"`
		Role              string `json:"role"`
		DailyRequestLimit int    `json:"daily_request_limit"`
		MaxConcurrent     *int   `json:"max_concurrent"`
	}
	if !bind(c, &body) {
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	if !accountName.MatchString(body.Username) {
		fail(c, 400, "账号须以小写字母开头，使用 3 至 32 位小写字母、数字、下划线或短横线")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if body.Role != "admin" && body.Role != "member" {
		fail(c, 400, "账号角色无效")
		return
	}
	if body.DisplayName == "" {
		body.DisplayName = body.Username
	}
	concurrent := 2
	if body.MaxConcurrent != nil {
		concurrent = *body.MaxConcurrent
	}
	if utf8.RuneCountInString(body.DisplayName) > 80 || !validAdminQuota(body.DailyRequestLimit, concurrent) {
		fail(c, 400, "名称或额度设置无效：日请求额度须为非负数，并发数须为 1 至 8")
		return
	}
	password := randomSecret()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, 500, "创建初始密码失败")
		return
	}
	p.mu.Lock()
	var existing accountRecord
	err = p.identity.Get("accounts", body.Username, &existing)
	if err == nil {
		p.mu.Unlock()
		fail(c, 409, "账号已存在")
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		p.mu.Unlock()
		fail(c, 500, "读取账号失败")
		return
	}
	account := accountRecord{Account: Account{ID: ID(), Username: body.Username, DisplayName: body.DisplayName, Role: body.Role, Enabled: true, MustChangePassword: true, CreatedAt: now(), DailyRequestLimit: body.DailyRequestLimit, MaxConcurrent: concurrent}, PasswordHash: string(hash), Version: 1}
	err = p.identity.Put("accounts", account.Username, account)
	p.mu.Unlock()
	if err != nil {
		fail(c, 500, "创建账号失败")
		return
	}
	p.audit(portalActor(c), "user.create", account.ID, "success")
	c.JSON(201, gin.H{"user": account.Account, "temporary_password": password})
}

func validAdminQuota(daily, concurrent int) bool {
	return daily >= 0 && daily <= 1_000_000 && concurrent >= 1 && concurrent <= 8
}

func (p *Portal) adminUpdateUser(c *gin.Context) {
	var body struct {
		Enabled           *bool   `json:"enabled"`
		DailyRequestLimit *int    `json:"daily_request_limit"`
		MaxConcurrent     *int    `json:"max_concurrent"`
		DisplayName       *string `json:"display_name"`
	}
	if !bind(c, &body) {
		return
	}
	p.mu.Lock()
	account, err := p.accountByID(c.Param("id"))
	if err != nil {
		p.mu.Unlock()
		adminAccountError(c, err)
		return
	}
	if body.Enabled != nil && !*body.Enabled {
		if account.ID == portalActor(c).ID {
			p.mu.Unlock()
			fail(c, 409, "不能停用当前登录账号")
			return
		}
		if account.Role == "admin" && account.Enabled {
			accounts, e := p.accounts()
			if e != nil {
				p.mu.Unlock()
				fail(c, 500, "读取管理员失败")
				return
			}
			active := 0
			for _, a := range accounts {
				if a.Role == "admin" && a.Enabled {
					active++
				}
			}
			if active <= 1 {
				p.mu.Unlock()
				fail(c, 409, "至少保留一个启用的管理员")
				return
			}
		}
	}
	if body.Enabled != nil {
		account.Enabled = *body.Enabled
	}
	if body.DailyRequestLimit != nil {
		account.DailyRequestLimit = *body.DailyRequestLimit
	}
	if body.MaxConcurrent != nil {
		account.MaxConcurrent = *body.MaxConcurrent
	}
	if body.DisplayName != nil {
		account.DisplayName = strings.TrimSpace(*body.DisplayName)
	}
	if account.DisplayName == "" || utf8.RuneCountInString(account.DisplayName) > 80 || !validAdminQuota(account.DailyRequestLimit, account.MaxConcurrent) {
		p.mu.Unlock()
		fail(c, 400, "名称或额度设置无效")
		return
	}
	account.Version++
	err = p.identity.Put("accounts", account.Username, account)
	p.mu.Unlock()
	if err != nil {
		fail(c, 500, "更新账号失败")
		return
	}
	if !account.Enabled {
		p.stopAccountWork(account.ID)
	} else if body.Enabled != nil && account.Role == "member" {
		if _, err = p.runtimeFor(account); err != nil {
			p.audit(portalActor(c), "user.update", account.ID, "runtime_failed")
			fail(c, 500, "账号已更新，但工作区启动失败，请检查服务后重新启用")
			return
		}
	}
	p.audit(portalActor(c), "user.update", account.ID, "success")
	c.JSON(200, gin.H{"user": account.Account})
}

func adminAccountError(c *gin.Context, err error) {
	if errors.Is(err, store.ErrNotFound) {
		fail(c, 404, "账号不存在")
	} else {
		fail(c, 500, "读取账号失败")
	}
}

func (p *Portal) adminResetPassword(c *gin.Context) {
	password := randomSecret()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fail(c, 500, "生成密码失败")
		return
	}
	p.mu.Lock()
	account, err := p.accountByID(c.Param("id"))
	if err != nil {
		p.mu.Unlock()
		adminAccountError(c, err)
		return
	}
	account.PasswordHash = string(hash)
	account.MustChangePassword = true
	account.Version++
	err = p.identity.Put("accounts", account.Username, account)
	p.mu.Unlock()
	if err != nil {
		fail(c, 500, "重置密码失败")
		return
	}
	p.audit(portalActor(c), "user.reset_password", account.ID, "success")
	c.JSON(200, gin.H{"user": account.Account, "temporary_password": password})
}

func (p *Portal) adminOverview(c *gin.Context) {
	zone := c.DefaultQuery("timezone", "Asia/Shanghai")
	loc, err := time.LoadLocation(zone)
	if err != nil || len(zone) > 100 {
		fail(c, 400, "时区无效")
		return
	}
	stamp := time.Now().In(loc)
	start := time.Date(stamp.Year(), stamp.Month(), stamp.Day(), 0, 0, 0, 0, loc)
	filter := domain.UsageFilter{From: start.UnixMilli(), To: start.AddDate(0, 0, 1).UnixMilli()}
	accounts, err := p.accounts()
	if err != nil {
		fail(c, 500, "读取账号失败")
		return
	}
	totals := usageTotals{}
	enabled, pending, failed := 0, 0, 0
	perUser := []adminUsageUser{}
	for _, account := range accounts {
		if account.Enabled {
			enabled++
		}
		if account.Role != "member" {
			continue
		}
		a, e := p.runtimeFor(account)
		if e != nil {
			fail(c, 500, "读取用户工作区失败")
			return
		}
		usage, e := a.Store.Usage(filter)
		if e != nil {
			fail(c, 500, "读取用量失败")
			return
		}
		userTotals := usageTotals{}
		for _, v := range usage {
			addUsage(&totals, v)
			addUsage(&userTotals, v)
		}
		jobs, e := readList[domain.Job](a, "jobs")
		if e != nil {
			fail(c, 500, "读取任务失败")
			return
		}
		for _, j := range jobs {
			if j.State == "failed" {
				failed++
			} else if !completedJob(j.State) {
				pending++
			}
		}
		count, e := p.dailyRequests(account.ID)
		if e != nil {
			fail(c, 500, "读取每日额度失败")
			return
		}
		perUser = append(perUser, adminUsageUser{UserID: account.ID, Username: account.Username, DisplayName: account.DisplayName, Requests: userTotals.Requests, Tokens: userTotals.TotalTokens, EstimatedCNY: userTotals.EstimatedCNY, DailyRequests: count, DailyRequestLimit: account.DailyRequestLimit})
	}
	sort.Slice(perUser, func(i, j int) bool { return perUser[i].Username < perUser[j].Username })
	c.JSON(200, gin.H{"users": len(accounts), "enabled_users": enabled, "requests": totals.Requests, "tokens": totals.TotalTokens, "estimated_cny": totals.EstimatedCNY, "pending_jobs": pending, "failed_jobs": failed, "unreported": totals.Missing, "per_user": perUser, "timezone": zone, "from": start.Format(time.RFC3339), "to": start.AddDate(0, 0, 1).Format(time.RFC3339)})
}

func (p *Portal) adminJobs(c *gin.Context) {
	accounts, err := p.accounts()
	if err != nil {
		fail(c, 500, "读取账号失败")
		return
	}
	items := []adminJob{}
	for _, account := range accounts {
		if account.Role != "member" || (c.Query("user_id") != "" && account.ID != c.Query("user_id")) {
			continue
		}
		a, e := p.runtimeFor(account)
		if e != nil {
			fail(c, 500, "读取用户工作区失败")
			return
		}
		jobs, e := readList[domain.Job](a, "jobs")
		if e != nil {
			fail(c, 500, "读取任务失败")
			return
		}
		for _, j := range jobs {
			if c.Query("state") != "" && j.State != c.Query("state") {
				continue
			}
			items = append(items, adminJob{ID: j.ID, Kind: j.Kind, State: j.State, Phase: j.Phase, Progress: j.Progress, Message: portalJobMessage(a, j, j.Message), Error: portalJobMessage(a, j, j.Error), CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, Attempts: j.Attempts, UserID: account.ID, Username: account.Username})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	c.JSON(200, gin.H{"items": items, "total": len(items)})
}

func portalJobMessage(a *App, j domain.Job, message string) string {
	for _, snapshot := range append(j.ProviderSnapshotHistory, j.ProviderSnapshot) {
		for _, provider := range snapshot {
			if provider.APIKey != "" {
				message = strings.ReplaceAll(message, provider.APIKey, "[redacted]")
			}
		}
	}
	if message == "" {
		return ""
	}
	return a.safeError(errors.New(message))
}

func (p *Portal) adminJobTarget(c *gin.Context) (*App, accountRecord, domain.Job, bool) {
	account, err := p.accountByID(c.Param("id"))
	if err != nil {
		adminAccountError(c, err)
		return nil, accountRecord{}, domain.Job{}, false
	}
	if account.Role != "member" {
		fail(c, 400, "管理员账号没有创作任务")
		return nil, account, domain.Job{}, false
	}
	a, err := p.runtimeFor(account)
	if err != nil {
		fail(c, 500, "读取用户工作区失败")
		return nil, account, domain.Job{}, false
	}
	var j domain.Job
	if e := a.Store.Get("jobs", c.Param("job"), &j); e != nil {
		if errors.Is(e, store.ErrNotFound) {
			fail(c, 404, "任务不存在")
		} else {
			fail(c, 500, "读取任务失败")
		}
		return nil, account, j, false
	}
	return a, account, j, true
}

func (p *Portal) adminRetryJob(c *gin.Context) {
	a, account, j, ok := p.adminJobTarget(c)
	if !ok {
		return
	}
	if !account.Enabled {
		fail(c, 409, "请先启用该账号")
		return
	}
	if j.Kind == "publish" || j.Kind == "publish_prepare" || j.State == "submission_unknown" {
		fail(c, 409, "任务需要用户核实或重新确认，不能由管理端直接重试")
		return
	}
	p.forward(c, a, "/api/v1/jobs/"+j.ID+"/retry")
	result := "rejected"
	if c.Writer.Status() < 400 {
		result = "success"
	}
	p.audit(portalActor(c), "job.retry", account.ID+"/"+j.ID, result)
}

func (p *Portal) adminCancelJob(c *gin.Context) {
	a, account, j, ok := p.adminJobTarget(c)
	if !ok {
		return
	}
	a.mu.Lock()
	err := a.Store.Get("jobs", j.ID, &j)
	if err != nil {
		a.mu.Unlock()
		fail(c, 404, "任务不存在")
		return
	}
	if j.State != "queued" || a.jobRuns[j.ID] != nil || (j.Phase != "" && j.Phase != "queued") || j.RemoteTaskID != "" || len(j.RemoteTaskIDs) > 0 {
		a.mu.Unlock()
		fail(c, 409, "仅可安全取消尚未执行的排队任务")
		return
	}
	j.State, j.Phase, j.Message, j.Error, j.UpdatedAt = "cancelled", "cancelled", "任务已由管理员取消", "任务已由管理员取消", now()
	err = a.Store.Put("jobs", j.ID, j)
	if err == nil {
		a.jobEntityState(j)
	}
	a.mu.Unlock()
	if err != nil {
		fail(c, 500, "取消任务失败")
		return
	}
	p.audit(portalActor(c), "job.cancel", account.ID+"/"+j.ID, "success")
	c.JSON(200, gin.H{"job_id": j.ID, "state": "cancelled"})
}

func adminModelResponse(settings domain.Settings) adminModelSettings {
	items := make(map[string]domain.ProviderConfig, len(settings.Providers))
	for key, provider := range settings.Providers {
		provider.Configured = provider.APIKey != "" || provider.Configured
		provider.APIKey = ""
		if provider.Configured {
			provider.KeyMask = "••••••••"
		}
		items[key] = provider
	}
	return adminModelSettings{Providers: items, CollectorURL: settings.CollectorURL, FFmpegPath: settings.FFmpegPath, FFprobePath: settings.FFprobePath, NodePath: settings.NodePath}
}

func (p *Portal) adminModels(c *gin.Context) {
	s, err := p.root.settings()
	if err != nil {
		fail(c, 500, "读取模型配置失败")
		return
	}
	c.JSON(200, adminModelResponse(s))
}

type portalSettingsReply struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (w *portalSettingsReply) Header() http.Header { return w.header }
func (w *portalSettingsReply) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *portalSettingsReply) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	return w.body.Write(b)
}

func (p *Portal) adminUpdateModels(c *gin.Context) {
	var input map[string]json.RawMessage
	if !bind(c, &input) || input == nil {
		if !c.IsAborted() {
			fail(c, 400, "模型配置不能为空")
		}
		return
	}
	allowed := map[string]bool{"providers": true, "collector_url": true, "ffmpeg_path": true, "ffprobe_path": true, "node_path": true}
	for key := range input {
		if !allowed[key] {
			fail(c, 400, "管理端只可修改平台模型和运行工具配置")
			return
		}
	}
	if raw, ok := input["providers"]; ok {
		var providers map[string]map[string]json.RawMessage
		if json.Unmarshal(raw, &providers) != nil || providers == nil {
			fail(c, 400, "模型配置格式无效")
			return
		}
		for _, fields := range providers {
			for field := range fields {
				if !has([]string{"base_url", "model", "api_key", "workspace_id", "configured", "key_mask"}, field) {
					fail(c, 400, "模型配置包含不支持的字段")
					return
				}
			}
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		fail(c, 400, "模型配置格式无效")
		return
	}
	request := c.Request.Clone(c.Request.Context())
	request.Method = http.MethodPatch
	urlCopy := *request.URL
	urlCopy.Path = "/settings"
	request.URL = &urlCopy
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.ContentLength = int64(len(data))
	request.Header = request.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	router := gin.New()
	p.root.routesSettings(router.Group(""))
	reply := &portalSettingsReply{header: make(http.Header)}
	router.ServeHTTP(reply, request)
	if reply.code >= 400 {
		p.audit(portalActor(c), "models.update", "platform", "rejected")
		c.Data(reply.code, "application/json; charset=utf-8", reply.body.Bytes())
		return
	}
	var settings domain.Settings
	if json.Unmarshal(reply.body.Bytes(), &settings) != nil {
		fail(c, 500, "读取保存结果失败")
		return
	}
	p.audit(portalActor(c), "models.update", "platform", "success")
	c.JSON(200, adminModelResponse(settings))
}

func (p *Portal) adminAudit(c *gin.Context) {
	rows, err := p.identity.List("audit")
	if err != nil {
		fail(c, 500, "读取审计日志失败")
		return
	}
	items, err := store.Decode[AuditEvent](rows)
	if err != nil {
		fail(c, 500, "读取审计日志失败")
		return
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
	if len(items) > 500 {
		items = items[:500]
	}
	c.JSON(200, gin.H{"items": items, "total": len(items)})
}

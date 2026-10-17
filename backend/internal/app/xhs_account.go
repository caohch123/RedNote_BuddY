package app

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

// xhsRPC is deliberately small so account and publish handlers can be tested
// without a browser, credentials, or a real platform side effect.
type xhsRPC interface {
	Call(context.Context, string, any, any) error
	Close() error
}

type xhsAccount struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	RedID    string `json:"red_id,omitempty"`
}

type xhsAccountStatus struct {
	Status          string      `json:"status"`
	Account         *xhsAccount `json:"account,omitempty"`
	Message         string      `json:"message,omitempty"`
	DependencyReady bool        `json:"dependency_ready"`
}

type xhsLogin struct {
	ID                  string      `json:"login_id"`
	Status              string      `json:"status"`
	QRImage             string      `json:"qr_image,omitempty"`
	ExpiresAt           string      `json:"expires_at"`
	Message             string      `json:"message,omitempty"`
	Account             *xhsAccount `json:"account,omitempty"`
	remoteID            string
	remoteAuthenticated bool
}

type savedXHSAccount struct {
	EncryptedSession string     `json:"encrypted_session"`
	Account          xhsAccount `json:"account"`
}

type xhsState struct {
	mu sync.Mutex
	// op serializes login/export/logout with a publish, keeping the selected
	// account stable throughout a platform request.
	op         sync.Mutex
	rpc        xhsRPC
	status     xhsAccountStatus
	session    string
	generation string
	logins     map[string]*xhsLogin
	tokens     map[string]xhsPublishToken
	loaded     bool
	refreshing bool
	lastCheck  time.Time
	clock      func() time.Time
}

func newXHSState(root string, nodePath ...func() (string, error)) *xhsState {
	return &xhsState{rpc: providers.NewXHSPublisher(root, nodePath...), status: xhsAccountStatus{Status: "logged_out", Message: "请扫描二维码登录本人小红书账号"}, generation: ID(), logins: map[string]*xhsLogin{}, tokens: map[string]xhsPublishToken{}, clock: time.Now}
}

func (a *App) routesXHS(g *gin.RouterGroup) {
	g.GET("/xhs/account", a.getXHSAccount)
	g.POST("/xhs/login", a.startXHSLogin)
	g.GET("/xhs/login/:id", a.getXHSLogin)
	g.DELETE("/xhs/account", a.clearXHSAccount)
	g.POST("/publications/:id/publish-preview", a.previewXHSPublish)
	g.POST("/publications/:id/publish", a.submitXHSPublish)
}

func safeXHSProfile(v *xhsAccount) *xhsAccount {
	if v == nil || strings.TrimSpace(v.ID) == "" || len(v.ID) > 128 {
		return nil
	}
	out := &xhsAccount{ID: strings.TrimSpace(v.ID), Nickname: trimMessage(v.Nickname, 200), RedID: trimMessage(v.RedID, 128)}
	u, e := url.Parse(v.Avatar)
	if e == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && (allowedMediaHost(u.Hostname()) || strings.HasSuffix(u.Hostname(), ".xhscdn.net")) {
		out.Avatar = u.String()
	}
	return out
}

// Never use arbitrary bridge messages as UI errors; they may contain request
// headers, signed upload URLs, or the account's session cookie.
func xhsErrorMessage(err error) string {
	var p *providers.XHSPublisherError
	if errors.As(err, &p) {
		switch strings.ToUpper(p.Code) {
		case "AUTH_EXPIRED", "NOT_LOGGED_IN", "SESSION_EXPIRED", "SESSION_INVALID":
			return "小红书登录已失效，请重新扫码登录"
		case "DEPENDENCY_MISSING", "BROWSER_MISSING", "BRIDGE_UNAVAILABLE":
			return "扫码发布依赖不可用，请运行依赖检查并安装发布服务"
		case "RISK_CONTROL", "CAPTCHA_REQUIRED", "VERIFICATION_REQUIRED":
			return "小红书要求安全验证，请在本人账号完成验证后重新操作"
		case "PUBLISH_REJECTED", "INVALID_CONTENT":
			return "小红书明确拒绝了本次发布，请检查文案和素材后重新预览"
		case "LOGIN_EXPIRED":
			return "二维码已过期，请重新获取二维码"
		case "TOPIC_NOT_FOUND":
			return "未找到指定的小红书话题，请修改话题后重新确认"
		case "UPLOAD_FAILED":
			return "素材上传失败，笔记尚未提交，请检查网络后重新预览"
		case "VALIDATION_FAILED", "ASSET_CHANGED":
			return "发布稿或素材未通过检查，请修改后重新预览"
		}
	}
	return "小红书服务暂时不可用，请检查本机发布依赖和网络"
}

// Session-class bridge errors mean the saved login can never succeed on retry;
// only a fresh QR scan recovers. Keep this list in sync with xhsErrorMessage.
func isXHSSessionError(err error) bool {
	var p *providers.XHSPublisherError
	if !errors.As(err, &p) {
		return false
	}
	switch strings.ToUpper(p.Code) {
	case "AUTH_EXPIRED", "NOT_LOGGED_IN", "SESSION_EXPIRED", "SESSION_INVALID":
		return true
	}
	return false
}

// discardSavedXHSAccount drops an unusable saved session so the login entry
// stays available for a fresh QR scan instead of a disabled dead end.
func (a *App) discardSavedXHSAccount(message string) {
	_ = a.Store.Delete("xhs_accounts", "main")
	x := a.xhs
	x.mu.Lock()
	x.session = ""
	x.status = xhsAccountStatus{Status: "logged_out", DependencyReady: true, Message: message}
	x.mu.Unlock()
}

func (a *App) getXHSAccount(c *gin.Context) {
	a.refreshXHSAccountAsync()
	a.xhs.mu.Lock()
	result := a.xhs.status
	a.xhs.mu.Unlock()
	c.JSON(200, result)
}

func (a *App) refreshXHSAccountAsync() {
	x := a.xhs
	x.mu.Lock()
	if x.refreshing || (!x.lastCheck.IsZero() && x.clock().Sub(x.lastCheck) < 15*time.Second) || a.ctx.Err() != nil {
		x.mu.Unlock()
		return
	}
	x.refreshing = true
	x.lastCheck = x.clock()
	x.mu.Unlock()
	if !a.background(func() {
		x.op.Lock()
		defer x.op.Unlock()
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		a.restoreXHSAccount(ctx)
		_, _ = a.checkXHSAccount(ctx)
		x.mu.Lock()
		x.refreshing = false
		x.mu.Unlock()
	}) {
		x.mu.Lock()
		x.refreshing = false
		x.mu.Unlock()
	}
}

func (a *App) restoreXHSAccount(ctx context.Context) {
	x := a.xhs
	x.mu.Lock()
	loaded := x.loaded
	x.loaded = true
	x.mu.Unlock()
	if loaded {
		return
	}
	var saved savedXHSAccount
	if e := a.Store.Get("xhs_accounts", "main", &saved); e != nil {
		if !errors.Is(e, store.ErrNotFound) {
			x.mu.Lock()
			x.status = xhsAccountStatus{Status: "unavailable", Message: "读取本机登录状态失败"}
			x.mu.Unlock()
		}
		return
	}
	if !strings.HasPrefix(saved.EncryptedSession, "dpapi:") {
		a.discardSavedXHSAccount("登录凭据格式无效，请重新扫码")
		return
	}
	session, e := cryptSecret(strings.TrimPrefix(saved.EncryptedSession, "dpapi:"), false)
	if e != nil || session == "" {
		a.discardSavedXHSAccount("当前 Windows 用户无法解密登录状态，请重新扫码")
		return
	}
	var result xhsAccountStatus
	if e = x.rpc.Call(ctx, "account_restore", map[string]string{"session": session}, &result); e != nil {
		if isXHSSessionError(e) {
			a.discardSavedXHSAccount("小红书登录已失效，请重新扫码登录")
			return
		}
		x.mu.Lock()
		x.status = xhsAccountStatus{Status: "unavailable", Message: xhsErrorMessage(e)}
		x.loaded = false
		x.mu.Unlock()
		return
	}
	x.mu.Lock()
	x.session = session
	x.mu.Unlock()
	// Saved profile is display-only. Only a subsequent live account_status
	// verification may set the account to logged_in.
}

// checkXHSAccount requires a live platform verification and is called just
// before previewing and just before submitting. GET account uses cached state.
func (a *App) checkXHSAccount(ctx context.Context) (*xhsAccount, error) {
	x := a.xhs
	var health struct {
		Available bool `json:"available"`
	}
	if e := x.rpc.Call(ctx, "health", map[string]any{}, &health); e != nil || !health.Available {
		if e == nil {
			e = errors.New("publisher dependency unavailable")
		}
		x.mu.Lock()
		x.status = xhsAccountStatus{Status: "unavailable", Message: xhsErrorMessage(e)}
		x.mu.Unlock()
		return nil, e
	}
	var result xhsAccountStatus
	if e := x.rpc.Call(ctx, "account_status", map[string]any{}, &result); e != nil {
		x.mu.Lock()
		if isXHSSessionError(e) {
			// 平台拒绝的是本会话本身，重新扫码即可恢复；登录入口必须保持可用。
			x.status = xhsAccountStatus{Status: "logged_out", DependencyReady: true, Message: "小红书登录已失效，请重新扫码登录"}
		} else {
			x.status = xhsAccountStatus{Status: "unavailable", Message: xhsErrorMessage(e)}
		}
		x.mu.Unlock()
		return nil, e
	}
	result.DependencyReady = true
	result.Account = safeXHSProfile(result.Account)
	result.Message = ""
	if result.Status == "logged_in" && result.Account != nil {
		x.mu.Lock()
		old := x.status.Account
		hasSession := x.session != ""
		x.mu.Unlock()
		if !hasSession {
			result.Status = "logged_out"
			result.Account = nil
			result.Message = "请在工作台扫码登录账号"
		} else if old != nil && old.ID != result.Account.ID {
			x.mu.Lock()
			x.generation = ID()
			x.tokens = map[string]xhsPublishToken{}
			x.mu.Unlock()
		}
	} else {
		result.Account = nil
		if result.Status == "expired" {
			result.Message = "登录已失效，请重新扫码"
		} else {
			result.Status = "logged_out"
			result.Message = "请扫描二维码登录本人账号"
		}
	}
	x.mu.Lock()
	x.status = result
	x.lastCheck = x.clock()
	x.mu.Unlock()
	if result.Status != "logged_in" {
		return nil, errors.New(result.Message)
	}
	return result.Account, nil
}

func (a *App) startXHSLogin(c *gin.Context) {
	x := a.xhs
	x.mu.Lock()
	for id, l := range x.logins {
		if ex, e := time.Parse(time.RFC3339, l.ExpiresAt); e == nil && !x.clock().Before(ex) {
			delete(x.logins, id)
			continue
		}
		if l.Status == "initializing" || l.Status == "waiting_scan" || l.Status == "scanned" {
			x.mu.Unlock()
			c.JSON(202, gin.H{"login_id": l.ID})
			return
		}
	}
	login := &xhsLogin{ID: ID(), Status: "initializing", ExpiresAt: x.clock().Add(5 * time.Minute).UTC().Format(time.RFC3339), Message: "正在获取小红书登录二维码"}
	x.logins[login.ID] = login
	x.mu.Unlock()
	if !a.background(func() { a.runXHSLogin(login.ID) }) {
		a.failXHSLogin(login.ID, errors.New("service stopping"))
		fail(c, 503, "服务正在停止，请稍后重试")
		return
	}
	c.JSON(202, gin.H{"login_id": login.ID})
}

func (a *App) getXHSLogin(c *gin.Context) {
	x := a.xhs
	x.mu.Lock()
	defer x.mu.Unlock()
	l, ok := x.logins[c.Param("id")]
	if !ok {
		fail(c, 404, "登录会话不存在，请重新获取二维码")
		return
	}
	if ex, e := time.Parse(time.RFC3339, l.ExpiresAt); e == nil && !x.clock().Before(ex) && l.Status != "logged_in" && l.Status != "failed" {
		l.Status = "expired"
		l.QRImage = ""
		l.Message = "二维码已过期，请重新获取"
	}
	c.JSON(200, *l)
}

func (a *App) updateXHSLogin(id string, result xhsLogin) bool {
	x := a.xhs
	x.mu.Lock()
	defer x.mu.Unlock()
	l, ok := x.logins[id]
	if !ok {
		return false
	}
	if l.Status == "expired" || l.Status == "failed" {
		return false
	}
	if result.ID != "" {
		l.remoteID = result.ID
	}
	switch result.Status {
	case "starting", "initializing":
		l.Status = "initializing"
		l.Message = "正在获取二维码"
	case "waiting_scan":
		l.Status = "waiting_scan"
		l.Message = "请使用小红书 App 扫码"
	case "waiting_confirm", "scanned":
		l.Status = "scanned"
		l.Message = "已扫码，请在小红书 App 确认登录"
	case "logged_in":
		l.Status = "scanned"
		l.Message = "正在核验账号并保存本机登录状态"
		l.remoteAuthenticated = true
		l.QRImage = ""
	case "expired":
		l.Status = "expired"
		l.Message = "二维码已过期，请重新获取"
		l.QRImage = ""
	case "error", "failed":
		l.Status = "failed"
		l.Message = "登录失败，请重新获取二维码"
		l.QRImage = ""
	default:
		l.Status = "failed"
		l.Message = "登录服务返回无效状态"
	}
	if (l.Status == "waiting_scan" || l.Status == "scanned") && len(result.QRImage) <= 2<<20 && strings.HasPrefix(result.QRImage, "data:image/png;base64,") {
		l.QRImage = result.QRImage
	}
	if ex, e := time.Parse(time.RFC3339, result.ExpiresAt); e == nil && ex.After(x.clock()) && ex.Before(x.clock().Add(5*time.Minute)) {
		l.ExpiresAt = ex.UTC().Format(time.RFC3339)
	}
	return true
}

func (a *App) failXHSLogin(id string, err error) {
	x := a.xhs
	x.mu.Lock()
	defer x.mu.Unlock()
	if l := x.logins[id]; l != nil {
		l.Status = "failed"
		l.Message = xhsErrorMessage(err)
		l.QRImage = ""
	}
}

func (a *App) runXHSLogin(id string) {
	x := a.xhs
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	var result xhsLogin
	x.op.Lock()
	x.mu.Lock()
	valid := x.logins[id] != nil
	x.mu.Unlock()
	if !valid {
		x.op.Unlock()
		return
	}
	e := x.rpc.Call(ctx, "login_start", map[string]any{}, &result)
	x.op.Unlock()
	if e != nil {
		a.failXHSLogin(id, e)
		return
	}
	if !a.updateXHSLogin(id, result) {
		return
	}
	for {
		x.mu.Lock()
		l := x.logins[id]
		if l == nil {
			x.mu.Unlock()
			return
		}
		status, remote, expiry, authenticated := l.Status, l.remoteID, l.ExpiresAt, l.remoteAuthenticated
		x.mu.Unlock()
		if authenticated {
			x.op.Lock()
			e = a.persistXHSLogin(ctx, id)
			x.op.Unlock()
			if e != nil {
				a.failXHSLogin(id, e)
			}
			return
		}
		if status == "expired" || status == "failed" {
			return
		}
		if ex, e := time.Parse(time.RFC3339, expiry); e != nil || !x.clock().Before(ex) {
			a.updateXHSLogin(id, xhsLogin{Status: "expired"})
			return
		}
		select {
		case <-ctx.Done():
			a.updateXHSLogin(id, xhsLogin{Status: "expired"})
			return
		case <-time.After(2 * time.Second):
		}
		if remote == "" {
			a.failXHSLogin(id, errors.New("missing login ID"))
			return
		}
		result = xhsLogin{}
		x.op.Lock()
		e = x.rpc.Call(ctx, "login_status", map[string]string{"login_id": remote}, &result)
		x.op.Unlock()
		if e != nil {
			a.failXHSLogin(id, e)
			return
		}
		if !a.updateXHSLogin(id, result) {
			return
		}
	}
}

func (a *App) persistXHSLogin(ctx context.Context, id string) error {
	x := a.xhs
	x.mu.Lock()
	valid := x.logins[id] != nil && x.logins[id].remoteAuthenticated
	x.mu.Unlock()
	if !valid {
		return errors.New("login session cancelled")
	}
	var exported struct {
		Session string      `json:"session"`
		Account *xhsAccount `json:"account"`
	}
	if e := x.rpc.Call(ctx, "account_export", map[string]any{}, &exported); e != nil {
		return e
	}
	profile := safeXHSProfile(exported.Account)
	if exported.Session == "" || len(exported.Session) > 2<<20 || profile == nil {
		return errors.New("invalid account export")
	}
	encrypted, e := cryptSecret(exported.Session, true)
	if e != nil {
		return e
	}
	if e = a.Store.Put("xhs_accounts", "main", savedXHSAccount{EncryptedSession: "dpapi:" + encrypted, Account: *profile}); e != nil {
		return e
	}
	x.mu.Lock()
	x.session = exported.Session
	x.loaded = true
	x.generation = ID()
	x.tokens = map[string]xhsPublishToken{}
	x.mu.Unlock()
	verified, e := a.checkXHSAccount(ctx)
	if e != nil {
		return e
	}
	if verified.ID != profile.ID {
		a.Store.Delete("xhs_accounts", "main")
		x.mu.Lock()
		x.session = ""
		x.status = xhsAccountStatus{Status: "expired", DependencyReady: true, Message: "账号校验不一致，请重新扫码"}
		x.mu.Unlock()
		return errors.New("account mismatch")
	}
	x.mu.Lock()
	if l := x.logins[id]; l != nil {
		l.Account = verified
		l.Status = "logged_in"
		l.QRImage = ""
	}
	x.mu.Unlock()
	return nil
}

func (a *App) clearXHSAccount(c *gin.Context) {
	if a.hasActiveXHSPublish() {
		fail(c, 409, "发布正在执行，请等待结果后退出登录")
		return
	}
	x := a.xhs
	x.op.Lock()
	defer x.op.Unlock()
	if a.hasActiveXHSPublish() {
		fail(c, 409, "发布正在执行，请等待结果后退出登录")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result map[string]any
	// Local removal still succeeds when a browser process has already stopped.
	_ = x.rpc.Call(ctx, "logout", map[string]any{}, &result)
	if e := a.Store.Delete("xhs_accounts", "main"); e != nil {
		fail(c, 500, "清除本机登录状态失败")
		return
	}
	x.mu.Lock()
	x.session = ""
	x.generation = ID()
	x.tokens = map[string]xhsPublishToken{}
	x.logins = map[string]*xhsLogin{}
	x.loaded = true
	x.status = xhsAccountStatus{Status: "logged_out", DependencyReady: true, Message: "已退出本机账号"}
	x.mu.Unlock()
	c.JSON(200, xhsAccountStatus{Status: "logged_out", DependencyReady: true, Message: "已退出本机账号"})
}

func (a *App) hasActiveXHSPublish() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		log.Printf("list jobs for Xiaohongshu account state: %v", err)
		return true
	}
	for _, j := range jobs {
		var publication domain.Publication
		if a.Store.Get("publications", j.EntityID, &publication) == nil && publicationPlatform(publication) != "xiaohongshu" {
			continue
		}
		if j.Kind == "publish" && (j.State == "queued" || j.State == "running") {
			return true
		}
	}
	return false
}

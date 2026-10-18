package app

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const portalActorKey = "portal_actor"

type portalHandler = http.Handler

func portalActor(c *gin.Context) Account {
	value, _ := c.Get(portalActorKey)
	account, _ := value.(Account)
	return account
}
func realmCookie(realm string) string {
	if realm == "admin" {
		return "workbench_admin_session"
	}
	return "workbench_user_session"
}
func realmRole(realm string) string {
	if realm == "admin" {
		return "admin"
	}
	return "member"
}
func csrfEqual(c *gin.Context, s portalSession) bool {
	return subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(s.CSRF)) == 1
}

func (p *Portal) setSession(c *gin.Context, realm string, account *accountRecord) (portalSession, error) {
	token := randomSecret()
	s := portalSession{Realm: realm, CSRF: randomSecret(), LastSeen: time.Now().Unix(), ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	if account != nil {
		s.Username = account.Username
		s.Version = account.Version
		s.ExpiresAt = time.Now().Add(12 * time.Hour).Unix()
	}
	if err := p.identity.Put("sessions", sessionHash(token), s); err != nil {
		return s, err
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: realmCookie(realm), Value: token, Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(time.Until(time.Unix(s.ExpiresAt, 0)).Seconds())})
	return s, nil
}

func (p *Portal) deleteSession(c *gin.Context, realm string) {
	if cookie, err := c.Cookie(realmCookie(realm)); err == nil {
		_ = p.identity.Delete("sessions", sessionHash(cookie))
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: realmCookie(realm), Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func (p *Portal) session(c *gin.Context, realm string) (portalSession, accountRecord, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var s portalSession
	var account accountRecord
	token, err := c.Cookie(realmCookie(realm))
	if err != nil || len(token) > 100 {
		return s, account, false
	}
	key := sessionHash(token)
	if err = p.identity.Get("sessions", key, &s); err != nil || s.Realm != realm || s.ExpiresAt <= time.Now().Unix() || s.LastSeen < time.Now().Add(-30*time.Minute).Unix() {
		return s, account, false
	}
	if s.Username != "" {
		if err = p.identity.Get("accounts", s.Username, &account); err != nil || !account.Enabled || account.Role != realmRole(realm) || account.Version != s.Version {
			return s, account, false
		}
	}
	if s.LastSeen < time.Now().Add(-time.Minute).Unix() {
		s.LastSeen = time.Now().Unix()
		if p.identity.Put("sessions", key, s) != nil {
			return s, account, false
		}
	}
	return s, account, true
}

func (p *Portal) requireAccount(c *gin.Context, realm string, allowPasswordChange bool) (accountRecord, bool) {
	s, account, ok := p.session(c, realm)
	if !ok || s.Username == "" {
		c.AbortWithStatusJSON(401, gin.H{"error": "请登录后继续", "code": "unauthenticated"})
		return account, false
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions && !csrfEqual(c, s) {
		fail(c, 403, "会话校验失败，请刷新后重试")
		return account, false
	}
	if account.MustChangePassword && !allowPasswordChange {
		c.AbortWithStatusJSON(403, gin.H{"error": "首次登录请先修改密码", "code": "password_change_required"})
		return account, false
	}
	c.Set(portalActorKey, account.Account)
	return account, true
}

func (p *Portal) routesAuth(g *gin.RouterGroup, realm string) {
	g.GET("/me", func(c *gin.Context) {
		if account, ok := p.requireAccount(c, realm, true); ok {
			c.JSON(200, gin.H{"user": account.Account})
		}
	})
	g.POST("/login", func(c *gin.Context) { p.login(c, realm) })
	g.POST("/logout", func(c *gin.Context) {
		account, ok := p.requireAccount(c, realm, true)
		if !ok {
			return
		}
		p.mu.Lock()
		p.deleteSession(c, realm)
		p.mu.Unlock()
		p.audit(account.Account, "auth.logout", "", "succeeded")
		c.JSON(200, gin.H{"ok": true})
	})
	g.POST("/password", func(c *gin.Context) { p.changePassword(c, realm) })
}

func (p *Portal) login(c *gin.Context, realm string) {
	s, _, ok := p.session(c, realm)
	if !ok || !csrfEqual(c, s) {
		fail(c, 403, "登录会话已失效，请刷新后重试")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bind(c, &body) {
		return
	}
	body.Username = strings.ToLower(strings.TrimSpace(body.Username))
	if len(body.Username) > 32 || len(body.Password) > 72 {
		fail(c, 401, "账号或密码错误")
		return
	}
	key := realm + ":" + body.Username
	p.mu.Lock()
	if len(p.attempts) > 1000 {
		for name, attempt := range p.attempts {
			if time.Now().After(attempt.Until) {
				delete(p.attempts, name)
			}
		}
		if len(p.attempts) > 1000 {
			p.mu.Unlock()
			fail(c, 429, "登录服务繁忙，请稍后再试")
			return
		}
	}
	attempt := p.attempts[key]
	if attempt.Count >= 5 && time.Now().Before(attempt.Until) {
		p.mu.Unlock()
		c.Header("Retry-After", "900")
		fail(c, 429, "登录尝试过多，请15分钟后再试")
		return
	}
	var account accountRecord
	err := p.identity.Get("accounts", body.Username, &account)
	hash := p.dummyHash
	if err == nil {
		hash = []byte(account.PasswordHash)
	}
	p.mu.Unlock()
	passwordOK := bcrypt.CompareHashAndPassword(hash, []byte(body.Password)) == nil
	p.mu.Lock()
	defer p.mu.Unlock()
	var latest accountRecord
	valid := err == nil && passwordOK && account.Enabled && account.Role == realmRole(realm) && p.identity.Get("accounts", account.Username, &latest) == nil && latest.Version == account.Version && latest.Enabled
	if !valid {
		attempt = p.attempts[key]
		if time.Now().After(attempt.Until) {
			attempt = loginAttempts{Until: time.Now().Add(15 * time.Minute)}
		}
		attempt.Count++
		p.attempts[key] = attempt
		p.audit(Account{}, "auth.login_failed", "", "denied")
		fail(c, 401, "账号或密码错误")
		return
	}
	delete(p.attempts, key)
	latest.LastLogin = now()
	if err = p.identity.Put("accounts", latest.Username, latest); err != nil {
		fail(c, 500, "登录状态保存失败")
		return
	}
	p.deleteSession(c, realm)
	s, err = p.setSession(c, realm, &latest)
	if err != nil {
		fail(c, 500, "登录会话创建失败")
		return
	}
	p.audit(latest.Account, "auth.login", "", "succeeded")
	c.JSON(200, gin.H{"user": latest.Account, "csrf_token": s.CSRF})
}

func (p *Portal) changePassword(c *gin.Context, realm string) {
	account, ok := p.requireAccount(c, realm, true)
	if !ok {
		return
	}
	var body struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if !bind(c, &body) {
		return
	}
	if !validPassword(body.Next) {
		fail(c, 400, "新密码至少12个字符，UTF-8编码最多72字节")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(body.Current)) != nil {
		fail(c, 400, "当前密码错误")
		return
	}
	if body.Next == body.Current {
		fail(c, 400, "新密码不能与当前密码相同")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Next), bcrypt.DefaultCost)
	if err != nil {
		fail(c, 500, "密码更新失败")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var latest accountRecord
	if p.identity.Get("accounts", account.Username, &latest) != nil || latest.Version != account.Version || !latest.Enabled {
		fail(c, 401, "账号状态已变化，请重新登录")
		return
	}
	latest.PasswordHash = string(hash)
	latest.MustChangePassword = false
	latest.Version++
	if err = p.identity.Put("accounts", latest.Username, latest); err != nil {
		fail(c, 500, "密码保存失败")
		return
	}
	p.deleteSession(c, realm)
	s, err := p.setSession(c, realm, &latest)
	if err != nil {
		fail(c, 500, "请使用新密码重新登录")
		return
	}
	p.audit(latest.Account, "auth.password_changed", "", "succeeded")
	c.JSON(200, gin.H{"user": latest.Account, "csrf_token": s.CSRF})
}

func (p *Portal) sessionEndpoint(c *gin.Context) {
	realm := c.DefaultQuery("realm", "user")
	if realm != "user" && realm != "admin" {
		fail(c, 400, "登录入口无效")
		return
	}
	s, _, ok := p.session(c, realm)
	if !ok {
		p.mu.Lock()
		p.deleteSession(c, realm)
		var err error
		s, err = p.setSession(c, realm, nil)
		p.mu.Unlock()
		if err != nil {
			fail(c, 500, "无法初始化登录会话")
			return
		}
	}
	c.JSON(200, gin.H{"csrf_token": s.CSRF})
}

func (p *Portal) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), p.root.boundary(false))
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	})
	g := r.Group("/api/v1")
	g.GET("/session", p.sessionEndpoint)
	g.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"service": "workbench", "status": "ok", "authentication": true})
	})
	p.routesAuth(g.Group("/auth"), "user")
	p.routesAuth(g.Group("/admin/auth"), "admin")
	admin := g.Group("/admin")
	admin.Use(func(c *gin.Context) {
		if _, ok := p.requireAccount(c, "admin", false); ok {
			c.Next()
		}
	})
	p.routesAdmin(admin)
	p.routesAdminBilling(admin)
	r.NoRoute(p.dispatchUser)
	return r
}

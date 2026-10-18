package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"xhs-workbench/internal/domain"
)

type portalBrowser struct {
	p      *Portal
	router http.Handler
	cookie *http.Cookie
	csrf   string
	realm  string
}

func newPortalBrowser(t *testing.T, p *Portal, realm string) *portalBrowser {
	t.Helper()
	b := &portalBrowser{p: p, router: p.Router(), realm: realm}
	r := b.call(t, "GET", "/session?realm="+realm, nil)
	b.csrf = decodeResponse[map[string]string](t, r, 200)["csrf_token"]
	return b
}
func (b *portalBrowser) call(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://127.0.0.1:8080/api/v1"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", b.csrf)
	if b.cookie != nil {
		r.AddCookie(b.cookie)
	}
	w := httptest.NewRecorder()
	b.router.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == realmCookie(b.realm) {
			b.cookie = c
		}
	}
	return w
}
func (b *portalBrowser) authPath() string {
	if b.realm == "admin" {
		return "/admin/auth"
	}
	return "/auth"
}
func (b *portalBrowser) login(t *testing.T, username, password string) Account {
	t.Helper()
	v := decodeResponse[struct {
		User Account `json:"user"`
		CSRF string  `json:"csrf_token"`
	}](t, b.call(t, "POST", b.authPath()+"/login", map[string]string{"username": username, "password": password}), 200)
	b.csrf = v.CSRF
	return v.User
}
func (b *portalBrowser) change(t *testing.T, current, next string) {
	t.Helper()
	v := decodeResponse[struct {
		User Account `json:"user"`
		CSRF string  `json:"csrf_token"`
	}](t, b.call(t, "POST", b.authPath()+"/password", map[string]string{"current_password": current, "new_password": next}), 200)
	if v.User.MustChangePassword || v.CSRF == b.csrf {
		t.Fatal("password change did not rotate session")
	}
	b.csrf = v.CSRF
}
func portalPassword(t *testing.T, p *Portal, username string) string {
	t.Helper()
	text, err := cryptSecret(p.meta.Bootstrap, false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "账号："+username && i+1 < len(lines) {
			return strings.TrimPrefix(lines[i+1], "初始密码：")
		}
	}
	t.Fatal("missing bootstrap account")
	return ""
}
func newPortalFixture(t *testing.T) *Portal {
	t.Helper()
	f := newAPIFixture(t)
	p, e := NewPortal(f.app)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := p.Close(); e != nil {
			t.Error(e)
		}
	})
	return p
}

func TestPortalAuthenticationAndRoleBoundary(t *testing.T) {
	p := newPortalFixture(t)
	b := newPortalBrowser(t, p, "user")
	if r := b.call(t, "GET", "/rewrites", nil); r.Code != 401 {
		t.Fatal("anonymous content access", r.Code)
	}
	if r := b.call(t, "GET", "/admin/users", nil); r.Code != 401 {
		t.Fatal("anonymous admin access", r.Code)
	}
	password := portalPassword(t, p, "user")
	if u := b.login(t, "user", password); !u.MustChangePassword {
		t.Fatal("first login must change password")
	}
	if r := b.call(t, "GET", "/rewrites", nil); r.Code != 403 {
		t.Fatal("business bypass before password change")
	}
	oldCookie, oldCSRF := *b.cookie, b.csrf
	b.change(t, password, "Member-password-2026!")
	if r := b.call(t, "GET", "/rewrites", nil); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	stale := &portalBrowser{p: p, router: p.Router(), realm: "user", cookie: &oldCookie, csrf: oldCSRF}
	if r := stale.call(t, "GET", "/auth/me", nil); r.Code != 401 {
		t.Fatal("old session survived password change")
	}
	if r := b.call(t, "GET", "/admin/users", nil); r.Code != 401 {
		t.Fatal("member entered admin endpoint")
	}
	for _, r := range []*httptest.ResponseRecorder{
		b.call(t, "PATCH", "/settings", map[string]any{"providers": map[string]any{}}),
		b.call(t, "PUT", "/billing/prices", map[string]any{"items": []any{}}),
		b.call(t, "POST", "/settings/test", map[string]any{"capability": "text"}),
	} {
		if r.Code != 403 {
			t.Fatal("member changed platform configuration", r.Code)
		}
	}
	csrf := b.csrf
	b.csrf = "wrong"
	if r := b.call(t, "POST", "/rewrites", map[string]string{}); r.Code != 403 {
		t.Fatal("invalid CSRF accepted")
	}
	b.csrf = csrf
	a := newPortalBrowser(t, p, "admin")
	ap := portalPassword(t, p, "admin")
	a.login(t, "admin", ap)
	a.change(t, ap, "Admin-password-2026!")
	if r := a.call(t, "GET", "/admin/users", nil); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := a.call(t, "GET", "/rewrites", nil); r.Code != 401 {
		t.Fatal("admin read user content through user API")
	}
	if r := b.call(t, "POST", "/auth/logout", map[string]any{}); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if r := b.call(t, "GET", "/rewrites", nil); r.Code != 401 {
		t.Fatal("logout failed")
	}
	if r := a.call(t, "GET", "/admin/users", nil); r.Code != 200 {
		t.Fatal("user logout ended admin session")
	}
}

func TestPortalUserIsolationAndLegacyOwnership(t *testing.T) {
	p := newPortalFixture(t)
	fixture := apiFixture{app: p.root}
	note, asset, _ := seedSource(t, fixture)
	var owner accountRecord
	p.identity.Get("accounts", "user", &owner)
	password := "Second-member-password!"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	second := accountRecord{Account: Account{ID: ID(), Username: "second", Role: "member", Enabled: true, MaxConcurrent: 1}, PasswordHash: string(hash), Version: 1}
	if e := p.identity.Put("accounts", second.Username, second); e != nil {
		t.Fatal(e)
	}
	a, e := p.runtimeFor(second)
	if e != nil {
		t.Fatal(e)
	}
	if a.Store == p.root.Store || !within(filepath.Join(p.root.Data, "accounts"), a.Data) {
		t.Fatal("shared user storage")
	}
	b := newPortalBrowser(t, p, "user")
	b.login(t, "second", password)
	if r := b.call(t, "GET", "/collections/"+note.ID, nil); r.Code != 404 {
		t.Fatal("cross-user note access", r.Code, r.Body.String())
	}
	if r := b.call(t, "GET", "/assets/"+asset.ID+"/file", nil); r.Code != 404 {
		t.Fatal("cross-user asset access", r.Code)
	}
	if r := b.call(t, "GET", "/rewrites", nil); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if r := b.call(t, "PATCH", "/settings", map[string]string{"cookie": "second-private-cookie"}); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	s, _ := p.root.settings()
	if s.Cookie != "" {
		t.Fatal("new user modified primary Cookie")
	}
	if r := b.call(t, "PATCH", "/settings", map[string]string{"cookie": ""}); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	s, _ = a.settings()
	if s.Cookie != "" {
		t.Fatal("empty cookie did not clear")
	}
	if got, e := p.runtimeFor(owner); e != nil || got != p.root {
		t.Fatal("legacy owner remapped")
	}
	if e := p.root.Store.Get("notes", note.ID, &domain.Note{}); e != nil {
		t.Fatal("legacy note lost")
	}
}

func TestPortalRequestLimitAndDisable(t *testing.T) {
	p := newPortalFixture(t)
	var account accountRecord
	p.identity.Get("accounts", "user", &account)
	account.MaxConcurrent = 1
	account.DailyRequestLimit = 2
	p.identity.Put("accounts", account.Username, account)
	release, e := p.acquireRequest(context.Background(), account.ID)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, e = p.acquireRequest(ctx, account.ID); e == nil {
		t.Fatal("concurrency limit bypassed")
	}
	count, _ := p.dailyRequests(account.ID)
	if count != 1 {
		t.Fatal("waiting request was charged", count)
	}
	release()
	release, e = p.acquireRequest(context.Background(), account.ID)
	if e != nil {
		t.Fatal(e)
	}
	release()
	if _, e = p.acquireRequest(context.Background(), account.ID); e == nil {
		t.Fatal("daily quota bypassed")
	}
	account.DailyRequestLimit = 0
	account.Enabled = false
	account.Version++
	p.identity.Put("accounts", account.Username, account)
	if _, e = p.acquireRequest(context.Background(), account.ID); e == nil {
		t.Fatal("disabled account spent money")
	}
	if p.root.canRun() {
		t.Fatal("disabled account worker can claim work")
	}
}

func TestPortalBillingAggregatesAndFiltersUsers(t *testing.T) {
	p := newPortalFixture(t)
	var first accountRecord
	p.identity.Get("accounts", "user", &first)
	other := accountRecord{Account: Account{ID: ID(), Username: "writer", Role: "member", Enabled: true, MaxConcurrent: 1}, Version: 1}
	p.identity.Put("accounts", other.Username, other)
	a, e := p.runtimeFor(other)
	if e != nil {
		t.Fatal(e)
	}
	n := int64(300)
	v := domain.UsageRecord{ID: "one", CreatedAt: now(), Capability: "text", Model: "model", Provider: "https://model.example", Status: "succeeded", TotalTokens: &n}
	if e = p.root.Store.SaveUsage(v); e != nil {
		t.Fatal(e)
	}
	v.ID = "two"
	a.Store.SaveUsage(v)
	admin := newPortalBrowser(t, p, "admin")
	password := portalPassword(t, p, "admin")
	admin.login(t, "admin", password)
	admin.change(t, password, "Admin-password-2026!")
	all := decodeResponse[struct {
		Summary usageTotals          `json:"summary"`
		Items   []domain.UsageRecord `json:"items"`
	}](t, admin.call(t, "GET", "/admin/billing", nil), 200)
	if all.Summary.Requests != 2 || all.Summary.TotalTokens != 600 || len(all.Items) != 2 || all.Items[0].Username == "" {
		t.Fatal("missing aggregate user usage", all)
	}
	filtered := decodeResponse[struct {
		Summary usageTotals          `json:"summary"`
		Items   []domain.UsageRecord `json:"items"`
	}](t, admin.call(t, "GET", "/admin/billing?user_id="+first.ID, nil), 200)
	if filtered.Summary.Requests != 1 || filtered.Items[0].UserID != first.ID {
		t.Fatal("invalid user filter")
	}
	if r := admin.call(t, "GET", "/admin/billing?user_id=unknown", nil); r.Code != 400 {
		t.Fatal("invalid account silently aggregated")
	}
}

func TestPortalBootstrapDoesNotResetExistingPasswords(t *testing.T) {
	f := newAPIFixture(t)
	p, e := NewPortal(f.app)
	if e != nil {
		t.Fatal(e)
	}
	var original accountRecord
	p.identity.Get("accounts", "user", &original)
	file := filepath.Join(f.app.Data, "initial-accounts.txt")
	if _, e = os.Stat(file); e != nil {
		t.Fatal(e)
	}
	if e = p.identity.Close(); e != nil {
		t.Fatal(e)
	}
	second, e := NewPortal(f.app)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { second.Close() })
	var saved accountRecord
	second.identity.Get("accounts", "user", &saved)
	if original.ID != saved.ID || original.PasswordHash != saved.PasswordHash {
		t.Fatal("restart reset credentials")
	}
}

func TestPortalSessionExpiryDisableAndLoginThrottle(t *testing.T) {
	p := newPortalFixture(t)
	b := newPortalBrowser(t, p, "user")
	password := portalPassword(t, p, "user")
	b.login(t, "user", password)
	b.change(t, password, "Member-password-2026!")
	var session portalSession
	key := sessionHash(b.cookie.Value)
	p.identity.Get("sessions", key, &session)
	session.LastSeen = time.Now().Add(-31 * time.Minute).Unix()
	p.identity.Put("sessions", key, session)
	if r := b.call(t, "GET", "/auth/me", nil); r.Code != 401 {
		t.Fatal("idle session accepted")
	}
	b = newPortalBrowser(t, p, "user")
	b.login(t, "user", "Member-password-2026!")
	var account accountRecord
	p.identity.Get("accounts", "user", &account)
	account.Enabled = false
	account.Version++
	p.identity.Put("accounts", account.Username, account)
	if r := b.call(t, "GET", "/collections", nil); r.Code != 401 {
		t.Fatal("disabled session accepted")
	}
	guest := newPortalBrowser(t, p, "user")
	for i := 0; i < 5; i++ {
		if r := guest.call(t, "POST", "/auth/login", map[string]string{"username": "nonexistent", "password": "wrong"}); r.Code != 401 {
			t.Fatal(r.Code)
		}
	}
	if r := guest.call(t, "POST", "/auth/login", map[string]string{"username": "nonexistent", "password": "wrong"}); r.Code != 429 {
		t.Fatal("login not throttled", r.Code)
	}
	req := httptest.NewRequest("GET", "http://evil.example/api/v1/session", nil)
	w := httptest.NewRecorder()
	p.Router().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("host boundary bypassed")
	}
	req = httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/session", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	p.Router().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("origin boundary bypassed")
	}
}

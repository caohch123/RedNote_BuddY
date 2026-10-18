package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"xhs-workbench/internal/domain"
)

type portalAdminFixture struct {
	p      *Portal
	router *gin.Engine
	admin  accountRecord
	member accountRecord
}

func newPortalAdminFixture(t *testing.T) portalAdminFixture {
	t.Helper()
	base := newAPIFixture(t)
	p, err := NewPortal(base.app)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	accounts, err := p.accounts()
	if err != nil {
		t.Fatal(err)
	}
	f := portalAdminFixture{p: p}
	for _, a := range accounts {
		if a.Role == "admin" {
			f.admin = a
		} else {
			f.member = a
		}
	}
	f.router = gin.New()
	g := f.router.Group("/admin")
	g.Use(func(c *gin.Context) { c.Set(portalActorKey, f.admin.Account); c.Next() })
	p.routesAdmin(g)
	return f
}

func (f portalAdminFixture) call(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:8080/admin"+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	reply := httptest.NewRecorder()
	f.router.ServeHTTP(reply, req)
	return reply
}

func (f portalAdminFixture) createMember(t *testing.T, name string) accountRecord {
	t.Helper()
	decodeResponse[any](t, f.call(t, "POST", "/users", map[string]any{"username": name, "display_name": "测试成员", "daily_request_limit": 10, "max_concurrent": 1}), 201)
	var account accountRecord
	if err := f.p.identity.Get("accounts", name, &account); err != nil {
		t.Fatal(err)
	}
	return account
}

func TestPortalAdminAccountsProtectSecretsAndRevokeSessions(t *testing.T) {
	f := newPortalAdminFixture(t)
	result := decodeResponse[struct {
		User     Account `json:"user"`
		Password string  `json:"temporary_password"`
	}](t, f.call(t, "POST", "/users", map[string]any{"username": "writer", "daily_request_limit": 20, "max_concurrent": 2}), 201)
	if result.User.Role != "member" || !result.User.MustChangePassword || !validPassword(result.Password) {
		t.Fatalf("invalid new account %+v", result.User)
	}
	var original accountRecord
	if err := f.p.identity.Get("accounts", "writer", &original); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(original.PasswordHash), []byte(result.Password)) != nil {
		t.Fatal("temporary password does not match saved hash")
	}
	users := f.call(t, "GET", "/users", nil)
	if users.Code != 200 || strings.Contains(users.Body.String(), "password_hash") || strings.Contains(users.Body.String(), result.Password) {
		t.Fatalf("account secret exposed: %s", users.Body.String())
	}
	decodeResponse[any](t, f.call(t, "POST", "/users", map[string]any{"username": "writer"}), 409)
	for _, invalid := range []map[string]any{{"username": "ab"}, {"username": "goodname", "role": "superuser"}, {"username": "goodname", "max_concurrent": 0}, {"username": "goodname", "daily_request_limit": -1}} {
		decodeResponse[any](t, f.call(t, "POST", "/users", invalid), 400)
	}
	decodeResponse[any](t, f.call(t, "PATCH", "/users/"+f.admin.ID, map[string]any{"enabled": false}), 409)
	decodeResponse[any](t, f.call(t, "PATCH", "/users/"+original.ID, map[string]any{"daily_request_limit": 50, "max_concurrent": 3}), 200)
	var updated accountRecord
	if err := f.p.identity.Get("accounts", "writer", &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Version != original.Version+1 || updated.DailyRequestLimit != 50 || updated.MaxConcurrent != 3 {
		t.Fatal("updated quota did not revoke sessions")
	}
	reset := decodeResponse[struct {
		Password string `json:"temporary_password"`
	}](t, f.call(t, "POST", "/users/"+original.ID+"/reset-password", nil), 200)
	var final accountRecord
	if err := f.p.identity.Get("accounts", "writer", &final); err != nil {
		t.Fatal(err)
	}
	if final.Version != updated.Version+1 || !final.MustChangePassword || bcrypt.CompareHashAndPassword([]byte(final.PasswordHash), []byte(reset.Password)) != nil || reset.Password == result.Password {
		t.Fatal("password reset must replace hash, require change, and invalidate sessions")
	}
	audit := decodeResponse[struct {
		Items []AuditEvent `json:"items"`
	}](t, f.call(t, "GET", "/audit", nil), 200)
	if len(audit.Items) < 3 {
		t.Fatal("management writes were not audited")
	}
	for _, item := range audit.Items {
		if item.ActorID != f.admin.ID || strings.Contains(item.Result, reset.Password) {
			t.Fatal("invalid audit event")
		}
	}
}

func TestPortalAdminJobsAreScopedSanitizedAndSafelyCancelled(t *testing.T) {
	f := newPortalAdminFixture(t)
	member := f.createMember(t, "writer")
	a, err := f.p.runtimeFor(member)
	if err != nil {
		t.Fatal(err)
	}
	job := domain.Job{ID: "same-job", Kind: "rewrite", State: "queued", Phase: "queued", CreatedAt: now(), UpdatedAt: now(), Payload: json.RawMessage(`{"secret":"private-user-content"}`), ProviderSnapshot: map[string]domain.ProviderConfig{"text": {APIKey: "old-provider-key"}}, Message: "request old-provider-key", InputScene: &domain.Scene{ID: "private-scene"}}
	if err = a.Store.Put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	other := job
	other.State = "running"
	if err = f.p.root.Store.Put("jobs", other.ID, other); err != nil {
		t.Fatal(err)
	}
	response := f.call(t, "GET", "/jobs?user_id="+member.ID, nil)
	items := decodeResponse[struct {
		Items []adminJob `json:"items"`
		Total int        `json:"total"`
	}](t, response, 200)
	if items.Total != 1 || items.Items[0].UserID != member.ID {
		t.Fatal("user task filter returned another user's task")
	}
	for _, private := range []string{"old-provider-key", "private-user-content", "private-scene", "provider_snapshot", "payload"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("job details exposed %s", private)
		}
	}
	decodeResponse[any](t, f.call(t, "POST", "/users/"+member.ID+"/jobs/"+job.ID+"/cancel", nil), 200)
	var actual domain.Job
	if err = a.Store.Get("jobs", job.ID, &actual); err != nil || actual.State != "cancelled" {
		t.Fatal("selected queued task was not cancelled")
	}
	if err = f.p.root.Store.Get("jobs", other.ID, &actual); err != nil || actual.State != "running" {
		t.Fatal("another user's task was affected")
	}
	if claimed, err := a.Store.Claim("rewrite"); err != nil || claimed != nil {
		t.Fatal("cancelled task remained claimable")
	}
	claimedJob := domain.Job{ID: "claimed-before-registration", Kind: "rewrite", State: "queued", Phase: "queued"}
	if err = a.Store.Put("jobs", claimedJob.ID, claimedJob); err != nil {
		t.Fatal(err)
	}
	if claimed, err := a.Store.Claim("rewrite"); err != nil || claimed == nil || claimed.State != "running" {
		t.Fatal("cannot simulate worker handoff")
	}
	decodeResponse[any](t, f.call(t, "POST", "/users/"+member.ID+"/jobs/"+claimedJob.ID+"/cancel", nil), 409)
	if err = a.Store.Get("jobs", claimedJob.ID, &actual); err != nil || actual.State != "running" {
		t.Fatal("admin cancellation raced a claimed task before run registration")
	}
	decodeResponse[any](t, f.call(t, "POST", "/users/"+f.member.ID+"/jobs/"+other.ID+"/cancel", nil), 409)
	for _, unsafe := range []domain.Job{{ID: "submitted", Kind: "image_rewrite", State: "submission_unknown"}, {ID: "publish", Kind: "publish", State: "failed"}, {ID: "remote", Kind: "video_rewrite", State: "queued", Phase: "polling", RemoteTaskID: "remote-task"}} {
		if err = a.Store.Put("jobs", unsafe.ID, unsafe); err != nil {
			t.Fatal(err)
		}
		path := "/retry"
		if unsafe.ID == "remote" {
			path = "/cancel"
		}
		decodeResponse[any](t, f.call(t, "POST", "/users/"+member.ID+"/jobs/"+unsafe.ID+path, nil), 409)
	}
}

func TestPortalAdminOverviewUsesReportedUsageAndLocalDay(t *testing.T) {
	f := newPortalAdminFixture(t)
	member := f.createMember(t, "writer")
	a, err := f.p.runtimeFor(member)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	stamp := time.Now().In(loc)
	start := time.Date(stamp.Year(), stamp.Month(), stamp.Day(), 0, 0, 0, 0, loc)
	n := int64(100)
	input, output := int64(80), int64(20)
	for _, value := range []struct {
		app    *App
		record domain.UsageRecord
	}{
		{f.p.root, domain.UsageRecord{ID: "today-root", CreatedAt: start.Add(time.Minute).UTC().Format(time.RFC3339), TotalTokens: &n, InputTokens: &input, OutputTokens: &output, Price: &domain.TokenPrice{InputPerMillion: 1, OutputPerMillion: 2}, Status: "success"}},
		{a, domain.UsageRecord{ID: "today-unreported", CreatedAt: start.Add(2 * time.Minute).UTC().Format(time.RFC3339), Status: "failed"}},
		{a, domain.UsageRecord{ID: "yesterday", CreatedAt: start.Add(-time.Minute).UTC().Format(time.RFC3339), TotalTokens: &n, Status: "success"}},
	} {
		if err := value.app.Store.SaveUsage(value.record); err != nil {
			t.Fatal(err)
		}
	}
	result := decodeResponse[struct {
		Users      int              `json:"users"`
		Requests   int              `json:"requests"`
		Tokens     int64            `json:"tokens"`
		Unreported int              `json:"unreported"`
		Estimated  float64          `json:"estimated_cny"`
		PerUser    []adminUsageUser `json:"per_user"`
	}](t, f.call(t, "GET", "/overview", nil), 200)
	if result.Users != 3 || result.Requests != 2 || result.Tokens != 100 || result.Unreported != 1 || result.Estimated != 0.00012 || len(result.PerUser) != 2 {
		t.Fatalf("incorrect local-day aggregation: %+v", result)
	}
	decodeResponse[any](t, f.call(t, "GET", "/overview?timezone=invalid-zone", nil), 400)
}

func TestPortalAdminModelsRetainPrivateCookieAndMaskKeys(t *testing.T) {
	f := newPortalAdminFixture(t)
	settings, err := f.p.root.settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Cookie = "private-member-cookie"
	settings.Providers["text"] = domain.ProviderConfig{BaseURL: "https://example.com/v1", Model: "old-model", APIKey: "old-key"}
	if err = f.p.root.saveSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PATCH"} {
		var body any
		if method == "PATCH" {
			body = map[string]any{"providers": map[string]any{"text": map[string]any{"model": "new-model", "api_key": "new-key"}}}
		}
		reply := f.call(t, method, "/models", body)
		result := decodeResponse[adminModelSettings](t, reply, 200)
		if !result.Providers["text"].Configured || result.Providers["text"].APIKey != "" {
			t.Fatal("provider credential mask/configuration lost")
		}
		for _, secret := range []string{"private-member-cookie", "old-key", "new-key", "cookie_configured", "cookie_mask"} {
			if strings.Contains(reply.Body.String(), secret) {
				t.Fatalf("model response exposed %s", secret)
			}
		}
	}
	decodeResponse[any](t, f.call(t, "PATCH", "/models", map[string]any{"cookie": "__CLEAR__"}), 400)
	final, err := f.p.root.settings()
	if err != nil {
		t.Fatal(err)
	}
	if final.Cookie != "private-member-cookie" || final.Providers["text"].APIKey != "new-key" || final.Providers["text"].Model != "new-model" {
		t.Fatal("platform settings patch changed user cookie or failed to save provider")
	}
	decodeResponse[any](t, f.call(t, "PATCH", "/models", map[string]any{"providers": map[string]any{"unexpected": map[string]any{"model": "mock"}}}), 400)
	decodeResponse[any](t, f.call(t, "PATCH", "/models", map[string]any{"collector_url": "https://external.example", "providers": map[string]any{"text": map[string]any{"api_key": "rejected-key"}}}), 400)
	unchanged, err := f.p.root.settings()
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Cookie != final.Cookie || unchanged.Providers["text"].APIKey != final.Providers["text"].APIKey || unchanged.CollectorURL != final.CollectorURL {
		t.Fatal("rejected platform settings patch partially changed secrets")
	}
}

func TestPortalAdminEnablingMemberStartsPreviouslyDormantRuntime(t *testing.T) {
	f := newPortalAdminFixture(t)
	member := f.createMember(t, "dormant")
	member.Enabled = false
	if err := f.p.identity.Put("accounts", member.Username, member); err != nil {
		t.Fatal(err)
	}
	a, err := f.p.runtimeFor(member)
	if err != nil {
		t.Fatal(err)
	}
	// Suppress account refresh; this test exercises only local worker recovery.
	a.xhs.mu.Lock()
	a.xhs.lastCheck = a.xhs.clock()
	a.xhs.mu.Unlock()
	job := domain.Job{ID: "dormant-job", Kind: "publish", EntityID: "missing-source", State: "queued", Phase: "queued"}
	if err = a.Store.Put("jobs", job.ID, job); err != nil {
		t.Fatal(err)
	}
	f.p.mu.Lock()
	f.p.startWorkers = true
	f.p.mu.Unlock()
	decodeResponse[any](t, f.call(t, "PATCH", "/users/"+member.ID, map[string]any{"enabled": true}), 200)
	var saved domain.Job
	if err = a.Store.Get("jobs", job.ID, &saved); err != nil || saved.State != "cancelled" {
		t.Fatal("enabling a dormant user did not run safe queue recovery")
	}
	unused := false
	a.workerOnce.Do(func() { unused = true })
	if unused {
		t.Fatal("enabling member did not start runtime workers")
	}
}

func TestPortalAdminAuditReturnsNewestFiveHundredWithStableTieBreak(t *testing.T) {
	f := newPortalAdminFixture(t)
	for i := 504; i >= 0; i-- {
		event := AuditEvent{ID: fmt.Sprintf("audit-%03d", i), CreatedAt: "2026-01-02T00:00:00Z", ActorName: "admin", Action: "fixture", Result: "success"}
		if err := f.p.identity.Put("audit", event.ID, event); err != nil {
			t.Fatal(err)
		}
	}
	old := AuditEvent{ID: "last-written-but-old", CreatedAt: "2026-01-01T00:00:00Z", ActorName: "admin", Action: "fixture", Result: "success"}
	if err := f.p.identity.Put("audit", old.ID, old); err != nil {
		t.Fatal(err)
	}
	result := decodeResponse[struct {
		Items []AuditEvent `json:"items"`
		Total int          `json:"total"`
	}](t, f.call(t, "GET", "/audit", nil), 200)
	if result.Total != 500 || len(result.Items) != 500 || result.Items[0].ID != "audit-504" || result.Items[499].ID != "audit-005" {
		t.Fatal("audit history was limited before stable chronological sorting")
	}
	for i := 1; i < len(result.Items); i++ {
		if result.Items[i-1].ID <= result.Items[i].ID {
			t.Fatal("equal-time audit order is unstable")
		}
	}
}

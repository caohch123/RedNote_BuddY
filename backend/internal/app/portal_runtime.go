package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/store"
)

func (p *Portal) configureRuntime(a *App, account accountRecord) {
	a.priceStore = p.root.Store
	a.canRun = func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		latest, err := p.accountByID(account.ID)
		return !p.closed && err == nil && latest.Enabled
	}
	a.usageGuard = func(ctx context.Context, capability string) (func(), error) {
		return p.acquireRequest(ctx, account.ID)
	}
	if a != p.root {
		a.sharedSettings = p.root.settings
		a.frontendOrigins = p.root.frontendOrigins
	}
}

func (p *Portal) runtimeFor(account accountRecord) (*App, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("服务正在关闭")
	}
	latest, err := p.accountByID(account.ID)
	if err != nil || latest.Role != "member" {
		p.mu.Unlock()
		return nil, errors.New("用户工作区不存在")
	}
	a := p.runtimes[account.ID]
	if a == nil {
		if !safeID.MatchString(account.ID) {
			p.mu.Unlock()
			return nil, errors.New("用户标识无效")
		}
		a, err = New(p.root.Root, filepath.Join(p.root.Data, "accounts", account.ID), p.root.Addr)
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		p.configureRuntime(a, latest)
		p.runtimes[account.ID] = a
	}
	start := p.startWorkers && latest.Enabled
	p.mu.Unlock()
	if start {
		a.StartWorkers()
	}
	return a, nil
}

func (p *Portal) StartWorkers() error {
	p.mu.Lock()
	p.startWorkers = true
	p.mu.Unlock()
	accounts, err := p.accounts()
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if account.Role == "member" && account.Enabled {
			if _, err = p.runtimeFor(account); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Portal) stopAccountWork(id string) {
	p.mu.Lock()
	latest, err := p.accountByID(id)
	if err != nil || latest.Enabled {
		p.mu.Unlock()
		return
	}
	a := p.runtimes[id]
	p.mu.Unlock()
	if a == nil {
		return
	}
	a.mu.Lock()
	for _, run := range a.jobRuns {
		run.cancel()
	}
	a.mu.Unlock()
}

func (p *Portal) forward(c *gin.Context, a *App, path string) {
	p.mu.Lock()
	handler := p.handlers[a.Data]
	if handler == nil {
		handler = a.Router()
		p.handlers[a.Data] = handler
	}
	p.mu.Unlock()
	request := c.Request.Clone(c.Request.Context())
	urlCopy := *request.URL
	urlCopy.Path, urlCopy.RawPath = path, ""
	request.URL = &urlCopy
	request.Header = request.Header.Clone()
	request.Header.Set("X-CSRF-Token", a.CSRF)
	handler.ServeHTTP(c.Writer, request)
	c.Abort()
}

func (p *Portal) dispatchUser(c *gin.Context) {
	path := c.Request.URL.Path
	if !strings.HasPrefix(path, "/api/v1/") || strings.HasPrefix(path, "/api/v1/admin/") {
		fail(c, 404, "接口不存在")
		return
	}
	account, ok := p.requireAccount(c, "user", false)
	if !ok {
		return
	}
	if path == "/api/v1/settings" && c.Request.Method != http.MethodGet {
		if c.Request.Method != http.MethodPatch {
			fail(c, 403, "此操作需要管理员权限")
			return
		}
		var input map[string]json.RawMessage
		if !bind(c, &input) {
			return
		}
		if len(input) != 1 || input["cookie"] == nil {
			fail(c, 403, "模型与运行工具由管理员配置")
			return
		}
		var cookie string
		if string(input["cookie"]) == "null" || json.Unmarshal(input["cookie"], &cookie) != nil {
			fail(c, 400, "Cookie 格式无效")
			return
		}
		if cookie == "" {
			cookie = "__CLEAR__"
		}
		body, _ := json.Marshal(map[string]string{"cookie": cookie})
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
	} else if strings.HasPrefix(path, "/api/v1/settings/") || path == "/api/v1/billing/prices" {
		fail(c, 403, "此操作需要管理员权限")
		return
	}
	a, err := p.runtimeFor(account)
	if err != nil {
		fail(c, 500, "用户工作区不可用")
		return
	}
	p.forward(c, a, path)
}

func quotaDay(userID string) string {
	return userID + "-" + time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
}

func (p *Portal) dailyRequests(userID string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var count quotaCounter
	err := p.identity.Get("daily_requests", quotaDay(userID), &count)
	if errors.Is(err, store.ErrNotFound) {
		err = nil
	}
	return count.Requests, err
}

func (p *Portal) acquireRequest(ctx context.Context, userID string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		account, err := p.accountByID(userID)
		if p.closed || err != nil || !account.Enabled {
			p.mu.Unlock()
			return nil, errors.New("账号已停用，任务已停止")
		}
		var counter quotaCounter
		key := quotaDay(userID)
		err = p.identity.Get("daily_requests", key, &counter)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			p.mu.Unlock()
			return nil, errors.New("无法核对调用额度")
		}
		if account.DailyRequestLimit > 0 && counter.Requests >= account.DailyRequestLimit {
			p.mu.Unlock()
			return nil, errors.New("已达到今日模型请求额度，请联系管理员或明日重试")
		}
		limit := account.MaxConcurrent
		if limit < 1 {
			limit = 1
		}
		if p.inflight[userID] < limit {
			counter.Requests++
			if p.identity.Put("daily_requests", key, counter) != nil {
				p.mu.Unlock()
				return nil, errors.New("无法保存调用额度")
			}
			p.inflight[userID]++
			p.mu.Unlock()
			return func() { p.mu.Lock(); p.inflight[userID]--; p.mu.Unlock() }, nil
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

var safeID = regexp.MustCompile(`^[a-f0-9]{32}$`)

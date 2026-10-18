package app

import (
	"encoding/csv"
	"errors"
	"log"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

type billingMeta struct {
	StartedAt string `json:"started_at"`
}
type billingPrices struct {
	Items []domain.TokenPrice `json:"items"`
}
type usageTotals struct {
	Requests          int     `json:"requests"`
	Reported          int     `json:"reported"`
	Missing           int     `json:"missing"`
	Failed            int     `json:"failed"`
	Pending           int     `json:"pending"`
	Unknown           int     `json:"unknown"`
	InputTokens       int64   `json:"input_tokens"`
	InputReported     int     `json:"input_reported"`
	OutputTokens      int64   `json:"output_tokens"`
	OutputReported    int     `json:"output_reported"`
	TotalTokens       int64   `json:"total_tokens"`
	CachedTokens      int64   `json:"cached_tokens"`
	CachedReported    int     `json:"cached_reported"`
	ReasoningTokens   int64   `json:"reasoning_tokens"`
	ReasoningReported int     `json:"reasoning_reported"`
	EstimatedCNY      float64 `json:"estimated_cny"`
	Priced            int     `json:"priced"`
	Images            int64   `json:"images"`
	VideoSeconds      float64 `json:"video_seconds"`
}

type usageGroup struct {
	Key string `json:"key"`
	usageTotals
}
type billingModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (a *App) initBilling() error {
	var meta billingMeta
	if err := a.Store.Get("billing_meta", "main", &meta); errors.Is(err, store.ErrNotFound) {
		return a.Store.Put("billing_meta", "main", billingMeta{StartedAt: now()})
	} else {
		return err
	}
}

func (a *App) meteredClient(s domain.Settings, j *domain.Job) *providers.Client {
	return providers.New(s).WithUsageGuard(a.usageGuard).WithUsage(func(v domain.UsageRecord) {
		v.JobID, v.EntityID, v.Operation = j.ID, j.EntityID, j.Kind
		var prices billingPrices
		if err := a.billingPriceStore().Get("billing_prices", "main", &prices); err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Print("billing: cannot read price configuration")
		}
		for _, price := range prices.Items {
			if price.Provider == v.Provider && price.Model == v.Model {
				v.Price = &price
				break
			}
		}
		if err := a.Store.SaveUsage(v); err != nil {
			log.Print("billing: cannot persist usage record")
		}
	})
}

func (a *App) billingPriceStore() *store.Store {
	if a.priceStore != nil {
		return a.priceStore
	}
	return a.Store
}
func (a *App) readUsage(f domain.UsageFilter) ([]domain.UsageRecord, error) {
	if a.usageReader != nil {
		return a.usageReader(f)
	}
	return a.Store.Usage(f)
}

func addUsage(t *usageTotals, v domain.UsageRecord) {
	t.Requests++
	if v.TotalTokens != nil {
		t.Reported++
		t.TotalTokens += *v.TotalTokens
	} else {
		t.Missing++
	}
	if v.InputTokens != nil {
		t.InputReported++
		t.InputTokens += *v.InputTokens
	}
	if v.OutputTokens != nil {
		t.OutputReported++
		t.OutputTokens += *v.OutputTokens
	}
	if v.CachedTokens != nil {
		t.CachedReported++
		t.CachedTokens += *v.CachedTokens
	}
	if v.ReasoningTokens != nil {
		t.ReasoningReported++
		t.ReasoningTokens += *v.ReasoningTokens
	}
	if v.ImageCount != nil {
		t.Images += *v.ImageCount
	}
	if v.VideoSeconds != nil {
		t.VideoSeconds += *v.VideoSeconds
	}
	if v.EstimatedCNY != nil {
		t.Priced++
		t.EstimatedCNY += *v.EstimatedCNY
	}
	switch v.Status {
	case "failed":
		t.Failed++
	case "pending":
		t.Pending++
	case "unknown":
		t.Unknown++
	}
}

func sortedUsageGroups(groups map[string]*usageGroup) []usageGroup {
	items := []usageGroup{}
	for _, v := range groups {
		items = append(items, *v)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalTokens == items[j].TotalTokens {
			return items[i].Key < items[j].Key
		}
		return items[i].TotalTokens > items[j].TotalTokens
	})
	return items
}

func billingFilter(c *gin.Context) (domain.UsageFilter, *time.Location, bool) {
	f := domain.UsageFilter{Model: c.Query("model"), Capability: c.Query("capability")}
	for _, field := range []struct {
		name   string
		target *int64
	}{{"from", &f.From}, {"to", &f.To}} {
		if text := c.Query(field.name); text != "" {
			v, err := time.Parse(time.RFC3339Nano, text)
			if err != nil || v.Year() < 2000 || v.Year() > 2200 {
				fail(c, 400, "统计时间格式无效")
				return f, nil, false
			}
			*field.target = v.UnixMilli()
		}
	}
	if f.To != 0 && f.From != 0 && f.To <= f.From {
		fail(c, 400, "结束时间须晚于开始时间")
		return f, nil, false
	}
	if len(f.Model) > 200 {
		fail(c, 400, "模型名称过长")
		return f, nil, false
	}
	if f.Capability != "" && !has([]string{"text", "vision", "ocr", "asr", "image", "video"}, f.Capability) {
		fail(c, 400, "调用类型无效")
		return f, nil, false
	}
	tz := c.DefaultQuery("timezone", "UTC")
	loc, err := time.LoadLocation(tz)
	if err != nil || len(tz) > 100 {
		fail(c, 400, "时区无效")
		return f, nil, false
	}
	return f, loc, true
}

func (a *App) routesBilling(g *gin.RouterGroup) {
	g.GET("/billing", a.billingSummary)
	g.GET("/billing/export", a.exportBilling)
	g.PUT("/billing/prices", func(c *gin.Context) {
		var input struct {
			Items []struct {
				Provider string   `json:"provider"`
				Model    string   `json:"model"`
				Input    *float64 `json:"input_per_million"`
				Output   *float64 `json:"output_per_million"`
				Cached   *float64 `json:"cached_per_million"`
			} `json:"items"`
		}
		if c.ShouldBindJSON(&input) != nil || input.Items == nil || len(input.Items) > 100 {
			fail(c, 400, "单价配置格式无效")
			return
		}
		body := billingPrices{Items: []domain.TokenPrice{}}
		for _, item := range input.Items {
			if item.Input == nil || item.Output == nil {
				fail(c, 400, "请填写输入与输出单价，免费单价可显式填写 0")
				return
			}
			body.Items = append(body.Items, domain.TokenPrice{Provider: item.Provider, Model: item.Model, InputPerMillion: *item.Input, OutputPerMillion: *item.Output, CachedPerMillion: item.Cached})
		}
		seen := map[string]bool{}
		for _, p := range body.Items {
			u, err := url.Parse(p.Provider)
			validRate := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1e9 }
			key := p.Provider + "\x00" + p.Model
			if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(p.Model) == "" || len(p.Model) > 200 || seen[key] || !validRate(p.InputPerMillion) || !validRate(p.OutputPerMillion) || (p.CachedPerMillion != nil && !validRate(*p.CachedPerMillion)) {
				fail(c, 400, "单价须为有效的非负数，模型与服务来源不可重复")
				return
			}
			seen[key] = true
		}
		if body.Items == nil {
			body.Items = []domain.TokenPrice{}
		}
		if a.put(c, "billing_prices", "main", body) {
			c.JSON(200, body)
		}
	})
}

func (a *App) billingSummary(c *gin.Context) {
	f, loc, ok := billingFilter(c)
	if !ok {
		return
	}
	page := 1
	if value := c.Query("page"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 1_000_000 {
			fail(c, 400, "页码无效")
			return
		}
		page = n
	}
	items, err := a.readUsage(f)
	if err != nil {
		fail(c, 500, "读取用量记录失败")
		return
	}
	var meta billingMeta
	var prices billingPrices
	if a.Store.Get("billing_meta", "main", &meta) != nil {
		fail(c, 500, "读取统计起始时间失败")
		return
	}
	if e := a.billingPriceStore().Get("billing_prices", "main", &prices); e != nil && !errors.Is(e, store.ErrNotFound) {
		fail(c, 500, "读取单价失败")
		return
	}
	if prices.Items == nil {
		prices.Items = []domain.TokenPrice{}
	}
	summary := usageTotals{}
	models, capabilities, timeline := map[string]*usageGroup{}, map[string]*usageGroup{}, map[string]*usageGroup{}
	users := map[string]*usageGroup{}
	options := map[string]billingModel{}
	addOption := func(provider, model string) {
		if model != "" {
			options[provider+"\x00"+model] = billingModel{Provider: provider, Model: model}
		}
	}
	if s, e := a.settings(); e == nil {
		for _, p := range s.Providers {
			if u, e := url.Parse(p.BaseURL); e == nil && u.Host != "" {
				addOption(u.Scheme+"://"+u.Host, p.Model)
			}
		}
	}
	hourly := f.From != 0 && f.To != 0 && f.To-f.From <= 48*int64(time.Hour/time.Millisecond)
	for _, v := range items {
		addUsage(&summary, v)
		if v.Username != "" {
			if users[v.Username] == nil {
				users[v.Username] = &usageGroup{Key: v.Username}
			}
			addUsage(&users[v.Username].usageTotals, v)
		}
		stamp, _ := time.Parse(time.RFC3339Nano, v.CreatedAt)
		key := stamp.In(loc).Format("2006-01-02")
		if hourly {
			key = stamp.In(loc).Format("2006-01-02T15:00:00Z07:00")
		}
		for _, group := range []struct {
			values map[string]*usageGroup
			key    string
		}{{models, v.Model}, {capabilities, v.Capability}, {timeline, key}} {
			if group.values[group.key] == nil {
				group.values[group.key] = &usageGroup{Key: group.key}
			}
			addUsage(&group.values[group.key].usageTotals, v)
		}
		addOption(v.Provider, v.Model)
	}
	for _, p := range prices.Items {
		addOption(p.Provider, p.Model)
	}
	modelOptions := []billingModel{}
	for _, o := range options {
		modelOptions = append(modelOptions, o)
	}
	sort.Slice(modelOptions, func(i, j int) bool {
		return modelOptions[i].Model+modelOptions[i].Provider < modelOptions[j].Model+modelOptions[j].Provider
	})
	series := sortedUsageGroups(timeline)
	sort.Slice(series, func(i, j int) bool { return series[i].Key < series[j].Key })
	start := (page - 1) * 20
	end := start + 20
	if start > len(items) {
		start = len(items)
	}
	if end > len(items) {
		end = len(items)
	}
	optionsUsers := []gin.H{}
	for _, account := range a.billingAccounts {
		optionsUsers = append(optionsUsers, gin.H{"id": account.ID, "username": account.Username, "display_name": account.DisplayName})
	}
	c.JSON(200, gin.H{"summary": summary, "timeline": series, "models": sortedUsageGroups(models), "capabilities": sortedUsageGroups(capabilities), "items": items[start:end], "total": len(items), "page": page, "page_size": 20, "started_at": meta.StartedAt, "model_options": modelOptions, "prices": prices.Items, "hourly": hourly, "user_options": optionsUsers, "users": sortedUsageGroups(users)})
}

func (a *App) exportBilling(c *gin.Context) {
	f, _, ok := billingFilter(c)
	if !ok {
		return
	}
	items, err := a.readUsage(f)
	if err != nil {
		fail(c, 500, "导出用量失败")
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="token-usage.csv"`)
	c.Status(200)
	_, _ = c.Writer.Write([]byte{0xef, 0xbb, 0xbf})
	w := csv.NewWriter(c.Writer)
	header := []string{"时间", "功能", "任务类型", "模型", "服务来源", "凭据标识", "请求状态", "输入Token", "输出Token", "总Token", "缓存Token（输入子集）", "推理Token（输出子集）", "图片张数", "视频秒数", "估算费用CNY", "任务ID"}
	if a.billingAccounts != nil {
		header = append(header, "用户账号")
	}
	_ = w.Write(header)
	integer := func(n *int64) string {
		if n == nil {
			return ""
		}
		return strconv.FormatInt(*n, 10)
	}
	decimal := func(n *float64) string {
		if n == nil {
			return ""
		}
		return strconv.FormatFloat(*n, 'f', -1, 64)
	}
	cell := func(s string) string {
		if strings.HasPrefix(strings.TrimSpace(s), "=") || strings.HasPrefix(strings.TrimSpace(s), "+") || strings.HasPrefix(strings.TrimSpace(s), "-") || strings.HasPrefix(strings.TrimSpace(s), "@") || strings.ContainsAny(s, "\r\n\t") {
			return "'" + s
		}
		return s
	}
	for _, v := range items {
		row := []string{v.CreatedAt, v.Capability, v.Operation, cell(v.Model), cell(v.Provider), v.CredentialID, v.Status, integer(v.InputTokens), integer(v.OutputTokens), integer(v.TotalTokens), integer(v.CachedTokens), integer(v.ReasoningTokens), integer(v.ImageCount), decimal(v.VideoSeconds), decimal(v.EstimatedCNY), cell(v.JobID)}
		if a.billingAccounts != nil {
			row = append(row, cell(v.Username))
		}
		_ = w.Write(row)
	}
	w.Flush()
	if w.Error() != nil {
		log.Print("billing: CSV write failed")
	}
}

package app

import (
	"github.com/gin-gonic/gin"
	"sort"
	"xhs-workbench/internal/domain"
)

func (p *Portal) adminBillingView(c *gin.Context) (*App, bool) {
	accounts, err := p.accounts()
	if err != nil {
		fail(c, 500, "读取账号失败")
		return nil, false
	}
	selected := c.Query("user_id")
	view := &App{Store: p.root.Store, sharedSettings: p.root.settings, billingAccounts: []Account{}}
	runtimes := map[string]*App{}
	valid := selected == ""
	for _, account := range accounts {
		if account.Role != "member" {
			continue
		}
		view.billingAccounts = append(view.billingAccounts, account.Account)
		if selected != "" && selected != account.ID {
			continue
		}
		valid = true
		a, e := p.runtimeFor(account)
		if e != nil {
			fail(c, 500, "读取用户用量失败")
			return nil, false
		}
		runtimes[account.ID] = a
	}
	if !valid {
		fail(c, 400, "用户筛选无效")
		return nil, false
	}
	sort.Slice(view.billingAccounts, func(i, j int) bool { return view.billingAccounts[i].Username < view.billingAccounts[j].Username })
	view.usageReader = func(f domain.UsageFilter) ([]domain.UsageRecord, error) {
		items := []domain.UsageRecord{}
		for _, account := range view.billingAccounts {
			a := runtimes[account.ID]
			if a == nil {
				continue
			}
			rows, e := a.Store.Usage(f)
			if e != nil {
				return nil, e
			}
			for _, v := range rows {
				v.UserID = account.ID
				v.Username = account.Username
				items = append(items, v)
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt == items[j].CreatedAt {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt > items[j].CreatedAt
		})
		return items, nil
	}
	return view, true
}

func (p *Portal) routesAdminBilling(g *gin.RouterGroup) {
	g.GET("/billing", func(c *gin.Context) {
		if view, ok := p.adminBillingView(c); ok {
			view.billingSummary(c)
		}
	})
	g.GET("/billing/export", func(c *gin.Context) {
		if view, ok := p.adminBillingView(c); ok {
			view.exportBilling(c)
		}
	})
	g.PUT("/billing/prices", func(c *gin.Context) {
		p.forward(c, p.root, "/api/v1/billing/prices")
		result := "rejected"
		if c.Writer.Status() < 400 {
			result = "success"
		}
		p.audit(portalActor(c), "billing.prices", "platform", result)
	})
}

package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestBillingRealProviderUsagePersistsAcrossJobHistoryDeletion(t *testing.T) {
	f := newAPIFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"新标题\",\"body\":\"正文\"}"}}],"usage":{"prompt_tokens":1000000,"completion_tokens":200000,"total_tokens":1200000,"prompt_tokens_details":{"cached_tokens":400000},"completion_tokens_details":{"reasoning_tokens":100000}},"secret":"private-key"}`))
	}))
	defer server.Close()
	price := domain.TokenPrice{Provider: server.URL, Model: "fixture", InputPerMillion: 2, OutputPerMillion: 8}
	decodeResponse[billingPrices](t, f.call(t, "PUT", "/billing/prices", billingPrices{Items: []domain.TokenPrice{price}}, true), 200)
	s := domain.Settings{Providers: map[string]domain.ProviderConfig{"text": {BaseURL: server.URL, APIKey: "private-key", Model: "fixture"}}}
	j := domain.Job{ID: "metered-job", EntityID: "project", Kind: "rewrite"}
	if _, err := f.app.meteredClient(s, &j).Rewrite(context.Background(), domain.Note{}, nil, domain.RewriteOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = f.app.Store.Delete("jobs", j.ID)
	response := f.call(t, "GET", "/billing?timezone=Asia%2FShanghai", nil, false)
	var result struct {
		Summary   usageTotals          `json:"summary"`
		Items     []domain.UsageRecord `json:"items"`
		StartedAt string               `json:"started_at"`
	}
	result = decodeResponse[struct {
		Summary   usageTotals          `json:"summary"`
		Items     []domain.UsageRecord `json:"items"`
		StartedAt string               `json:"started_at"`
	}](t, response, 200)
	if result.Summary.Requests != 1 || result.Summary.TotalTokens != 1200000 || result.Summary.EstimatedCNY != 3.6 || result.StartedAt == "" || result.Items[0].JobID != j.ID {
		t.Fatalf("incorrect ledger: %+v", result)
	}
	if strings.Contains(response.Body.String(), "private-key") || strings.Contains(response.Body.String(), "正文") {
		t.Fatal("billing leaked credentials or model content")
	}
	if f.call(t, "PUT", "/billing/prices", billingPrices{}, false).Code != 403 {
		t.Fatal("price changes require CSRF")
	}
}

func TestBillingFiltersPaginationAndCSVUnknownValues(t *testing.T) {
	f := newAPIFixture(t)
	input, output, total := int64(70), int64(30), int64(100)
	for i := 0; i < 23; i++ {
		id := ID()
		v := domain.UsageRecord{ID: id, CreatedAt: "2026-10-17T16:30:00Z", Capability: "text", Operation: "rewrite", Model: "=UNTRUSTED()", Provider: "https://provider.example", Status: "success", InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
		if i == 22 {
			v.InputTokens = nil
			v.OutputTokens = nil
			v.TotalTokens = nil
			v.Status = "failed"
		}
		if err := f.app.Store.SaveUsage(v); err != nil {
			t.Fatal(err)
		}
	}
	query := "?from=2026-10-18T00:00:00%2B08:00&to=2026-10-19T00:00:00%2B08:00&timezone=Asia%2FShanghai"
	response := f.call(t, "GET", "/billing"+query+"&page=2", nil, false)
	var result struct {
		Summary  usageTotals          `json:"summary"`
		Items    []domain.UsageRecord `json:"items"`
		Timeline []usageGroup         `json:"timeline"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if len(result.Items) != 3 || result.Summary.Requests != 23 || result.Summary.TotalTokens != 2200 || result.Summary.Missing != 1 || result.Timeline[0].Key != "2026-10-18T00:00:00+08:00" {
		t.Fatalf("filter or aggregation error: %+v", result)
	}
	csvResponse := f.call(t, "GET", "/billing/export"+query, nil, false)
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvResponse.Body.String(), "\ufeff"))).ReadAll()
	if err != nil || len(rows) != 24 {
		t.Fatalf("CSV export: %v, %d", err, len(rows))
	}
	missing := 0
	for _, row := range rows[1:] {
		if row[3] != "'=UNTRUSTED()" {
			t.Fatal("spreadsheet formula not escaped")
		}
		if row[9] == "" {
			missing++
		}
	}
	if missing != 1 {
		t.Fatal("unknown usage must be blank in CSV, not zero")
	}
	for _, path := range []string{"/billing?from=invalid", "/billing?page=-1", "/billing?capability=invalid", "/billing?timezone=invalid", "/billing?from=2026-10-19T00:00:00Z&to=2026-10-18T00:00:00Z"} {
		if f.call(t, "GET", path, nil, false).Code != 400 {
			t.Fatalf("invalid filter accepted: %s", path)
		}
	}
	for _, items := range [][]domain.TokenPrice{{{Provider: "https://provider.example?key=private", Model: "x"}}, {{Provider: "https://provider.example", Model: "x", InputPerMillion: -1}}} {
		if f.call(t, "PUT", "/billing/prices", billingPrices{Items: items}, true).Code != 400 {
			t.Fatal("invalid price accepted")
		}
	}
}

func TestBillingWorkerRecordsActualRewriteAndRequiresExplicitRates(t *testing.T) {
	f := newAPIFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"新标题\",\"body\":\"新正文\"}"}}],"usage":{"prompt_tokens":25,"completion_tokens":10,"total_tokens":35}}`))
	}))
	defer server.Close()
	s, err := f.app.settings()
	if err != nil {
		t.Fatal(err)
	}
	s.Providers["text"] = domain.ProviderConfig{BaseURL: server.URL, Model: "worker-model", APIKey: "test-key"}
	if err = f.app.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	note, _, _ := seedSource(t, f)
	r := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites", map[string]string{"note_id": note.ID}, true), 201)
	decodeResponse[map[string]string](t, f.call(t, "POST", "/rewrites/"+r.ID+"/generate", domain.RewriteOptions{IncludeBody: true}, true), 202)
	j, err := f.app.Store.Claim("rewrite")
	if err != nil || j == nil {
		t.Fatalf("claim: %v", err)
	}
	f.app.runJob(j)
	records, err := f.app.Store.Usage(domain.UsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].JobID != j.ID || records[0].TotalTokens == nil || *records[0].TotalTokens != 35 || records[0].EstimatedCNY != nil {
		t.Fatalf("worker usage missing or invented pricing: %+v", records)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"items": []any{map[string]any{"provider": server.URL, "model": "worker-model"}}}, map[string]any{"items": []any{map[string]any{"provider": server.URL, "model": "worker-model", "input_per_million": nil, "output_per_million": 0}}}} {
		if f.call(t, "PUT", "/billing/prices", body, true).Code != 400 {
			t.Fatal("missing price silently became a free price")
		}
	}
	if f.call(t, "PUT", "/billing/prices", billingPrices{Items: []domain.TokenPrice{{Provider: server.URL, Model: "worker-model", InputPerMillion: 0, OutputPerMillion: 0}}}, true).Code != 200 {
		t.Fatal("explicit free rate rejected")
	}
}

package store

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"xhs-workbench/internal/domain"
)

func billingInt(n int64) *int64       { return &n }
func billingFloat(n float64) *float64 { return &n }

func TestBillingEstimateUsesSubsetsWithoutDoubleCounting(t *testing.T) {
	v := domain.UsageRecord{InputTokens: billingInt(1_000_000), OutputTokens: billingInt(200_000), CachedTokens: billingInt(400_000), ReasoningTokens: billingInt(100_000), Price: &domain.TokenPrice{InputPerMillion: 2, OutputPerMillion: 8, CachedPerMillion: billingFloat(.5)}}
	if cost := EstimateTokenCost(v); cost == nil || math.Abs(*cost-3) > 1e-10 {
		t.Fatalf("cost=%v, want 3", cost)
	}
	v.CachedTokens = nil
	if EstimateTokenCost(v) != nil {
		t.Fatal("unknown cache usage cannot be charged at a configured cache rate")
	}
	v.Price.CachedPerMillion = nil
	if cost := EstimateTokenCost(v); cost == nil || math.Abs(*cost-3.6) > 1e-10 {
		t.Fatal("normal-input estimate is incorrect")
	}
	v.OutputTokens = nil
	if EstimateTokenCost(v) != nil {
		t.Fatal("partial usage must not be priced as a free response")
	}
}

func TestBillingPollUpdatesAreDurableAndPreservePriceSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	initial := domain.UsageRecord{ID: "video-call", CreatedAt: "2026-10-17T16:30:00Z", Capability: "video", Model: "model", Status: "pending", Price: &domain.TokenPrice{InputPerMillion: 2, OutputPerMillion: 8}}
	if err = s.SaveUsage(initial); err != nil {
		t.Fatal(err)
	}
	updated := initial
	updated.CreatedAt = "2026-10-18T01:00:00Z"
	updated.Status = "success"
	updated.VideoSeconds = billingFloat(5)
	updated.InputTokens = billingInt(1_000_000)
	updated.OutputTokens = billingInt(100_000)
	updated.TotalTokens = billingInt(1_100_000)
	updated.Price = &domain.TokenPrice{InputPerMillion: 99, OutputPerMillion: 99}
	for i := 0; i < 3; i++ {
		if err = s.SaveUsage(updated); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err := s.Usage(domain.UsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CreatedAt != initial.CreatedAt || items[0].Price.InputPerMillion != 2 || math.Abs(*items[0].EstimatedCNY-2.8) > 1e-10 {
		t.Fatalf("duplicate poll or modified price snapshot: %+v", items)
	}
	from, _ := time.Parse(time.RFC3339, "2026-10-17T16:00:00Z")
	to, _ := time.Parse(time.RFC3339, initial.CreatedAt)
	items, err = s.Usage(domain.UsageFilter{From: from.UnixMilli(), To: to.UnixMilli()})
	if err != nil || len(items) != 0 {
		t.Fatal("end boundary must be exclusive")
	}
	items, err = s.Usage(domain.UsageFilter{Model: "model' OR 1=1 --"})
	if err != nil || len(items) != 0 {
		t.Fatal("model filter was not parameterized")
	}
}

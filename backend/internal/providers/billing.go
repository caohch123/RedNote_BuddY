package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"xhs-workbench/internal/domain"
)

type usageContextKey struct{}
type usageContext struct {
	capability, taskID string
	poll               bool
}

func usageCtx(ctx context.Context, capability string) context.Context {
	return context.WithValue(ctx, usageContextKey{}, usageContext{capability: capability})
}

func (c *Client) WithUsage(observer func(domain.UsageRecord)) *Client {
	c.usage = observer
	return c
}

func (c *Client) WithUsageGuard(guard func(context.Context, string) (func(), error)) *Client {
	c.usageGuard = guard
	return c
}
func (c *Client) acquireUsage(ctx context.Context, capability string) (func(), error) {
	if c.usageGuard != nil {
		return c.usageGuard(ctx, capability)
	}
	return func() {}, nil
}

func number(v json.RawMessage) *int64 {
	var n *int64
	if len(v) == 0 || json.Unmarshal(v, &n) != nil || n == nil || *n < 0 || *n > 1_000_000_000_000 {
		return nil
	}
	return n
}

func usageNumber(m map[string]json.RawMessage, names ...string) *int64 {
	for _, key := range names {
		if n := number(m[key]); n != nil {
			return n
		}
	}
	return nil
}

func (c *Client) reportUsage(p domain.ProviderConfig, meta usageContext, status int, raw []byte) {
	if c.usage == nil || meta.capability == "" {
		return
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" {
		return
	}
	provider := u.Scheme + "://" + u.Host
	hash := sha256.Sum256([]byte(provider + "\x00" + p.WorkspaceID + "\x00" + p.APIKey))
	v := domain.UsageRecord{ID: uuid(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Capability: meta.capability, Provider: provider, Model: p.Model, CredentialID: hex.EncodeToString(hash[:])[:12], Status: "unknown"}
	var response struct {
		Usage  map[string]json.RawMessage `json:"usage"`
		Code   string                     `json:"code"`
		Error  json.RawMessage            `json:"error"`
		Output struct {
			ID     string `json:"task_id"`
			Status string `json:"task_status"`
		} `json:"output"`
	}
	parsed := json.Unmarshal(raw, &response) == nil
	if status >= 200 && status < 300 && parsed {
		v.Status = "success"
	}
	if (status >= 400 && status < 500 && status != 408) || response.Code != "" || (len(response.Error) > 0 && string(response.Error) != "null") {
		v.Status = "failed"
	}
	if meta.capability == "video" {
		taskID := response.Output.ID
		if meta.poll {
			taskID = meta.taskID
		}
		if meta.poll && (response.Output.Status == "PENDING" || response.Output.Status == "RUNNING" || response.Output.Status == "" || response.Output.Status == "UNKNOWN") {
			return
		}
		if safeCode.MatchString(taskID) {
			taskHash := sha256.Sum256([]byte(provider + "\x00" + p.WorkspaceID + "\x00" + p.Model + "\x00" + taskID))
			v.ID = "video-" + hex.EncodeToString(taskHash[:])
		}
		switch response.Output.Status {
		case "PENDING", "RUNNING":
			v.Status = "pending"
		case "FAILED", "CANCELED":
			v.Status = "failed"
		case "SUCCEEDED":
			v.Status = "success"
		}
	}
	m := response.Usage
	v.InputTokens = usageNumber(m, "prompt_tokens", "input_tokens")
	v.OutputTokens = usageNumber(m, "completion_tokens", "output_tokens")
	v.TotalTokens = usageNumber(m, "total_tokens")
	if v.TotalTokens == nil && v.InputTokens != nil && v.OutputTokens != nil {
		n := *v.InputTokens + *v.OutputTokens
		v.TotalTokens = &n
	}
	if v.TotalTokens != nil {
		known := int64(0)
		if v.InputTokens != nil {
			known += *v.InputTokens
		}
		if v.OutputTokens != nil {
			known += *v.OutputTokens
		}
		if *v.TotalTokens < known {
			v.InputTokens, v.OutputTokens, v.TotalTokens = nil, nil, nil
		}
	}
	for _, key := range []string{"prompt_tokens_details", "input_tokens_details"} {
		var details map[string]json.RawMessage
		if json.Unmarshal(m[key], &details) == nil {
			v.CachedTokens = usageNumber(details, "cached_tokens")
			if v.CachedTokens != nil {
				break
			}
		}
	}
	var details map[string]json.RawMessage
	if json.Unmarshal(m["completion_tokens_details"], &details) == nil {
		v.ReasoningTokens = usageNumber(details, "reasoning_tokens")
	}
	if v.CachedTokens == nil {
		v.CachedTokens = usageNumber(m, "cached_tokens")
	}
	if v.InputTokens != nil && v.CachedTokens != nil && *v.CachedTokens > *v.InputTokens {
		v.CachedTokens = nil
	}
	if v.OutputTokens != nil && v.ReasoningTokens != nil && *v.ReasoningTokens > *v.OutputTokens {
		v.ReasoningTokens = nil
	}
	v.ImageCount = usageNumber(m, "image_count")
	if duration := m["output_video_duration"]; len(duration) > 0 {
		var seconds float64
		if json.Unmarshal(duration, &seconds) == nil && seconds >= 0 && seconds <= 86400 {
			v.VideoSeconds = &seconds
		}
	}
	// The ledger contains numeric usage and configured model identities only.
	// Response text, prompts, headers, keys and signed URLs are never retained.
	if p.APIKey != "" {
		v.Model = strings.ReplaceAll(v.Model, p.APIKey, "[redacted]")
	}
	c.usage(v)
}

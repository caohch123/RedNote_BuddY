package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xhs-workbench/internal/domain"
)

func (c *Client) Collect(ctx context.Context, rawURL, cookie string) (domain.Note, []domain.Asset, error) {
	n := domain.Note{}
	u, e := url.Parse(rawURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return n, nil, errors.New("采集链接无效")
	}
	host := strings.ToLower(u.Hostname())
	if host != "www.xiaohongshu.com" && host != "xiaohongshu.com" && host != "www.rednote.com" && host != "rednote.com" && host != "xhslink.com" && host != "xhslink.cn" {
		return n, nil, errors.New("仅支持小红书笔记链接")
	}
	if c.settings.CollectorURL == "" {
		return n, nil, errors.New("采集服务未配置或未启动")
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	e = c.request(ctx, domain.ProviderConfig{}, "POST", endpoint(c.settings.CollectorURL, "/xhs/detail"), map[string]any{"url": rawURL, "download": false, "check_record": false, "cookie": cookie}, &out)
	if e != nil {
		return n, nil, collectorError(e)
	}
	if len(out.Data) == 0 {
		return n, nil, errors.New("未获取到笔记，请检查链接、Cookie 或小红书访问限制")
	}
	d := out.Data
	n.NoteID = stringField(d, "作品ID", "note_id", "noteId")
	if n.NoteID == "" {
		return n, nil, errors.New("采集响应缺少笔记 ID")
	}
	n.Platform = "xiaohongshu"
	n.URL = rawURL
	n.Title = stringField(d, "作品标题", "title")
	n.Body = stringField(d, "作品描述", "desc", "body")
	n.Topics = stringSlice(field(d, "作品标签", "topics", "tagList"))
	n.Author = domain.Author{ID: stringField(d, "作者ID", "author_id"), Name: stringField(d, "作者昵称", "author_name"), AvatarURL: stringField(d, "作者头像", "avatar_url")}
	n.Type = "image"
	if strings.Contains(stringField(d, "作品类型", "type"), "视频") || stringField(d, "作品类型", "type") == "video" {
		n.Type = "video"
	}
	n.Likes = metric(field(d, "点赞数量", "likes", "likedCount"))
	n.Collects = metric(field(d, "收藏数量", "collects", "collectedCount"))
	n.Comments = metric(field(d, "评论数量", "comments", "commentCount"))
	n.PublishedAt = normalizedTime(field(d, "发布时间", "published_at", "time"))
	n.CollectedAt = time.Now().UTC().Format(time.RFC3339)
	n.Tags = []string{}
	n.AssetIDs = []string{}
	n.Status = "pending"
	n.MetadataStatus = "succeeded"
	n.MediaStatus = "pending"
	n.ExtractionStatus = "pending"
	assets := []domain.Asset{}
	add := func(link, kind string, pos int) {
		if link == "" {
			return
		}
		assets = append(assets, domain.Asset{Kind: kind, SourceURL: link, Position: pos, Status: "pending", Extraction: domain.Extraction{Status: "pending", Segments: []domain.Segment{}}})
	}
	links := stringSlice(field(d, "下载地址", "download_urls", "media_urls"))
	for i, link := range links {
		kind := "image"
		if n.Type == "video" {
			kind = "video"
		}
		add(link, kind, i)
	}
	for i, link := range stringSlice(field(d, "动图地址", "live_urls")) {
		if link != "NaN" && link != "None" {
			add(link, "video", len(links)+i)
		}
	}
	if n.Type == "video" {
		add(stringField(d, "封面地址", "cover_url"), "cover", len(assets))
	}
	// Build a new allowlisted snapshot. Upstream params and arbitrary data fields
	// are never persisted, even when nested under a provider result.
	n.Raw = map[string]any{"source": "XHS-Downloader 2.8", "note_id": n.NoteID, "title": n.Title, "body": n.Body, "topics": n.Topics, "author": n.Author, "type": n.Type, "published_at": n.PublishedAt}
	return n, assets, nil
}

func collectorError(err error) error {
	if errors.Is(err, ErrConnectionFailed) {
		return errors.New("小红书采集服务未连接或响应超时；请重新启动工作台，再到「设置 → 账号与采集」检测采集服务")
	}
	return err
}

func field(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
func stringField(m map[string]any, keys ...string) string {
	v := field(m, keys...)
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	}
	return ""
}
func stringSlice(v any) []string {
	result := []string{}
	switch x := v.(type) {
	case string:
		for _, part := range strings.Fields(x) {
			result = append(result, strings.TrimPrefix(part, "#"))
		}
	case []any:
		for _, part := range x {
			switch p := part.(type) {
			case string:
				if p != "" {
					result = append(result, strings.TrimPrefix(p, "#"))
				}
			case map[string]any:
				if s, ok := p["name"].(string); ok && s != "" {
					result = append(result, s)
				}
			}
		}
	case []string:
		return x
	}
	return result
}
func metric(v any) *int64 {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	s = strings.TrimSuffix(s, "+")
	s = strings.ReplaceAll(s, ",", "")
	multiple := float64(1)
	switch {
	case strings.HasSuffix(s, "万"):
		multiple = 10000
		s = strings.TrimSuffix(s, "万")
	case strings.HasSuffix(strings.ToLower(s), "w"):
		multiple = 10000
		s = s[:len(s)-1]
	case strings.HasSuffix(strings.ToLower(s), "k"):
		multiple = 1000
		s = s[:len(s)-1]
	}
	n, e := strconv.ParseFloat(s, 64)
	if e != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) || n*multiple > math.MaxInt64 {
		return nil
	}
	i := int64(n * multiple)
	return &i
}
func normalizedTime(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "未知" || s == "" {
		return ""
	}
	if n, e := strconv.ParseFloat(s, 64); e == nil {
		if n > 1e11 {
			n /= 1000
		}
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, format := range []string{time.RFC3339, "2006-01-02_15:04:05", "2006-01-02 15:04:05"} {
		if t, e := time.ParseInLocation(format, s, loc); e == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xhs-workbench/internal/domain"
)

// UploadVideoRewriteReference uses Bailian's model-bound private temporary
// storage. It runs only inside an explicitly confirmed generation job.
func (c *Client) UploadVideoRewriteReference(ctx context.Context, data []byte, extension string) (domain.VideoReferenceUpload, error) {
	var receipt domain.VideoReferenceUpload
	if len(data) == 0 || len(data) > 100<<20 || (extension != ".mp4" && extension != ".mov") {
		return receipt, errors.New("参考视频须为 MP4/MOV，且不超过 100 MB")
	}
	p, err := c.config("video")
	if err != nil {
		return receipt, err
	}
	base, err := WanVideoBase(p)
	if err != nil {
		return receipt, err
	}
	if !WanVideoModel(p.Model) {
		return receipt, errors.New("请配置百炼万相 3.0 视频模型")
	}
	var policy struct {
		Data struct {
			Policy          string          `json:"policy"`
			Signature       string          `json:"signature"`
			Directory       string          `json:"upload_dir"`
			Host            string          `json:"upload_host"`
			MaxSize         json.RawMessage `json:"max_file_size_mb"`
			AccessKey       string          `json:"oss_access_key_id"`
			ACL             string          `json:"x_oss_object_acl"`
			ForbidOverwrite string          `json:"x_oss_forbid_overwrite"`
		} `json:"data"`
	}
	query := url.Values{"action": {"getPolicy"}, "model": {p.Model}}
	if err = c.request(ctx, p, "GET", endpoint(base, "/uploads")+"?"+query.Encode(), nil, &policy); err != nil {
		return receipt, fmt.Errorf("参考视频上传凭证获取失败：%w", err)
	}
	v := policy.Data
	limitText := strings.Trim(string(v.MaxSize), `"`)
	var limit float64
	_ = json.Unmarshal([]byte(limitText), &limit)
	if limit <= 0 || float64(len(data)) > limit*1024*1024 {
		return receipt, errors.New("参考视频超过百炼临时上传容量限制")
	}
	u, err := url.Parse(v.Host)
	configured, _ := url.Parse(base)
	// The local fixture/gateway exception is restricted to the configured origin.
	local := configured.Scheme == "http" && u != nil && u.Scheme == configured.Scheme && u.Host == configured.Host
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (!local && (u.Scheme != "https" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".aliyuncs.com") || (u.Port() != "" && u.Port() != "443"))) {
		return receipt, errors.New("百炼返回了无效的临时上传地址")
	}
	if !strings.HasPrefix(v.Directory, "dashscope-instant/") || strings.Contains(v.Directory, "..") || strings.ContainsAny(v.Directory, "?#\\\r\n") || v.Policy == "" || v.Signature == "" || v.AccessKey == "" || v.ACL != "private" || v.ForbidOverwrite != "true" {
		return receipt, errors.New("百炼临时上传凭证不完整或无效")
	}
	random := make([]byte, 12)
	if _, err = rand.Read(random); err != nil {
		return receipt, errors.New("参考视频上传准备失败")
	}
	hash := sha256.Sum256(data)
	name := hex.EncodeToString(hash[:]) + "-" + hex.EncodeToString(random) + extension
	key := strings.TrimRight(v.Directory, "/") + "/" + name
	var envelope bytes.Buffer
	w := multipart.NewWriter(&envelope)
	for _, field := range [][2]string{{"OSSAccessKeyId", v.AccessKey}, {"Signature", v.Signature}, {"policy", v.Policy}, {"x-oss-object-acl", v.ACL}, {"x-oss-forbid-overwrite", v.ForbidOverwrite}, {"key", key}, {"success_action_status", "200"}} {
		if err = w.WriteField(field[0], field[1]); err != nil {
			return receipt, errors.New("参考视频上传准备失败")
		}
	}
	if _, err = w.CreateFormFile("file", name); err != nil {
		return receipt, errors.New("参考视频上传准备失败")
	}
	prefixSize := envelope.Len()
	if err = w.Close(); err != nil {
		return receipt, errors.New("参考视频上传准备失败")
	}
	body := io.MultiReader(bytes.NewReader(envelope.Bytes()[:prefixSize]), bytes.NewReader(data), bytes.NewReader(envelope.Bytes()[prefixSize:]))
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), body)
	if err != nil {
		return receipt, errors.New("参考视频上传准备失败")
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.ContentLength = int64(envelope.Len() + len(data))
	client := *c.http
	client.Timeout = 10 * time.Minute
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return receipt, errors.New("参考视频上传失败或超时，未提交视频生成任务")
	}
	defer resp.Body.Close()
	_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if readErr != nil || (resp.StatusCode != 200 && resp.StatusCode != 204) {
		return receipt, fmt.Errorf("参考视频上传失败（HTTP %d），未提交视频生成任务", resp.StatusCode)
	}
	receipt.URL = "oss://" + key
	// Provider URLs last 48h; leave an hour for queueing and clock differences.
	receipt.ExpiresAt = time.Now().UTC().Add(47 * time.Hour).Format(time.RFC3339)
	return receipt, nil
}

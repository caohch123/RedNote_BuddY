package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const maxImageBytes = 20 << 20

// GenerateImage implements the configured OpenAI-compatible image endpoint.
// A paid POST is sent once; ambiguous completion never becomes an automatic retry.
func (c *Client) GenerateImage(ctx context.Context, prompt string) ([]byte, string, error) {
	return c.generateImage(ctx, prompt, nil, "2:3")
}

func (c *Client) RewriteImage(ctx context.Context, prompt string, images [][]byte, ratio string) ([]byte, string, error) {
	p, err := c.config("image")
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(p.Model, "qwen-image-3.0") && !strings.HasPrefix(p.Model, "qwen-image-2.1") {
		return nil, "", errors.New("参考图仿写需使用支持 image 扩展参数的百炼 Qwen-Image 3.0／2.1 模型")
	}
	if len(images) < 1 || len(images) > 3 {
		return nil, "", errors.New("参考图片需为 1–3 张")
	}
	return c.generateImage(ctx, prompt, images, ratio)
}

func (c *Client) generateImage(ctx context.Context, prompt string, images [][]byte, ratio string) ([]byte, string, error) {
	sizes := map[string]string{"2:3": "1024x1536", "3:4": "960x1280", "1:1": "1024x1024", "9:16": "720x1280"}
	size, ok := sizes[ratio]
	if !ok {
		return nil, "", errors.New("图片比例请选择 3:4、1:1 或 9:16")
	}
	return c.generateImageSize(ctx, prompt, images, size, nil)
}

type ImageReceipt struct {
	RequestID string `json:"request_id"`
	URL       string `json:"url,omitempty"`
	Base64    string `json:"base64,omitempty"`
}

// GenerateImageAtSize supports a fully custom/automatic scheme without leaking
// an unselected primary image into the paid request. It uses the same receipt
// persistence and uncertain-submission behavior as referenced generation.
func (c *Client) GenerateImageAtSize(ctx context.Context, prompt string, size string, receipt func(ImageReceipt) error) ([]byte, string, error) {
	if !supportedImageOutputSize(size) {
		return nil, "", errors.New("图片输出尺寸不支持")
	}
	return c.generateImageSize(ctx, prompt, nil, size, receipt)
}

func supportedImageOutputSize(size string) bool {
	allowed := map[string]bool{"960x1280": true, "1536x2048": true, "1024x1024": true, "2048x2048": true, "720x1280": true, "1152x2048": true}
	return allowed[size]
}

// RewriteImageAtSize sends the reviewed prompt unchanged and persists the receipt
// before downloading. Restoring a receipt never repeats the paid POST.
func (c *Client) RewriteImageAtSize(ctx context.Context, prompt string, images [][]byte, size string, receipt func(ImageReceipt) error) ([]byte, string, error) {
	p, err := c.config("image")
	if err != nil {
		return nil, "", err
	}
	if (!strings.HasPrefix(p.Model, "qwen-image-3.0") && !strings.HasPrefix(p.Model, "qwen-image-2.1")) || len(images) < 1 || len(images) > 3 {
		return nil, "", errors.New("图片仿写需要 Qwen-Image 3.0／2.1 和 1–3 张输入图")
	}
	if !supportedImageOutputSize(size) {
		return nil, "", errors.New("图片输出尺寸不支持")
	}
	return c.generateImageSize(ctx, prompt, images, size, receipt)
}

func (c *Client) RestoreImage(ctx context.Context, receipt ImageReceipt) ([]byte, string, error) {
	p, err := c.config("image")
	if err != nil {
		return nil, "", err
	}
	var data []byte
	if receipt.Base64 != "" {
		if base64.StdEncoding.DecodedLen(len(receipt.Base64)) > maxImageBytes {
			return nil, "", errors.New("图片超过大小限制")
		}
		data, err = base64.StdEncoding.DecodeString(receipt.Base64)
	} else {
		base, e := url.Parse(p.BaseURL)
		if e != nil {
			return nil, "", e
		}
		data, err = c.downloadImage(ctx, base, receipt.URL)
	}
	if err != nil {
		return nil, "", err
	}
	return validateGeneratedImage(data)
}

func (c *Client) generateImageSize(ctx context.Context, prompt string, images [][]byte, size string, receipt func(ImageReceipt) error) ([]byte, string, error) {
	p, err := c.config("image")
	if err != nil {
		return nil, "", err
	}
	base, _ := url.Parse(p.BaseURL)
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, "", errors.New("图片 API 地址必须使用 HTTP 或 HTTPS")
	}
	if strings.TrimSpace(prompt) == "" || len([]rune(prompt)) > 4000 {
		return nil, "", errors.New("图片提示词需为 1–4000 个字符")
	}
	input := map[string]any{"model": p.Model, "prompt": prompt, "n": 1, "size": size, "prompt_extend": false}
	refs := []string{}
	if len(images) > 0 {
		for _, img := range images {
			if len(img) > 10<<20 {
				return nil, "", errors.New("参考图片不得超过 10 MB")
			}
			_, mime, e := validateGeneratedImage(img)
			config, _, decodeErr := image.DecodeConfig(bytes.NewReader(img))
			if e != nil || decodeErr != nil || config.Width > 8000 || config.Height > 8000 || config.Width > config.Height*10 || config.Height > config.Width*10 {
				return nil, "", errors.New("参考图片无效或尺寸超过 8000 像素")
			}
			refs = append(refs, "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(img))
		}
		input["image"] = refs
	}
	native := nativeImageBase(base)
	requestURL := endpoint(p.BaseURL, "/images/generations")
	if native {
		content := []map[string]string{}
		for _, ref := range refs {
			content = append(content, map[string]string{"image": ref})
		}
		content = append(content, map[string]string{"text": prompt})
		input = map[string]any{"model": p.Model, "input": map[string]any{"messages": []map[string]any{{"role": "user", "content": content}}}, "parameters": map[string]any{"n": 1, "size": strings.ReplaceAll(size, "x", "*"), "prompt_extend": false}}
		requestURL = endpoint(p.BaseURL, "/services/aigc/multimodal-generation/generation")
	}
	payload, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, "", errors.New("图片 API 地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	requestClient := *c.http
	requestClient.Timeout = 10 * time.Minute
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	unknown := fmt.Errorf("图片生成提交结果不明，请核实供应商记录后再制作: %w", ErrSubmissionUnknown)
	release, err := c.acquireUsage(ctx, "image")
	if err != nil {
		return nil, "", err
	}
	defer release()
	resp, err := requestClient.Do(req)
	if err != nil {
		c.reportUsage(p, usageContext{capability: "image"}, 0, nil)
		return nil, "", unknown
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20+1))
	usageStatus := resp.StatusCode
	if err != nil || len(raw) > 32<<20 {
		usageStatus = 0
	}
	c.reportUsage(p, usageContext{capability: "image"}, usageStatus, raw)
	if err != nil || len(raw) > 32<<20 {
		return nil, "", unknown
	}
	if resp.StatusCode >= 500 || resp.StatusCode == 408 || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		return nil, "", unknown
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == 404 && strings.HasPrefix(p.Model, "qwen-image-") {
			return nil, "", fmt.Errorf("%w；请核对百炼图片模型权限及完整 API Host，图片调用示例使用业务空间专属 /compatible-mode/v1 地址", statusError(resp.StatusCode, raw))
		}
		return nil, "", statusError(resp.StatusCode, raw)
	}
	var out struct {
		RequestID string `json:"request_id"`
		Data      []struct {
			Base64 string `json:"b64_json"`
			URL    string `json:"url"`
		} `json:"data"`
		Code   string `json:"code"`
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil, "", unknown
	}
	if out.Code != "" {
		return nil, "", statusError(400, raw)
	}
	resultBase64, resultURL := "", ""
	if native {
		urls := []string{}
		for _, choice := range out.Output.Choices {
			for _, entry := range choice.Message.Content {
				if entry.Image != "" {
					urls = append(urls, entry.Image)
				}
			}
		}
		if len(urls) != 1 {
			return nil, "", unknown
		}
		resultURL = urls[0]
	} else {
		if len(out.Data) != 1 {
			return nil, "", unknown
		}
		resultBase64, resultURL = out.Data[0].Base64, out.Data[0].URL
	}
	var data []byte
	if receipt != nil {
		requestID := resp.Header.Get("x-request-id")
		if requestID == "" {
			requestID = out.RequestID
		}
		if err := receipt(ImageReceipt{RequestID: requestID, URL: resultURL, Base64: resultBase64}); err != nil {
			return nil, "", unknown
		}
	}
	outputError := func(err error) ([]byte, string, error) {
		return nil, "", fmt.Errorf("图片生成已返回，但本地保存结果失败（%v），请核实供应商生成结果: %w", err, ErrSubmissionUnknown)
	}
	if resultBase64 != "" {
		if base64.StdEncoding.DecodedLen(len(resultBase64)) > maxImageBytes {
			return outputError(errors.New("生成图片超过 20 MB"))
		}
		data, err = base64.StdEncoding.DecodeString(resultBase64)
		if err != nil {
			return outputError(errors.New("图片服务返回无效图片编码"))
		}
	} else if resultURL != "" {
		data, err = c.downloadImage(ctx, base, resultURL)
		if err != nil {
			return outputError(err)
		}
	} else {
		return nil, "", unknown
	}
	verified, mime, err := validateGeneratedImage(data)
	if err != nil {
		return outputError(err)
	}
	return verified, mime, nil
}

func nativeImageBase(base *url.URL) bool {
	path := strings.TrimRight(base.Path, "/")
	return strings.HasSuffix(path, "/api/v1") || strings.HasSuffix(path, "/api/v1/services/aigc/multimodal-generation/generation")
}

func validateGeneratedImage(data []byte) ([]byte, string, error) {
	if len(data) == 0 || len(data) > maxImageBytes {
		return nil, "", errors.New("生成图片为空或超过 20 MB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	mime := http.DetectContentType(data)
	if err != nil || !strings.HasPrefix(mime, "image/") || config.Width < 1 || config.Height < 1 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 100000000 {
		return nil, "", errors.New("图片服务未返回有效图片")
	}
	return data, mime, nil
}

func (c *Client) downloadImage(ctx context.Context, base *url.URL, raw string) ([]byte, error) {
	validate := func(u *url.URL) error {
		if u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return errors.New("生成图片地址无效")
		}
		if u.Host == base.Host && u.Scheme == base.Scheme {
			return nil
		}
		if u.Scheme != "https" {
			return errors.New("生成图片必须来自 HTTPS 公网地址")
		}
		if p := u.Port(); p != "" && p != "443" {
			return errors.New("生成图片端口不允许")
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || validate(u) != nil {
		return nil, errors.New("生成图片地址无效")
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 15 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("生成图片地址无效")
		}
		if strings.EqualFold(address, base.Host) || (host == base.Hostname() && port == "443" && base.Port() == "" && base.Scheme == "https") || (host == base.Hostname() && port == "80" && base.Port() == "" && base.Scheme == "http") {
			return dialer.DialContext(ctx, network, address)
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("生成图片域名解析失败")
		}
		for _, ip := range ips {
			if !imagePublicIP(ip) {
				return nil, errors.New("生成图片下载目标必须为公网地址")
			}
		}
		for _, ip := range ips {
			if conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("生成图片下载连接失败")
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 {
			return errors.New("生成图片重定向过多")
		}
		return validate(req.URL)
	}}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	// Provider credentials never travel to output URLs or their redirects.
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("下载生成图片失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载生成图片 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		return nil, errors.New("生成图片超过 20 MB 或读取失败")
	}
	return data, nil
}

func imagePublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		_, network, _ := net.ParseCIDR(block)
		if network.Contains(ip) {
			return false
		}
	}
	return ip.IsGlobalUnicast()
}

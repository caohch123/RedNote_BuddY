package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/tooling"
)

// Exact suffix boundaries prevent names such as xhscdn.com.attacker.example.
func allowedMediaHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range []string{"xhscdn.com", "xiaohongshu.com", "xhslink.com", "xhslink.cn", "volces.com", "volccdn.com", "byteimg.com", "ibyteimg.com", "bytecdn.cn", "volcengine.com", "tos-cn-beijing.volces.com"} {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		_, n, _ := net.ParseCIDR(block)
		if n.Contains(ip) {
			return false
		}
	}
	return ip.IsGlobalUnicast()
}
func validateMediaURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Host == "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || !allowedMediaHost(u.Hostname()) {
		return errors.New("媒体下载地址不在允许的平台域名内")
	}
	if p := u.Port(); p != "" && p != "443" && p != "80" {
		return errors.New("媒体端口不允许")
	}
	return nil
}
func publicMediaTransport(allowedHost func(string) bool) *http.Transport {
	return &http.Transport{Proxy: nil, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		if !allowedHost(host) {
			return nil, errors.New("下载域名不允许")
		}
		ips, e := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if e != nil {
			return nil, errors.New("下载域名解析失败")
		}
		if len(ips) == 0 {
			return nil, errors.New("下载地址不存在")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("下载目标必须为公网地址")
			}
		}
		var last error
		dialer := net.Dialer{Timeout: 15 * time.Second}
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}}
}
func mediaHTTPClient() *http.Client {
	return &http.Client{Transport: publicMediaTransport(allowedMediaHost), Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("下载重定向次数过多")
		}
		return validateMediaURL(req.URL.String())
	}}
}

type mediaHTTPStatusError struct{ Status int }

func (e *mediaHTTPStatusError) Error() string { return fmt.Sprintf("媒体下载 HTTP %d", e.Status) }

type mediaTransientError struct{ message string }

func (e *mediaTransientError) Error() string { return e.message }
func (a *App) downloadAsset(ctx context.Context, asset *domain.Asset) error {
	if e := validateMediaURL(asset.SourceURL); e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", asset.SourceURL, nil)
	if e != nil {
		return errors.New("下载地址无效")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.xiaohongshu.com/")
	resp, e := mediaHTTPClient().Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &mediaTransientError{message: "媒体下载失败，请检查网络或重新采集链接"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &mediaHTTPStatusError{Status: resp.StatusCode}
	}
	if resp.ContentLength > 512<<20 {
		return errors.New("媒体超过 512 MB 限制")
	}
	head := make([]byte, 512)
	n, e := io.ReadFull(resp.Body, head)
	if e != nil && e != io.EOF && e != io.ErrUnexpectedEOF {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &mediaTransientError{message: "媒体响应中断，请重新采集"}
	}
	mime := http.DetectContentType(head[:n])
	kind, ext := mediaType(mime)
	if kind == "" {
		mime = strings.Split(resp.Header.Get("Content-Type"), ";")[0]
		kind, ext = mediaType(mime)
	}
	if kind == "" || kind == "subtitle" {
		return errors.New("下载响应不是有效媒体")
	}
	if asset.Kind == "video" && kind != "video" {
		return errors.New("视频下载返回的内容类型不正确")
	}
	if (asset.Kind == "cover" || asset.Kind == "image") && kind != "image" {
		return errors.New("图片下载返回的内容类型不正确")
	}
	path := filepath.Join(a.Data, "assets", asset.ID+ext)
	tmp := path + ".part"
	out, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(e, io.ErrUnexpectedEOF) || errors.Is(e, io.EOF) {
			return &mediaTransientError{message: "媒体下载不完整，请重新采集"}
		}
		return errors.New("媒体保存中断，请检查网络与磁盘")
	}
	defer os.Remove(tmp)
	h := sha256.New()
	writer := io.MultiWriter(out, h)
	_, e = writer.Write(head[:n])
	var count int64
	if e == nil {
		count, e = io.Copy(writer, io.LimitReader(resp.Body, (512<<20)-int64(n)+1))
	}
	closeErr := out.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if count+int64(n) > 512<<20 {
		return errors.New("媒体超过 512 MB 限制")
	}
	size := count + int64(n)
	if resp.ContentLength >= 0 && size != resp.ContentLength {
		return &mediaTransientError{message: "媒体下载不完整，请重新采集"}
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	asset.MIME = mime
	if asset.Kind != "cover" {
		asset.Kind = kind
	}
	asset.LocalPath, _ = filepath.Rel(a.Data, path)
	asset.Size = count + int64(n)
	asset.SHA256 = hex.EncodeToString(h.Sum(nil))
	asset.Status = "ready"
	asset.Error = ""
	a.probeAsset(asset)
	return nil
}

func (a *App) probeAsset(v *domain.Asset) {
	path, e := a.assetPath(*v)
	if e != nil {
		return
	}
	if strings.HasPrefix(v.MIME, "image/") {
		if f, e := os.Open(path); e == nil {
			cfg, _, err := image.DecodeConfig(f)
			f.Close()
			if err == nil {
				v.Width = cfg.Width
				v.Height = cfg.Height
				return
			}
		}
	}
	s, e := a.settings()
	if e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	out, e := tooling.Command(ctx, a.Root, s.FFprobePath, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=width,height,duration", "-of", "json", path).Output()
	if e != nil {
		return
	}
	var p struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration string `json:"duration"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &p) != nil {
		return
	}
	v.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	for _, st := range p.Streams {
		if st.Width > 0 {
			v.Width = st.Width
			v.Height = st.Height
		}
		if d, _ := strconv.ParseFloat(st.Duration, 64); d > v.Duration {
			v.Duration = d
		}
	}
}
func (a *App) generatedAsset(owner, kind, path, mime string) (domain.Asset, error) {
	if !within(a.Data, path) {
		return domain.Asset{}, errors.New("生成素材路径超出数据目录")
	}
	f, e := os.Open(path)
	if e != nil {
		return domain.Asset{}, e
	}
	h := sha256.New()
	n, e := io.Copy(h, f)
	f.Close()
	if e != nil {
		return domain.Asset{}, e
	}
	if n == 0 {
		return domain.Asset{}, errors.New("生成文件为空")
	}
	rel, _ := filepath.Rel(a.Data, path)
	v := domain.Asset{ID: ID(), OwnerID: owner, Kind: kind, LocalPath: rel, MIME: mime, Size: n, SHA256: hex.EncodeToString(h.Sum(nil)), Status: "ready", CreatedAt: now(), Extraction: domain.Extraction{Status: "not_applicable", Segments: []domain.Segment{}}}
	a.probeAsset(&v)
	return v, a.Store.Put("assets", v.ID, v)
}

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/tooling"
)

const videoRewriteAnalysisRulesVersion = 1
const videoRewriteMaxInputBytes int64 = 100 << 20

type videoRewriteProbe struct {
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
		FrameRate string `json:"avg_frame_rate"`
	} `json:"streams"`
}

func (a *App) probeVideoRewrite(ctx context.Context, path string) (videoRewriteProbe, error) {
	var out videoRewriteProbe
	s, err := a.settings()
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := tooling.Command(ctx, a.Root, s.FFprobePath, "ffprobe", "-v", "error", "-show_entries", "format=duration,format_name:stream=codec_type,codec_name,width,height,duration,avg_frame_rate", "-of", "json", path).Output()
	if err != nil || json.Unmarshal(data, &out) != nil {
		return out, errors.New("视频检查失败，请检查 FFprobe 和文件格式")
	}
	if out.duration() <= 0 || out.width() == 0 || out.height() == 0 {
		return out, errors.New("素材没有有效视频轨或时长")
	}
	return out, nil
}
func (p videoRewriteProbe) duration() float64 {
	d, _ := strconv.ParseFloat(p.Format.Duration, 64)
	for _, s := range p.Streams {
		v, _ := strconv.ParseFloat(s.Duration, 64)
		if v > d {
			d = v
		}
	}
	return d
}
func (p videoRewriteProbe) width() int {
	for _, s := range p.Streams {
		if s.CodecType == "video" && s.Width > 0 {
			return s.Width
		}
	}
	return 0
}
func (p videoRewriteProbe) height() int {
	for _, s := range p.Streams {
		if s.CodecType == "video" && s.Height > 0 {
			return s.Height
		}
	}
	return 0
}
func (p videoRewriteProbe) audio() bool {
	for _, s := range p.Streams {
		if s.CodecType == "audio" {
			return true
		}
	}
	return false
}
func (p videoRewriteProbe) browserCompatible() bool {
	for _, s := range p.Streams {
		if s.CodecType == "video" && s.CodecName != "h264" {
			return false
		}
		if s.CodecType == "audio" && s.CodecName != "aac" && s.CodecName != "mp3" {
			return false
		}
	}
	return true
}

func (a *App) validateVideoRewriteMaterial(rewriteID, id, kind string, modelLimits ...bool) (domain.Asset, error) {
	var r domain.Rewrite
	var asset domain.Asset
	if a.Store.Get("rewrites", rewriteID, &r) != nil || a.Store.Get("assets", id, &asset) != nil {
		return asset, errors.New("素材不属于当前项目")
	}
	allowed := asset.OwnerID == r.ID || asset.OwnerID == r.NoteID
	if !allowed {
		var v domain.VideoProject
		allowed = a.Store.Get("videos", asset.OwnerID, &v) == nil && v.RewriteID == r.ID
	}
	rp, rok := a.recordWorkspacePlatform("rewrites", r.ID, map[string]bool{})
	ap, aok := a.recordWorkspacePlatform("assets", id, map[string]bool{})
	if !allowed || !rok || !aok || rp != ap {
		return asset, errors.New("素材不属于当前项目或工作区")
	}
	if asset.Status != "ready" || asset.LibraryDeletedAt != "" {
		return asset, errors.New("素材未下载完成或已移除")
	}
	path, err := a.assetPath(asset)
	if err != nil {
		return asset, err
	}
	file, err := os.Open(path)
	if err != nil {
		return asset, errors.New("本地素材不可用")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return asset, errors.New("本地素材文件无效")
	}
	if len(modelLimits) > 0 && !modelLimits[0] {
		if kind == "video" && (asset.Kind != "video" || !strings.HasPrefix(asset.MIME, "video/")) || kind == "image" && !strings.HasPrefix(asset.MIME, "image/") {
			return asset, errors.New("素材类型与位置不匹配")
		}
	} else {
		switch kind {
		case "video":
			if asset.Kind != "video" || !strings.HasPrefix(asset.MIME, "video/") || info.Size() > videoRewriteMaxInputBytes {
				return asset, errors.New("参考视频须为 MP4/MOV，且不超过 100 MB")
			}
			probe, e := a.probeVideoRewrite(a.ctx, path)
			if e != nil {
				return asset, e
			}
			if !strings.Contains(probe.Format.FormatName, "mov") && !strings.Contains(probe.Format.FormatName, "mp4") {
				return asset, errors.New("参考视频只支持 MP4/MOV")
			}
			if probe.duration() < 1 || probe.duration() > 15.001 {
				return asset, errors.New("参考视频须为 1–15 秒，请选择较短视频")
			}
			ratio := float64(probe.width()) / float64(probe.height())
			if probe.width() < 240 || probe.height() < 240 || probe.width() > 4096 || probe.height() > 4096 || ratio < 0.125 || ratio > 8 {
				return asset, errors.New("百炼参考视频单边需为 240–4096 像素，长宽比不超过 8:1")
			}
			for _, stream := range probe.Streams {
				if stream.CodecType == "video" {
					fpsParts := strings.Split(stream.FrameRate, "/")
					fps, _ := strconv.ParseFloat(fpsParts[0], 64)
					if len(fpsParts) == 2 {
						denominator, _ := strconv.ParseFloat(fpsParts[1], 64)
						if denominator > 0 {
							fps /= denominator
						}
					}
					if fps < 16 {
						return asset, errors.New("百炼参考视频帧率需至少 16fps")
					}
				}

			}
			asset.Width = probe.width()
			asset.Height = probe.height()
			asset.Duration = probe.duration()
		case "image":
			if !strings.HasPrefix(asset.MIME, "image/") {
				return asset, errors.New("请选择参考图片")
			}
			data, e := io.ReadAll(io.LimitReader(file, 20<<20+1))
			if e != nil {
				return asset, e
			}
			if _, e = providers.ValidateWanVideoImage(data); e != nil {
				return asset, e
			}
			if _, e = file.Seek(0, io.SeekStart); e != nil {
				return asset, e
			}
			cfg, _, e := image.DecodeConfig(file)
			if e != nil {
				return asset, e
			}

			asset.Width = cfg.Width
			asset.Height = cfg.Height
		default:
			return asset, errors.New("不支持的参考素材类型")
		}
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return asset, err
	}
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return asset, err
	}
	asset.SHA256 = hex.EncodeToString(h.Sum(nil))
	asset.Size = info.Size()
	return asset, nil
}

func (a *App) videoRewriteTemp(suffix string) (string, error) {
	path := filepath.Join(a.Data, "assets", ID()+suffix)
	if _, err := a.checkedMediaDeletionPath(path); err != nil {
		return "", err
	}
	return path, nil
}
func (a *App) removeVideoRewriteTemp(path string) {
	if safe, e := a.checkedMediaDeletionPath(path); e == nil {
		_ = os.Remove(safe)
	}
}

func (a *App) analyzeVideoRewriteMaterial(ctx context.Context, j *domain.Job, client *providers.Client, assetID string) (domain.VideoMaterialAnalysis, error) {
	var result domain.VideoMaterialAnalysis
	var project domain.VideoProject
	var original domain.Asset
	if a.Store.Get("videos", j.EntityID, &project) != nil || a.Store.Get("assets", assetID, &original) != nil {
		return result, errors.New("视频项目或素材不存在")
	}
	kind := "video"
	if strings.HasPrefix(original.MIME, "image/") {
		kind = "image"
	}
	asset, err := a.validateVideoRewriteMaterial(project.RewriteID, assetID, kind)
	if err != nil {
		return result, err
	}
	settings, err := a.settings()
	if err != nil {
		return result, err
	}
	configs := j.ProviderSnapshot
	if len(configs) == 0 {
		configs = settings.Providers
	}
	transcript := ""
	if asset.Extraction.Edited || asset.Extraction.Status == "succeeded" {
		transcript = strings.TrimSpace(asset.Extraction.Text)
	}
	if len([]rune(transcript)) > 12000 {
		return result, errors.New("素材转录超过 12000 字，请精简后重新分析")
	}
	signature := digestImageValue([]string{imageConfigSignature(configs["vision"]), imageConfigSignature(configs["asr"]), transcript, strconv.FormatBool(asset.Extraction.Edited)})
	key := digestImageValue([]string{asset.SHA256, kind, signature, strconv.Itoa(videoRewriteAnalysisRulesVersion)})
	originalKey := key
	if a.Store.Get("video_analysis_cache", key, &result) == nil {
		result.AssetID = asset.ID
		return result, nil
	}
	path, err := a.assetPath(asset)
	if err != nil {
		return result, err
	}
	features := map[string]string{}
	if kind == "image" {
		data, e := os.ReadFile(path)
		if e != nil {
			return result, e
		}
		if len(data) > 10<<20 {
			preview, e := a.videoRewriteTemp(".analysis.jpg")
			if e != nil {
				return result, e
			}
			defer a.removeVideoRewriteTemp(preview)
			if e = tooling.Command(ctx, a.Root, settings.FFmpegPath, "ffmpeg", "-nostdin", "-y", "-i", path, "-frames:v", "1", "-vf", "scale=1024:1024:force_original_aspect_ratio=decrease", preview).Run(); e != nil {
				return result, errors.New("参考图片分析副本制作失败，请检查 FFmpeg")
			}
			data, e = os.ReadFile(preview)
			if e != nil {
				return result, e
			}
		}
		row, e := client.AnalyzeImage(ctx, data)
		if e != nil {
			return result, e
		}
		features = map[string]string{"subject": row.Subject, "style": strings.Join([]string{row.Texture, row.Palette, row.Lighting}, "；")}
	} else {
		probe, e := a.probeVideoRewrite(ctx, path)
		if e != nil {
			return result, e
		}
		if !probe.audio() {
			transcript = "无音轨"
		} else if transcript == "" && !asset.Extraction.Edited && asset.Extraction.Status != "succeeded" {
			pcm, e := a.videoRewriteTemp(".pcm")
			if e != nil {
				return result, e
			}
			defer a.removeVideoRewriteTemp(pcm)
			if e = tooling.Command(ctx, a.Root, settings.FFmpegPath, "ffmpeg", "-nostdin", "-y", "-i", path, "-vn", "-ac", "1", "-ar", "16000", "-f", "s16le", pcm).Run(); e != nil {
				return result, errors.New("音轨提取失败，请检查 FFmpeg")
			}
			segments, e := client.Transcribe(ctx, pcm)
			if e != nil {
				return result, e
			}
			parts := []string{}
			for _, s := range segments {
				parts = append(parts, s.Text)
			}
			transcript = strings.Join(parts, "\n")
			if len([]rune(transcript)) > 12000 {
				return result, errors.New("素材转录超过 12000 字，请精简后重新分析")
			}
			a.mu.Lock()
			var current domain.Asset
			if a.Store.Get("assets", asset.ID, &current) == nil {
				if current.Extraction.Edited {
					transcript = strings.TrimSpace(current.Extraction.Text)
				} else {
					current.Extraction = domain.Extraction{Text: transcript, Segments: segments, Status: "succeeded"}
					err = a.Store.Put("assets", asset.ID, current)
				}
				asset.Extraction = current.Extraction
			}
			a.mu.Unlock()
			if err != nil {
				return result, err
			}
			if len([]rune(transcript)) > 12000 {
				return result, errors.New("素材转录超过 12000 字，请精简后重新分析")
			}
			signature = digestImageValue([]string{imageConfigSignature(configs["vision"]), imageConfigSignature(configs["asr"]), transcript, strconv.FormatBool(asset.Extraction.Edited)})
			key = digestImageValue([]string{asset.SHA256, kind, signature, strconv.Itoa(videoRewriteAnalysisRulesVersion)})
		}
		frames := []providers.VideoAnalysisFrame{}
		count := min(8, max(3, int(math.Ceil(probe.duration()/4))+1))
		for i := 0; i < count; i++ {
			ts := math.Max(0, (probe.duration()-0.1)*float64(i)/float64(count-1))
			frame, e := a.videoRewriteTemp(".jpg")
			if e != nil {
				return result, e
			}
			e = tooling.Command(ctx, a.Root, settings.FFmpegPath, "ffmpeg", "-nostdin", "-y", "-ss", strconv.FormatFloat(ts, 'f', 3, 64), "-i", path, "-frames:v", "1", "-vf", "scale=960:960:force_original_aspect_ratio=decrease", "-q:v", "3", frame).Run()
			if e != nil {
				a.removeVideoRewriteTemp(frame)
				return result, errors.New("视频抽帧失败，请检查 FFmpeg")
			}
			data, e := os.ReadFile(frame)
			a.removeVideoRewriteTemp(frame)
			if e != nil {
				return result, e
			}
			frames = append(frames, providers.VideoAnalysisFrame{Timestamp: ts, Image: data})
		}
		features, err = client.AnalyzeVideoRewriteFrames(ctx, frames, transcript)
		if err != nil {
			return result, err
		}
	}
	result = domain.VideoMaterialAnalysis{AssetID: asset.ID, SHA256: asset.SHA256, Kind: kind, ModelSignature: signature, RulesVersion: videoRewriteAnalysisRulesVersion, Features: features, Transcript: transcript}
	// Content-identical assets reuse machine analysis; manual text is included in
	// the cache key and never written into another asset's extraction record.
	err = a.Store.Put("video_analysis_cache", key, result)
	if err == nil && key != originalKey {
		err = a.Store.Put("video_analysis_cache", originalKey, result)
	}
	return result, err
}

func validateVideoRewritePublicURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("视频需要可直接下载的公网 HTTPS 链接")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || !strings.Contains(host, ".") {
		return errors.New("视频链接必须指向公网地址")
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return errors.New("视频链接必须指向公网地址")
	}
	return nil
}
func videoRewritePublicClient() *http.Client {
	return &http.Client{Transport: publicMediaTransport(func(string) bool { return true }), Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("视频链接重定向过多")
		}
		return validateVideoRewritePublicURL(req.URL.String())
	}}
}

// A private seam permits deterministic download/recovery tests without relaxing
// production's DNS pinning or allowing localhost as a user-provided URL.
var videoRewriteFetch = downloadVideoRewriteURL

func downloadVideoRewriteURL(ctx context.Context, raw, path string, limit int64) (string, error) {
	if err := validateVideoRewritePublicURL(raw); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return "", errors.New("视频链接无效")
	}
	resp, err := videoRewritePublicClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("视频链接不可访问，请检查有效期和访问权限")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > limit {
		return "", errors.New("视频链接无法下载或文件超过大小限制")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil || size == 0 || size > limit || (resp.ContentLength >= 0 && size != resp.ContentLength) {
		return "", errors.New("视频下载不完整或超出大小限制")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) downloadVideoRewriteResult(ctx context.Context, j *domain.Job, raw string, output domain.VideoOutput, providerResolution ...int) (assetID, previewID, coverID, warning string, err error) {
	if err = validateVideoRewriteResultURL(raw); err != nil {
		return
	}
	path, e := a.videoRewriteTemp(".mp4")
	if e != nil {
		err = e
		return
	}
	keep := false
	defer func() {
		if !keep {
			a.removeVideoRewriteTemp(path)
		}
	}()
	if _, err = videoRewriteFetch(ctx, raw, path, 512<<20); err != nil {
		return
	}
	probe, e := a.probeVideoRewrite(ctx, path)
	if e != nil {
		err = e
		return
	}
	if !strings.Contains(probe.Format.FormatName, "mov") && !strings.Contains(probe.Format.FormatName, "mp4") {
		err = errors.New("视频模型返回的文件不是有效 MP4/MOV")
		return
	}
	asset, e := a.generatedAsset(j.EntityID, "video", path, "video/mp4")
	if e != nil {
		err = e
		return
	}
	keep = true
	assetID = asset.ID
	parts := []string{}
	if math.Abs(probe.duration()-float64(output.Duration)) > 0.35 {
		parts = append(parts, fmt.Sprintf("实际时长 %.2f 秒与要求 %d 秒不同，已保留原片", probe.duration(), output.Duration))
	}
	ratio := strings.Split(output.Ratio, ":")
	if len(ratio) == 2 {
		w, _ := strconv.ParseFloat(ratio[0], 64)
		h, _ := strconv.ParseFloat(ratio[1], 64)
		if h > 0 && math.Abs(float64(probe.width())/float64(probe.height())-w/h) > 0.025 {
			parts = append(parts, "实际画幅比例与要求不同，已保留原片")
		}
	}
	requestedResolution, _ := strconv.Atoi(strings.TrimSuffix(strings.ToLower(output.Resolution), "p"))
	if len(providerResolution) > 0 && providerResolution[0] > 0 && providerResolution[0] != requestedResolution {
		parts = append(parts, "供应商返回的分辨率档位与要求不同，已保留原片")
	}
	if output.GenerateAudio && !probe.audio() {
		parts = append(parts, "返回视频没有音轨，已保留原片")
	}
	s, e := a.settings()
	if e != nil {
		warning = strings.Join(parts, "；")
		return
	}
	if !probe.browserCompatible() {
		preview, e := a.videoRewriteTemp(".preview.mp4")
		if e == nil {
			args := []string{"-nostdin", "-y", "-i", path, "-map", "0:v:0", "-map", "0:a?", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", preview}
			if e = tooling.Command(ctx, a.Root, s.FFmpegPath, "ffmpeg", args...).Run(); e == nil {
				var p domain.Asset
				p, e = a.generatedAsset(j.EntityID, "video", preview, "video/mp4")
				if e == nil {
					previewID = p.ID
				}
			}
			if e != nil {
				a.removeVideoRewriteTemp(preview)
				parts = append(parts, "播放兼容副本制作失败，可下载原片")
			}
		}
	}
	cover, e := a.videoRewriteTemp(".cover.jpg")
	if e == nil {
		if e = tooling.Command(ctx, a.Root, s.FFmpegPath, "ffmpeg", "-nostdin", "-y", "-i", path, "-frames:v", "1", "-vf", "scale=960:960:force_original_aspect_ratio=decrease", cover).Run(); e == nil {
			var c domain.Asset
			c, e = a.generatedAsset(j.EntityID, "cover", cover, "image/jpeg")
			if e == nil {
				coverID = c.ID
			}
		}
		if e != nil {
			a.removeVideoRewriteTemp(cover)
		}
	}
	warning = strings.Join(parts, "；")
	return
}

// Alibaba result URLs are validated separately from collected platform assets.
// No bearer credentials are attached; redirects and DNS stay public and pinned.
func validateVideoRewriteResultURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("视频结果地址无效")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !strings.HasSuffix(host, ".aliyuncs.com") && !allowedMediaHost(host) {
		return errors.New("视频结果地址不属于允许的供应商域名")
	}
	return nil
}

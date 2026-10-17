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
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
	"xhs-workbench/internal/tooling"
)

var errXHSPublishUnknown = errors.New("发布结果不明，请到小红书核实。为避免重复发布，不会自动重试")
var xhsNoteIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

type xhsPublishAsset struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	MIME     string  `json:"mime"`
	Status   string  `json:"status"`
	Position int     `json:"position"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration"`
	Size     int64   `json:"size"`
	SHA256   string  `json:"sha256"`
	Path     string  `json:"-"`
}

type xhsPublishPreview struct {
	ConfirmationToken string            `json:"confirmation_token,omitempty"`
	ExpiresAt         string            `json:"expires_at,omitempty"`
	Account           *xhsAccount       `json:"account"`
	PublicationID     string            `json:"publication_id"`
	Title             string            `json:"title"`
	Body              string            `json:"body"`
	Topics            []string          `json:"topics"`
	Assets            []xhsPublishAsset `json:"assets"`
	MediaType         string            `json:"media_type"`
	Visibility        string            `json:"visibility"`
}

type xhsPublishToken struct {
	PublicationID string
	Fingerprint   string
	AccountID     string
	Generation    string
	Visibility    string
	ExpiresAt     time.Time
}

type xhsPublishPayload struct {
	Preview     xhsPublishPreview `json:"preview"`
	Fingerprint string            `json:"fingerprint"`
}

func xhsFinalDescription(body string, topics []string) string {
	for _, topic := range topics {
		body += " #" + strings.TrimPrefix(strings.TrimSpace(topic), "#") + "[话题]# "
	}
	return body
}

func xhsFingerprint(v xhsPublishPreview) string {
	v.ConfirmationToken = ""
	v.ExpiresAt = ""
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (a *App) validateXHSPublication(ctx context.Context, p domain.Publication, account *xhsAccount, visibility string) (xhsPublishPreview, error) {
	v := xhsPublishPreview{PublicationID: p.ID, Account: account, Title: p.Title, Body: p.Body, Topics: append([]string{}, p.Topics...), Visibility: visibility, MediaType: "image", Assets: []xhsPublishAsset{}}
	if publicationPlatform(p) != "xiaohongshu" {
		return v, errors.New("该发布稿目标不是小红书工作区，已停止发布")
	}
	if _, valid := a.publicationWorkspacePlatform(p, map[string]bool{}); !valid {
		return v, errors.New("发布稿来源或素材不属于小红书工作区，已停止发布")
	}
	if visibility != "public" && visibility != "private" {
		return v, errors.New("可见范围请选择公开或仅自己可见")
	}
	if p.Status == "published" || p.Status == "publishing" || p.Status == "submission_unknown" {
		return v, errors.New("该发布稿已发布、正在发布或结果不明，请先核实已有任务")
	}
	var rewrite domain.Rewrite
	if e := a.Store.Get("rewrites", p.RewriteID, &rewrite); e != nil {
		return v, errors.New("来源仿写项目不存在")
	}
	version, ok := getVersion(rewrite, p.VersionID)
	if !ok || !version.Confirmed {
		return v, errors.New("请先确认发布稿对应的仿写版本")
	}
	if p.Title != version.Title || p.Body != version.Body || !equalStrings(p.Topics, version.Topics) {
		return v, errors.New("发布稿与确认版本不一致，请重新创建发布稿")
	}
	if strings.TrimSpace(p.Title) == "" || utf8.RuneCountInString(p.Title) > 20 {
		return v, errors.New("本工具支持非空且不超过20字符的标题，请修改仿写版本后重新确认")
	}
	if len(p.Topics) > 10 {
		return v, errors.New("每条笔记最多支持10个话题")
	}
	for _, t := range p.Topics {
		if strings.TrimSpace(t) == "" || strings.HasPrefix(strings.TrimSpace(t), "#") || strings.ContainsAny(t, "\n\r[]") || utf8.RuneCountInString(t) > 30 {
			return v, errors.New("话题不超过30字符，名称不含开头的 #、换行或方括号")
		}
	}
	if utf8.RuneCountInString(xhsFinalDescription(p.Body, p.Topics)) > 1000 {
		return v, errors.New("正文连同实际发送的话题标记不能超过1000字符，请缩短并重新确认")
	}
	if len(p.AssetIDs) == 0 {
		return v, errors.New("直接发布需要至少一张图片或一个视频，请先准备素材")
	}
	selectedIDs := []string{}
	for _, id := range p.AssetIDs {
		var asset domain.Asset
		if e := a.Store.Get("assets", id, &asset); e != nil {
			return v, errors.New("发布素材尚未准备完成")
		}
		if asset.Kind == "video" {
			v.MediaType = "video"
		}
		if asset.Kind != "cover" || asset.OwnerID != p.ID || has(version.AssetIDs, id) {
			selectedIDs = append(selectedIDs, id)
		}
	}
	if p.VideoProjectID != "" {
		v.MediaType = "video"
	} else if v.MediaType == "image" && !equalStrings(p.AssetIDs, version.AssetIDs) {
		return v, errors.New("素材顺序或内容与确认版本不一致，请重新创建发布稿")
	} else if !equalStrings(selectedIDs, version.AssetIDs) {
		return v, errors.New("素材顺序或内容与确认版本不一致，请重新创建发布稿")
	}
	seen := map[string]bool{}
	videos, covers := 0, 0
	for _, id := range p.AssetIDs {
		if seen[id] {
			return v, errors.New("发布素材重复，请重新准备")
		}
		seen[id] = true
		var asset domain.Asset
		if e := a.Store.Get("assets", id, &asset); e != nil || asset.Status != "ready" {
			return v, errors.New("发布素材尚未准备完成")
		}
		if v.MediaType == "video" && asset.Kind == "subtitle" {
			continue
		}
		if v.MediaType == "image" && asset.Kind != "image" && asset.Kind != "cover" {
			return v, errors.New("图文发布稿只能包含图片")
		}
		if v.MediaType == "video" && asset.Kind != "video" && asset.Kind != "cover" && asset.Kind != "image" {
			return v, errors.New("视频发布稿只能包含一个视频，可选一张封面")
		}
		path, e := a.assetPath(asset)
		if e != nil {
			return v, errors.New("发布素材路径无效或文件缺失，请重新准备")
		}
		f, e := os.Open(path)
		if e != nil {
			return v, errors.New("无法读取发布素材")
		}
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			f.Close()
			return v, errors.New("发布素材必须是普通文件")
		}
		maxSize := int64(20 << 20)
		if asset.Kind == "video" {
			maxSize = 1 << 30
		}
		if info.Size() <= 0 || info.Size() > maxSize {
			f.Close()
			return v, errors.New("图片需小于20MiB，视频需小于1GiB，且文件不能为空")
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return v, errors.New("无法校验发布素材")
		}
		sha := hex.EncodeToString(h.Sum(nil))
		if asset.SHA256 != "" && sha != asset.SHA256 {
			return v, errors.New("发布素材文件已改变，请重新上传并确认")
		}
		row := xhsPublishAsset{ID: id, Kind: asset.Kind, MIME: asset.MIME, Status: "ready", Position: len(v.Assets), Size: info.Size(), SHA256: sha, Path: path}
		if asset.Kind == "image" || asset.Kind == "cover" {
			f, e = os.Open(path)
			if e != nil {
				return v, errors.New("无法读取图片")
			}
			config, format, e := image.DecodeConfig(f)
			f.Close()
			if e != nil || (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 {
				return v, errors.New("首版直接发布支持有效的 JPEG 或 PNG 图片")
			}
			row.Width = config.Width
			row.Height = config.Height
			row.MIME = "image/" + format
			if v.MediaType == "video" {
				row.Kind = "cover"
			}
			if row.Kind == "cover" {
				covers++
			}
		} else {
			videos++
			if e = a.validateXHSVideo(ctx, path, &row); e != nil {
				return v, e
			}
		}
		v.Assets = append(v.Assets, row)
	}
	if v.MediaType == "image" && (len(v.Assets) < 1 || len(v.Assets) > 18) {
		return v, errors.New("首版支持每条图文1至18张图片")
	}
	if v.MediaType == "video" {
		if videos != 1 || covers > 1 || len(v.Assets) > 2 {
			return v, errors.New("视频发布请选择一个视频，可选一张图片作封面，请取消其他素材")
		}
		if covers == 0 && p.VideoProjectID == "" {
			cover, e := a.prepareXHSVideoCover(ctx, p, v.Assets[0])
			if e != nil {
				return v, e
			}
			v.Assets = append(v.Assets, cover)
			covers++
		}
		if videos != 1 || covers != 1 || len(v.Assets) != 2 {
			return v, errors.New("视频发布需要一个 MP4 视频及一个 JPEG 或 PNG 封面")
		}
		if p.VideoProjectID == "" {
			return v, nil
		}
		var project domain.VideoProject
		if e := a.Store.Get("videos", p.VideoProjectID, &project); e != nil || project.RewriteID != p.RewriteID {
			return v, errors.New("视频与确认版本不一致")
		}
		matched := false
		for _, r := range project.Renders {
			if r.Revision == p.VideoRevision && has(p.AssetIDs, r.AssetID) && has(p.AssetIDs, r.CoverAssetID) {
				matched = true
			}
		}
		if !matched {
			return v, errors.New("发布素材未绑定已合成的视频版本")
		}
	}
	return v, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a *App) validateXHSVideo(ctx context.Context, path string, row *xhsPublishAsset) error {
	s, e := a.settings()
	if e != nil {
		return errors.New("无法读取本机媒体检查配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := tooling.Command(ctx, a.Root, s.FFprobePath, "ffprobe", "-v", "error", "-show_entries", "format=duration,format_name:stream=codec_type,codec_name,width,height,duration", "-of", "json", path)
	data, e := cmd.Output()
	if e != nil {
		return errors.New("视频检查失败，请检查 FFprobe 和视频格式")
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
			Name     string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Type   string `json:"codec_type"`
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &probe) != nil || !strings.Contains(probe.Format.Name, "mp4") {
		return errors.New("首版直接发布仅支持 MP4 视频")
	}
	var duration float64
	if _, e = fmt.Sscan(probe.Format.Duration, &duration); e != nil || duration <= 0 || duration > 900 {
		return errors.New("首版支持15分钟以内的有效视频")
	}
	found := false
	for _, s := range probe.Streams {
		if s.Type == "video" && (s.Codec == "h264" || s.Codec == "hevc") && s.Width > 0 && s.Height > 0 {
			row.Width = s.Width
			row.Height = s.Height
			found = true
			break
		}
	}
	if !found {
		return errors.New("视频需要 H.264 或 H.265 视频轨道")
	}
	row.Duration = duration
	row.MIME = "video/mp4"
	return nil
}

func (a *App) previewXHSPublish(c *gin.Context) {
	var body struct {
		Visibility string `json:"visibility"`
	}
	if !bind(c, &body) {
		return
	}
	if body.Visibility == "" {
		body.Visibility = "public"
	}
	x := a.xhs
	x.op.Lock()
	defer x.op.Unlock()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	a.restoreXHSAccount(ctx)
	account, e := a.checkXHSAccount(ctx)
	if e != nil {
		fail(c, 409, "请先通过工作台扫码登录有效的小红书账号")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var p domain.Publication
	if !a.get(c, "publications", c.Param("id"), &p) {
		return
	}
	preview, e := a.validateXHSPublication(ctx, p, account, body.Visibility)
	if e != nil {
		fail(c, 409, e.Error())
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for token, v := range x.tokens {
		if !x.clock().Before(v.ExpiresAt) {
			delete(x.tokens, token)
		}
	}
	if len(x.tokens) > 100 {
		fail(c, 429, "待确认的预览过多，请稍后重试")
		return
	}
	token := ID()
	expiry := x.clock().Add(5 * time.Minute)
	x.tokens[token] = xhsPublishToken{PublicationID: p.ID, Fingerprint: xhsFingerprint(preview), AccountID: account.ID, Generation: x.generation, Visibility: body.Visibility, ExpiresAt: expiry}
	preview.ConfirmationToken = token
	preview.ExpiresAt = expiry.UTC().Format(time.RFC3339)
	c.JSON(200, preview)
}

func (a *App) submitXHSPublish(c *gin.Context) {
	var body struct {
		Token string `json:"confirmation_token"`
	}
	if !bind(c, &body) {
		return
	}
	x := a.xhs
	x.op.Lock()
	defer x.op.Unlock()
	x.mu.Lock()
	token, ok := x.tokens[body.Token]
	expired := !ok || !x.clock().Before(token.ExpiresAt)
	generation := x.generation
	// Consume even a stale confirmation: a changed preview needs a new review.
	delete(x.tokens, body.Token)
	x.mu.Unlock()
	if expired || token.PublicationID != c.Param("id") || token.Generation != generation {
		fail(c, 409, "发布确认已失效，请重新预览并确认")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	account, e := a.checkXHSAccount(ctx)
	if e != nil || account.ID != token.AccountID {
		fail(c, 409, "登录账号已改变或失效，请重新预览")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var p domain.Publication
	if !a.get(c, "publications", c.Param("id"), &p) {
		return
	}
	preview, e := a.validateXHSPublication(ctx, p, account, token.Visibility)
	if e != nil {
		fail(c, 409, e.Error())
		return
	}
	if xhsFingerprint(preview) != token.Fingerprint {
		fail(c, 409, "发布内容或素材已改变，请重新预览确认")
		return
	}
	payload, _ := json.Marshal(xhsPublishPayload{Preview: preview, Fingerprint: token.Fingerprint})
	j := domain.Job{ID: ID(), Kind: "publish", EntityID: p.ID, State: "queued", Phase: "queued", CreatedAt: now(), UpdatedAt: now(), Payload: payload, SourceVersionID: p.VersionID}
	p.Status = "publishing"
	p.PublishJobID = j.ID
	p.PublishError = ""
	p.PublishAccountID = account.ID
	p.PublishAccountName = account.Nickname
	p.PublishMethod = "qr_account"
	p.Visibility = token.Visibility
	if e = a.Store.CreatePublishJob(p, j); e != nil {
		if errors.Is(e, store.ErrConflict) {
			fail(c, 409, "发布稿已提交，请等待或核实已有任务")
		} else {
			fail(c, 500, "保存发布任务失败")
		}
		return
	}
	c.JSON(202, gin.H{"job_id": j.ID})
}

func (a *App) publishXHSJob(ctx context.Context, j *domain.Job) error {
	x := a.xhs
	x.op.Lock()
	defer x.op.Unlock()
	var payload xhsPublishPayload
	if json.Unmarshal(j.Payload, &payload) != nil || payload.Preview.Account == nil {
		return errors.New("发布快照无效，请重新预览确认")
	}
	a.restoreXHSAccount(ctx)
	account, e := a.checkXHSAccount(ctx)
	if e != nil || account.ID != payload.Preview.Account.ID {
		return errors.New("发布前账号已失效或改变，请重新扫码并预览确认")
	}
	a.mu.Lock()
	var p domain.Publication
	e = a.Store.Get("publications", j.EntityID, &p)
	if e != nil || p.Status != "publishing" || p.PublishJobID != j.ID {
		a.mu.Unlock()
		return errors.New("发布稿状态已改变，已停止提交")
	}
	// Revalidate the immutable snapshot, without treating this job's own
	// publishing status as a second submission.
	p.Status = "ready"
	preview, e := a.validateXHSPublication(ctx, p, account, payload.Preview.Visibility)
	a.mu.Unlock()
	if e != nil {
		return e
	}
	if xhsFingerprint(preview) != payload.Fingerprint {
		return errors.New("发布队列中的内容或素材已改变，请重新预览确认")
	}
	type rpcAsset struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	assets := []rpcAsset{}
	for _, asset := range preview.Assets {
		assets = append(assets, rpcAsset{ID: asset.ID, Kind: asset.Kind, Path: asset.Path, SHA256: asset.SHA256})
	}
	x.mu.Lock()
	session := x.session
	x.mu.Unlock()
	if session == "" {
		return errors.New("本机账号尚未登录")
	}
	j.Phase = "submitting"
	j.Progress = 30
	j.Message = "正在向小红书提交已确认的发布稿"
	j.UpdatedAt = now()
	if e = a.Store.Put("jobs", j.ID, j); e != nil {
		return errors.New("无法保存发布提交状态，已停止请求")
	}
	var result struct {
		NoteID      string      `json:"note_id"`
		URL         string      `json:"url"`
		PublishedAt string      `json:"published_at"`
		Account     *xhsAccount `json:"account"`
		Visibility  string      `json:"visibility"`
	}
	params := map[string]any{"session": session, "data_root": a.Data, "expected_account_id": account.ID, "publication": map[string]any{"id": p.ID, "title": preview.Title, "body": preview.Body, "topics": preview.Topics, "media_type": preview.MediaType, "visibility": preview.Visibility, "assets": assets}}
	if e = x.rpc.Call(ctx, "publish", params, &result); e != nil {
		var bridge *providers.XHSPublisherError
		if errors.As(e, &bridge) && !bridge.SubmissionUnknown {
			return errors.New(xhsErrorMessage(e))
		}
		return errXHSPublishUnknown
	}
	if !xhsNoteIDPattern.MatchString(result.NoteID) {
		return errXHSPublishUnknown
	}
	if result.Account != nil && result.Account.ID != account.ID {
		return errXHSPublishUnknown
	}
	if result.Visibility != "" && result.Visibility != preview.Visibility {
		return errXHSPublishUnknown
	}
	publishedAt := result.PublishedAt
	if _, e = time.Parse(time.RFC3339, publishedAt); e != nil {
		publishedAt = now()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e = a.Store.Get("publications", p.ID, &p); e != nil {
		return errXHSPublishUnknown
	}
	p.Status = "published"
	p.PublishError = ""
	p.XHSNoteID = strings.ToLower(result.NoteID)
	p.PlatformWorkID = p.XHSNoteID
	p.TargetPlatform = "xiaohongshu"
	p.SubmittedAt = publishedAt
	p.PublishedURL = "https://www.xiaohongshu.com/explore/" + p.XHSNoteID
	p.PublishedAt = publishedAt
	if e = a.Store.Put("publications", p.ID, p); e != nil {
		return errXHSPublishUnknown
	}
	j.RemoteTaskID = p.XHSNoteID
	return nil
}

func (a *App) publishEntityState(j domain.Job) {
	var p domain.Publication
	if a.Store.Get("publications", j.EntityID, &p) != nil || p.PublishJobID != j.ID || p.Status == "published" || p.PlatformWorkID != "" {
		return
	}
	switch j.State {
	case "submission_unknown":
		p.Status = "submission_unknown"
		p.PublishError = j.Error
	case "failed", "waiting_config", "cancelled":
		p.Status = "publish_failed"
		p.PublishError = j.Error
	case "queued":
		p.Status = "publishing"
	}
	a.Store.Put("publications", p.ID, p)
}

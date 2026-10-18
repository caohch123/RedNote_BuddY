package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

const videoRewriteWorkflow = "video_rewrite_v2"
const videoRewriteProvider = "bailian_wan3"
const videoRewritePromptRules = 2

var videoPurposeKeys = []string{"content", "subject", "action", "camera", "style", "audio"}
var videoRewriteRatios = []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}

type videoRewriteJobPayload struct {
	Revision     int                            `json:"revision"`
	SubmissionID string                         `json:"submission_id,omitempty"`
	Snapshot     domain.VideoGenerationSnapshot `json:"snapshot"`
}

func newVideoRewriteState(primary string) *domain.VideoRewriteState {
	s := &domain.VideoRewriteState{PrimaryAssetID: primary, VideoURLs: map[string]string{}, PurposeSources: map[string]string{}, Output: domain.VideoOutput{Duration: 10, Resolution: "720p", Ratio: "9:16", GenerateAudio: true}, Analyses: []domain.VideoMaterialAnalysis{}, Script: []domain.VideoScriptSegment{}, Submissions: []domain.VideoSubmission{}, Results: []domain.VideoRewriteResult{}, InitializationComplete: true}
	if primary != "" {
		for _, key := range []string{"content", "subject", "action"} {
			s.PurposeSources[key] = primary
		}
	}
	return s
}

// Source-linked schemes keep the note's media order even after edits or polling.
// Manual schemes follow in creation order; legacy project order is unchanged.
func sortVideoRewriteProjects(projects []domain.VideoProject, assets []domain.Asset) {
	positions := map[string]int{}
	for _, asset := range assets {
		positions[asset.ID] = asset.Position
	}
	rank := func(project domain.VideoProject) int {
		if project.VideoRewrite != nil {
			if position, ok := positions[project.VideoRewrite.InitialSourceAssetID]; ok {
				return position
			}
		}
		return int(^uint(0) >> 1)
	}
	sort.SliceStable(projects, func(i, j int) bool {
		vi, vj := projects[i].Workflow == videoRewriteWorkflow, projects[j].Workflow == videoRewriteWorkflow
		if vi != vj {
			return vi
		}
		if !vi {
			return false
		}
		if pi, pj := rank(projects[i]), rank(projects[j]); pi != pj {
			return pi < pj
		}
		if projects[i].CreatedAt != projects[j].CreatedAt {
			return projects[i].CreatedAt < projects[j].CreatedAt
		}
		return projects[i].ID < projects[j].ID
	})
}

// Called with a.mu held; initialization never edits an existing user's draft.
func (a *App) initializeVideoRewritesLocked(r domain.Rewrite, n domain.Note) error {
	projects, err := readList[domain.VideoProject](a, "videos")
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	updates := []domain.VideoProject{}
	count := 0
	for _, v := range projects {
		if v.RewriteID != r.ID || v.Workflow != videoRewriteWorkflow || v.VideoRewrite == nil {
			continue
		}
		count++
		if v.VideoRewrite.InitialSourceAssetID != "" {
			existing[v.VideoRewrite.InitialSourceAssetID] = true
		}
		if v.Status == "waiting_material" {
			var asset domain.Asset
			if a.Store.Get("assets", v.VideoRewrite.PrimaryAssetID, &asset) == nil && asset.Status == "ready" {
				v.Status = "draft"
				v.UpdatedAt = now()
				updates = append(updates, v)
			}
		}
	}
	assets := []domain.Asset{}
	for _, id := range n.AssetIDs {
		var asset domain.Asset
		if a.Store.Get("assets", id, &asset) == nil && asset.Kind == "video" {
			assets = append(assets, asset)
		}
	}
	sort.SliceStable(assets, func(i, j int) bool { return assets[i].Position < assets[j].Position })
	versionID := ""
	if len(r.Versions) > 0 {
		versionID = r.Versions[0].ID
	}
	for _, asset := range assets {
		if existing[asset.ID] {
			continue
		}
		v := domain.VideoProject{ID: ID(), RewriteID: r.ID, VersionID: versionID, Title: n.Title, Workflow: videoRewriteWorkflow, Revision: 1, Scenes: []domain.Scene{}, Renders: []domain.RenderVersion{}, Status: "draft", VideoRewrite: newVideoRewriteState(asset.ID), CreatedAt: now(), UpdatedAt: now()}
		v.VideoRewrite.InitialSourceAssetID = asset.ID
		if asset.Status != "ready" {
			v.Status = "waiting_material"
		}
		updates = append(updates, v)
		count++
	}
	if count == 0 {
		v := domain.VideoProject{ID: ID(), RewriteID: r.ID, VersionID: versionID, Title: n.Title, Workflow: videoRewriteWorkflow, Revision: 1, Scenes: []domain.Scene{}, Renders: []domain.RenderVersion{}, Status: "draft", VideoRewrite: newVideoRewriteState(""), CreatedAt: now(), UpdatedAt: now()}
		updates = append(updates, v)
	}
	return a.Store.Atomic(func(tx *store.Transaction) error {
		if err := tx.Put("rewrites", r.ID, r); err != nil {
			return err
		}
		for _, v := range updates {
			if err := tx.Put("videos", v.ID, v); err != nil {
				return err
			}
		}
		return nil
	})
}

func videoProviderConfigured(p domain.ProviderConfig) bool {
	return p.BaseURL != "" && p.Model != "" && p.APIKey != ""
}
func videoRewriteSupported(p domain.ProviderConfig) bool {
	if !providers.WanVideoModel(p.Model) {
		return false
	}
	if p.BaseURL == "" {
		return true
	}
	_, err := providers.WanVideoBase(p)
	return err == nil
}
func videoRewriteModelSignature(s domain.Settings) string {
	return digestImageValue([]any{videoRewritePromptRules, imageConfigSignature(s.Providers["video"]), imageConfigSignature(s.Providers["text"]), imageConfigSignature(s.Providers["vision"]), imageConfigSignature(s.Providers["asr"])})
}

func (a *App) routesVideoRewrites(g *gin.RouterGroup) {
	g.GET("/video-capabilities", func(c *gin.Context) {
		s, e := a.settings()
		if e != nil {
			fail(c, 500, "读取模型设置失败")
			return
		}
		c.JSON(200, gin.H{"video_configured": videoProviderConfigured(s.Providers["video"]), "text_configured": videoProviderConfigured(s.Providers["text"]), "vision_configured": videoProviderConfigured(s.Providers["vision"]), "asr_configured": videoProviderConfigured(s.Providers["asr"]), "supported": videoRewriteSupported(s.Providers["video"]), "min_duration": 2, "max_duration": 30, "resolutions": []string{"480p", "720p", "1080p"}, "ratios": videoRewriteRatios, "rules_version": videoRewritePromptRules, "provider": videoRewriteProvider, "models": []string{"wan3.0-video", "wan3.0-video-prime"}, "max_reference_video_duration": 15, "max_reference_video_total": 15, "max_total_video_duration": 30, "max_video_mb": 100, "max_image_mb": 20})
	})
	g.POST("/rewrites/:id/videos/initialize", func(c *gin.Context) {
		a.mu.Lock()
		defer a.mu.Unlock()
		var r domain.Rewrite
		if !a.get(c, "rewrites", c.Param("id"), &r) {
			return
		}
		var n domain.Note
		if !a.getRewriteSource(c, r, &n) {
			return
		}
		if e := a.initializeVideoRewritesLocked(r, n); e != nil {
			fail(c, 500, "初始化视频仿写失败")
			return
		}
		all, e := readList[domain.VideoProject](a, "videos")
		if e != nil {
			fail(c, 500, "读取视频仿写失败")
			return
		}
		out := []domain.VideoProject{}
		for _, v := range all {
			if v.RewriteID == r.ID && v.Workflow == videoRewriteWorkflow {
				out = append(out, publicVideoProject(v))
			}
		}
		sortVideoRewriteProjects(out, a.assetsByIDs(n.AssetIDs))
		c.JSON(200, out)
	})
	g.POST("/video-projects/:id/compose-script", a.composeVideoRewrite)
	g.POST("/video-projects/:id/apply", a.applyVideoRewrite)
	g.POST("/video-projects/:id/resume", a.resumeVideoRewrite)
}

func validateVideoOutput(o domain.VideoOutput) error {
	return providers.ValidateVideoRewriteOutput(o)
}
func videoRewriteIDs(s *domain.VideoRewriteState) []string {
	out := []string{}
	for _, id := range []string{s.PrimaryAssetID, s.ReferenceVideoID, s.ReferenceImageID} {
		if id == "" {
			continue
		}
		selected := false
		for _, source := range s.PurposeSources {
			selected = selected || source == id
		}
		if selected && !has(out, id) {
			out = append(out, id)
		}
	}
	return out
}
func (a *App) validateVideoRewriteState(rewriteID string, s *domain.VideoRewriteState) error {
	if s == nil {
		return errors.New("视频方案为空")
	}
	if e := validateVideoOutput(s.Output); e != nil {
		return e
	}
	if len([]rune(s.Requirements)) > 1000 {
		return errors.New("补充要求最多 1000 字")
	}
	seen := map[string]bool{}
	for i, id := range []string{s.PrimaryAssetID, s.ReferenceVideoID, s.ReferenceImageID} {
		if id == "" {
			continue
		}
		if seen[id] {
			return errors.New("素材不能重复选择")
		}
		seen[id] = true
		kind := "video"
		if i == 2 {
			kind = "image"
		}
		if _, e := a.validateVideoRewriteMaterial(rewriteID, id, kind, has(videoRewriteIDs(s), id)); e != nil {
			return e
		}
	}
	for key, id := range s.PurposeSources {
		if !has(videoPurposeKeys, key) {
			return errors.New("参考用途无效")
		}
		if id == "" {
			delete(s.PurposeSources, key)
			continue
		}
		if !seen[id] {
			return errors.New("参考用途指向未选择的素材")
		}
		if id == s.ReferenceImageID && key != "subject" && key != "style" {
			return errors.New("参考图片只能提供主体形象或视觉风格")
		}
	}
	if len(videoRewriteIDs(s)) == 0 && strings.TrimSpace(s.Requirements) == "" {
		return errors.New("没有参与创作的素材时，请填写补充要求")
	}
	return nil
}
func (a *App) videoRewriteSnapshot(v domain.VideoProject, s domain.Settings) (domain.VideoGenerationSnapshot, error) {
	w := v.VideoRewrite
	if e := a.validateVideoRewriteState(v.RewriteID, w); e != nil {
		return domain.VideoGenerationSnapshot{}, e
	}
	ids := videoRewriteIDs(w)
	hashes := map[string]string{}
	extractions := map[string]string{}
	urls := map[string]string{}
	total := 0.0
	for _, id := range ids {
		kind := "video"
		if id == w.ReferenceImageID {
			kind = "image"
		}
		asset, e := a.validateVideoRewriteMaterial(v.RewriteID, id, kind)
		if e != nil {
			return domain.VideoGenerationSnapshot{}, e
		}
		path, e := a.assetPath(asset)
		if e != nil {
			return domain.VideoGenerationSnapshot{}, e
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return domain.VideoGenerationSnapshot{}, e
		}
		hashes[id] = digestVideoBytes(data)
		if asset.Extraction.Edited || asset.Extraction.Status == "succeeded" {
			extractions[id] = digestImageValue([]any{asset.Extraction.Edited, strings.TrimSpace(asset.Extraction.Text), asset.Extraction.Segments})
		}
		if kind == "video" {
			total += asset.Duration
			// Keep the media-type marker for immutable numbering, never user URLs.
			urls[id] = ""
		}
	}
	if total > 15.001 {
		return domain.VideoGenerationSnapshot{}, errors.New("两条参考视频合计不能超过 15 秒，请更换短视频")
	}
	if total+float64(w.Output.Duration) > 30.001 {
		return domain.VideoGenerationSnapshot{}, errors.New("输入参考视频与输出视频时长合计不能超过 30 秒，请调低生成时长或更换短视频")
	}
	snap := domain.VideoGenerationSnapshot{Provider: videoRewriteProvider, PrimaryAssetID: w.PrimaryAssetID, InputAssetIDs: ids, InputHashes: hashes, VideoURLs: urls, PurposeSources: copyVideoSources(w.PurposeSources), Requirements: strings.TrimSpace(w.Requirements), Output: w.Output, Script: append([]domain.VideoScriptSegment{}, w.Script...), ModelSignature: videoRewriteModelSignature(s), Model: s.Providers["video"].Model, Revision: v.Revision}
	snap.Analyses = append([]domain.VideoMaterialAnalysis{}, w.Analyses...)
	snap.InputDigest = digestImageValue([]any{videoRewritePromptRules, snap.Provider, snap.PrimaryAssetID, ids, hashes, extractions, snap.PurposeSources, snap.Requirements, snap.Output, snap.ModelSignature})
	snap.Prompt = w.Prompt
	return snap, nil
}
func copyVideoSources(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if v != "" {
			out[k] = v
		}
	}
	return out
}
func digestVideoBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *App) createVideoRewrite(c *gin.Context, rewriteID, versionID, primary string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var r domain.Rewrite
	if !a.get(c, "rewrites", rewriteID, &r) {
		return
	}
	if versionID == "" && len(r.Versions) > 0 {
		versionID = r.Versions[0].ID
	}
	if _, ok := getVersion(r, versionID); !ok {
		fail(c, 404, "仿写版本不存在")
		return
	}
	state := newVideoRewriteState(primary)
	if primary != "" {
		if _, e := a.validateVideoRewriteMaterial(r.ID, primary, "video"); e != nil {
			fail(c, 400, e.Error())
			return
		}
	}
	v := domain.VideoProject{ID: ID(), RewriteID: r.ID, VersionID: versionID, Title: r.Title, Workflow: videoRewriteWorkflow, Revision: 1, Scenes: []domain.Scene{}, Renders: []domain.RenderVersion{}, Status: "draft", VideoRewrite: state, CreatedAt: now(), UpdatedAt: now()}
	if a.put(c, "videos", v.ID, v) {
		c.JSON(201, publicVideoProject(v))
	}
}

func (a *App) editVideoRewrite(c *gin.Context) {
	var b struct {
		Revision     int `json:"revision"`
		VideoRewrite struct {
			PrimaryAssetID   string             `json:"primary_asset_id"`
			ReferenceVideoID string             `json:"reference_video_id"`
			ReferenceImageID string             `json:"reference_image_id"`
			VideoURLs        map[string]string  `json:"video_urls"`
			PurposeSources   map[string]string  `json:"purpose_sources"`
			Requirements     string             `json:"requirements"`
			Output           domain.VideoOutput `json:"output"`
			Prompt           *string            `json:"prompt"`
		} `json:"video_rewrite"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var v domain.VideoProject
	if !a.get(c, "videos", c.Param("id"), &v) {
		return
	}
	if v.Workflow != videoRewriteWorkflow || v.VideoRewrite == nil {
		fail(c, 409, "视频工作流不匹配")
		return
	}
	if v.Revision != b.Revision {
		fail(c, 409, "方案已更新，请刷新后重试")
		return
	}
	if a.videoBusy(v.ID) {
		fail(c, 409, "视频任务正在执行，请等待完成")
		return
	}
	old := v.VideoRewrite
	next := *old
	next.PrimaryAssetID = b.VideoRewrite.PrimaryAssetID
	next.ReferenceVideoID = b.VideoRewrite.ReferenceVideoID
	next.ReferenceImageID = b.VideoRewrite.ReferenceImageID
	next.VideoURLs = map[string]string{} // Legacy public links no longer participate.
	next.PurposeSources = copyVideoSources(b.VideoRewrite.PurposeSources)
	next.Requirements = strings.TrimSpace(b.VideoRewrite.Requirements)
	next.Output = b.VideoRewrite.Output
	// Empty drafts remain saveable; full material/requirements validation occurs on compose.
	if e := validateVideoOutput(next.Output); e != nil {
		fail(c, 400, e.Error())
		return
	}
	if len([]rune(next.Requirements)) > 1000 {
		fail(c, 400, "补充要求最多 1000 字")
		return
	}
	for key, id := range next.PurposeSources {
		if !has(videoPurposeKeys, key) || !has([]string{next.PrimaryAssetID, next.ReferenceVideoID, next.ReferenceImageID}, id) || id == next.ReferenceImageID && (key != "subject" && key != "style") {
			fail(c, 400, "参考用途与素材不匹配")
			return
		}
	}
	for i, id := range []string{next.PrimaryAssetID, next.ReferenceVideoID, next.ReferenceImageID} {
		if id == "" || id == old.PrimaryAssetID && v.Status == "waiting_material" {
			continue
		}
		kind := "video"
		if i == 2 {
			kind = "image"
		}
		if _, e := a.validateVideoRewriteMaterial(v.RewriteID, id, kind, has(videoRewriteIDs(&next), id)); e != nil {
			fail(c, 400, e.Error())
			return
		}
	}
	basis := func(w *domain.VideoRewriteState) string {
		return digestImageValue([]any{w.PrimaryAssetID, w.ReferenceVideoID, w.ReferenceImageID, w.PurposeSources, w.Requirements, w.Output})
	}
	if basis(old) != basis(&next) {
		next.ScriptInputDigest = ""
		next.ScriptModelSignature = ""
		next.ScriptRulesVersion = 0
	}
	v.VideoRewrite = &next
	v.Revision++
	v.UpdatedAt = now()
	v.Status = "draft"
	if b.VideoRewrite.Prompt != nil {
		if len([]rune(*b.VideoRewrite.Prompt)) > 20000 {
			fail(c, 400, "最终视频提示词最多 20000 字，请精简后保存")
			return
		}
		next.Prompt = *b.VideoRewrite.Prompt
	}
	if a.put(c, "videos", v.ID, v) {
		c.JSON(200, publicVideoProject(v))
	}
}

func validateVideoScript(segments []domain.VideoScriptSegment, o domain.VideoOutput) error {
	return providers.ValidateVideoRewriteScript(segments, o)
}

func canonicalVideoRewritePrompt(s domain.VideoGenerationSnapshot) string {
	labels := map[string]string{}
	videos, images := 0, 0
	for _, id := range s.InputAssetIDs {
		if _, ok := s.VideoURLs[id]; ok {
			videos++
			labels[id] = fmt.Sprintf("视频%d", videos)
		} else {
			images++
			labels[id] = fmt.Sprintf("图%d", images)
		}
	}
	names := map[string]string{"content": "内容结构", "subject": "主体形象", "action": "动作", "camera": "运镜节奏", "style": "视觉风格", "audio": "声音氛围"}
	parts := []string{fmt.Sprintf("生成一条 %d 秒、%s、%s 的动态视频。", s.Output.Duration, s.Output.Resolution, s.Output.Ratio), "严格按所选用途参考素材；仅参考视觉风格时不得复制该素材的人物或物体。未选择的用途由脚本设计。"}
	for _, key := range videoPurposeKeys {
		if id := s.PurposeSources[key]; id != "" {
			parts = append(parts, names[key]+"参考"+labels[id]+"。")
		}
	}
	if s.Requirements != "" {
		parts = append(parts, "用户补充要求（仅对相应图像和视频设计调整优先，其余依据保持）："+s.Requirements)
	}
	for _, seg := range s.Script {
		parts = append(parts, fmt.Sprintf("%d–%d 秒：画面：%s；动作：%s；运镜：%s；对白与声音：%s。", seg.Start, seg.End, seg.Visual, seg.Action, seg.Camera, seg.Audio))
	}
	if s.Output.GenerateAudio {
		parts = append(parts, "使用模型原生声音，声音与脚本、动作同步。")
	} else {
		parts = append(parts, "输出无声视频。")
	}
	return strings.Join(parts, "\n")
}

func (a *App) composeVideoRewrite(c *gin.Context) {
	var b struct {
		Revision  int    `json:"revision"`
		RequestID string `json:"request_id"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var v domain.VideoProject
	if !a.get(c, "videos", c.Param("id"), &v) {
		return
	}
	if v.Workflow != videoRewriteWorkflow || v.VideoRewrite == nil {
		fail(c, 409, "请打开新版视频仿写工作台")
		return
	}
	if v.Revision != b.Revision {
		fail(c, 409, "方案已更新，请刷新确认")
		return
	}
	if a.videoBusy(v.ID) {
		fail(c, 409, "视频任务正在执行")
		return
	}
	settings, e := a.settings()
	if e != nil {
		fail(c, 500, "读取模型设置失败")
		return
	}
	if !videoProviderConfigured(settings.Providers["text"]) {
		fail(c, 409, "脚本模型待配置")
		return
	}
	if len(videoRewriteIDs(v.VideoRewrite)) > 0 && !videoProviderConfigured(settings.Providers["vision"]) {
		fail(c, 409, "视觉分析模型待配置")
		return
	}
	snap, e := a.videoRewriteSnapshot(v, settings)
	if e != nil {
		fail(c, 400, e.Error())
		return
	}
	p := videoRewriteJobPayload{Revision: v.Revision, Snapshot: snap}
	j, e := a.newVideoRewriteJobLocked("video_script", v.ID, p, settings)
	if e != nil {
		fail(c, 500, "保存脚本任务失败")
		return
	}
	v.Status = "analyzing"
	v.UpdatedAt = now()
	if e = a.Store.Atomic(func(tx *store.Transaction) error {
		if e := tx.Put("videos", v.ID, v); e != nil {
			return e
		}
		return tx.Put("jobs", j.ID, j)
	}); e != nil {
		fail(c, 500, "保存脚本任务失败")
		return
	}
	c.JSON(202, gin.H{"job_id": j.ID})
}

func (a *App) newVideoRewriteJobLocked(kind, id string, p videoRewriteJobPayload, s domain.Settings) (domain.Job, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return domain.Job{}, e
	}
	configs := map[string]domain.ProviderConfig{}
	for _, key := range []string{"video", "text", "vision", "asr"} {
		cfg := s.Providers[key]
		cfg.APIKey = ""
		cfg.Configured = false
		cfg.KeyMask = ""
		configs[key] = cfg
	}
	return domain.Job{ID: ID(), Kind: kind, EntityID: id, State: "queued", Phase: "queued", Payload: b, ProviderSnapshot: configs, CreatedAt: now(), UpdatedAt: now()}, nil
}

func (a *App) generateVideoRewrite(c *gin.Context) {
	var b struct {
		Revision  int    `json:"revision"`
		RequestID string `json:"request_id"`
		Prompt    string `json:"prompt"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var v domain.VideoProject
	if !a.get(c, "videos", c.Param("id"), &v) {
		return
	}
	if v.VideoRewrite == nil || v.Workflow != videoRewriteWorkflow {
		fail(c, 409, "视频工作流不匹配")
		return
	}
	w := v.VideoRewrite
	if strings.TrimSpace(b.RequestID) == "" || len(b.RequestID) > 128 {
		fail(c, 400, "生成请求需要有效请求 ID")
		return
	}
	for _, sub := range w.Submissions {
		if sub.RequestID == b.RequestID {
			if sub.Snapshot.Revision != b.Revision || sub.Snapshot.Prompt != b.Prompt {
				fail(c, 409, "请求 ID 已用于不同的生成依据，请刷新确认")
				return
			}
			c.JSON(202, gin.H{"job_id": sub.JobID})
			return
		}
	}
	if v.Revision != b.Revision || a.videoBusy(v.ID) {
		fail(c, 409, "方案已变化或视频任务正在执行")
		return
	}
	for _, sub := range w.Submissions {
		if sub.State != "succeeded" && sub.State != "cancelled" {
			fail(c, 409, "请先在工作台恢复或结束上次生成任务，再提交新视频")
			return
		}
	}
	settings, e := a.settings()
	if e != nil {
		fail(c, 500, "读取模型设置失败")
		return
	}
	if !videoProviderConfigured(settings.Providers["video"]) {
		fail(c, 409, "视频模型待配置")
		return
	}
	if !videoRewriteSupported(settings.Providers["video"]) {
		fail(c, 409, "当前模型不支持此工作流，请配置百炼 wan3.0-video 或 wan3.0-video-prime")
		return
	}
	snap, e := a.videoRewriteSnapshot(v, settings)
	if e != nil {
		fail(c, 400, e.Error())
		return
	}
	if w.ScriptRulesVersion != videoRewritePromptRules || w.ScriptInputDigest == "" || snap.InputDigest != w.ScriptInputDigest || w.ScriptModelSignature != snap.ModelSignature {
		fail(c, 409, "素材或设置已变化，请更新脚本后确认")
		return
	}
	if strings.TrimSpace(snap.Prompt) == "" || len([]rune(snap.Prompt)) > 20000 {
		fail(c, 400, "请填写最终视频提示词，最多 20000 字")
		return
	}
	if b.Prompt != snap.Prompt {
		fail(c, 409, "实际提示词已变化，请刷新后确认")
		return
	}
	sub := domain.VideoSubmission{ID: ID(), RequestID: b.RequestID, State: "queued", Snapshot: snap, CreatedAt: now()}
	p := videoRewriteJobPayload{Revision: v.Revision, SubmissionID: sub.ID, Snapshot: snap}
	j, e := a.newVideoRewriteJobLocked("video_rewrite", v.ID, p, settings)
	if e != nil {
		fail(c, 500, "保存视频任务失败")
		return
	}
	sub.JobID = j.ID
	sub.ProviderSnapshot = j.ProviderSnapshot
	w.Submissions = append(w.Submissions, sub)
	v.Status = "generating"
	v.UpdatedAt = now()
	e = a.Store.Atomic(func(tx *store.Transaction) error {
		if e := tx.Put("videos", v.ID, v); e != nil {
			return e
		}
		return tx.Put("jobs", j.ID, j)
	})
	if e != nil {
		fail(c, 500, "保存视频任务失败")
		return
	}
	c.JSON(202, gin.H{"job_id": j.ID})
}

func (a *App) videoRewriteJob(ctx context.Context, j *domain.Job, client *providers.Client) error {
	var p videoRewriteJobPayload
	if e := json.Unmarshal(j.Payload, &p); e != nil {
		return e
	}
	var v domain.VideoProject
	if e := a.Store.Get("videos", j.EntityID, &v); e != nil {
		return e
	}
	if v.VideoRewrite == nil {
		return errors.New("视频仿写已删除")
	}
	if j.Kind == "video_script" {
		if p.Snapshot.Provider != videoRewriteProvider {
			return errors.New("视频供应商已更换，请重新编排并审阅百炼脚本")
		}
		if v.Revision != p.Revision {
			return errChanged
		}
		analyses := []domain.VideoMaterialAnalysis{}
		for i, id := range p.Snapshot.InputAssetIDs {
			a.progress(j, "analysis", 5+i*30/max(1, len(p.Snapshot.InputAssetIDs)), "分析视频与参考素材")
			analysis, e := a.analyzeVideoRewriteMaterial(ctx, j, client, id)
			if e != nil {
				return e
			}
			if analysis.SHA256 != p.Snapshot.InputHashes[id] {
				return errChanged
			}
			analyses = append(analyses, analysis)
			a.mu.Lock()
			var current domain.VideoProject
			if ctx.Err() == nil && !a.jobCleared(j.ID) && a.Store.Get("videos", v.ID, &current) == nil && current.VideoRewrite != nil && current.Revision == p.Revision {
				current.VideoRewrite.Analyses = append([]domain.VideoMaterialAnalysis{}, analyses...)
				_ = a.Store.Put("videos", current.ID, current)
			}
			a.mu.Unlock()
		}
		a.progress(j, "compose_script", 45, "编排视频脚本")
		segments, e := client.ComposeVideoRewriteScript(ctx, providers.VideoRewriteScriptInput{Materials: analyses, PurposeSources: p.Snapshot.PurposeSources, Requirements: p.Snapshot.Requirements, Output: p.Snapshot.Output})
		if e != nil {
			return e
		}
		if e = validateVideoScript(segments, p.Snapshot.Output); e != nil {
			return e
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if ctx.Err() != nil || a.jobCleared(j.ID) {
			return context.Canceled
		}
		var current domain.VideoProject
		if e = a.Store.Get("videos", v.ID, &current); e != nil {
			return e
		}
		if current.Revision != p.Revision || current.VideoRewrite == nil {
			return errChanged
		}
		current.VideoRewrite.Analyses = analyses
		settings, e := a.settings()
		if e != nil {
			return e
		}
		for key, config := range j.ProviderSnapshot {
			config.APIKey = settings.Providers[key].APIKey
			settings.Providers[key] = config
		}
		fresh, e := a.videoRewriteSnapshot(current, settings)
		if e != nil {
			return e
		}
		if digestImageValue(fresh.InputAssetIDs) != digestImageValue(p.Snapshot.InputAssetIDs) || digestImageValue(fresh.InputHashes) != digestImageValue(p.Snapshot.InputHashes) {
			return errChanged
		}
		for _, analysis := range analyses {
			var source domain.Asset
			if a.Store.Get("assets", analysis.AssetID, &source) != nil {
				return errChanged
			}
			if source.Extraction.Edited && strings.TrimSpace(source.Extraction.Text) != strings.TrimSpace(analysis.Transcript) {
				return errChanged
			}
		}
		p.Snapshot.InputDigest = fresh.InputDigest
		p.Snapshot.Analyses = analyses
		p.Snapshot.Script = segments
		current.VideoRewrite.Script = segments
		current.VideoRewrite.Prompt = canonicalVideoRewritePrompt(p.Snapshot)
		if len([]rune(current.VideoRewrite.Prompt)) > 20000 {
			return errors.New("最终视频提示词过长，请精简补充要求后重新编排")
		}
		current.VideoRewrite.ScriptInputDigest = p.Snapshot.InputDigest
		current.VideoRewrite.ScriptModelSignature = p.Snapshot.ModelSignature
		current.VideoRewrite.ScriptRulesVersion = videoRewritePromptRules
		current.VideoRewrite.Analyses = analyses
		current.Status = "script_ready"
		current.UpdatedAt = now()
		return a.Store.Put("videos", current.ID, current)
	}
	return a.runVideoRewriteGeneration(ctx, j, client, p)
}

func (a *App) mutateVideoSubmission(projectID, submissionID string, fn func(*domain.VideoProject, *domain.VideoSubmission) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var v domain.VideoProject
	if e := a.Store.Get("videos", projectID, &v); e != nil {
		return e
	}
	if v.VideoRewrite == nil {
		return errors.New("视频工作流不存在")
	}
	for i := range v.VideoRewrite.Submissions {
		if v.VideoRewrite.Submissions[i].ID == submissionID {
			if v.VideoRewrite.Submissions[i].State == "cancelled" {
				return context.Canceled
			}
			if e := fn(&v, &v.VideoRewrite.Submissions[i]); e != nil {
				return e
			}
			v.UpdatedAt = now()
			return a.Store.Put("videos", v.ID, v)
		}
	}
	return errors.New("视频提交记录不存在")
}

func (a *App) runVideoRewriteGeneration(ctx context.Context, j *domain.Job, client *providers.Client, p videoRewriteJobPayload) error {
	var v domain.VideoProject
	if e := a.Store.Get("videos", j.EntityID, &v); e != nil {
		return e
	}
	var submission domain.VideoSubmission
	for _, sub := range v.VideoRewrite.Submissions {
		if sub.ID == p.SubmissionID {
			submission = sub
		}
	}
	if submission.ID == "" {
		return errors.New("视频提交记录不存在")
	}
	if submission.State == "cancelled" {
		return errors.New("任务已取消")
	}
	if submission.RemoteTaskID != "" {
		j.RemoteTaskID = submission.RemoteTaskID
	}
	if p.Snapshot.Provider != videoRewriteProvider && submission.ResultURL == "" {
		return errors.New("原任务属于已替换的视频供应商，请到原控制台核实或下载结果；取消此任务后可用百炼重新编排")
	}
	if j.RemoteTaskID == "" && submission.ResultURL == "" {
		if submission.State == "submitting" || submission.State == "submission_unknown" {
			return providers.ErrSubmissionUnknown
		}
		a.progress(j, "prepare_sources", 5, "核对并上传本地参考素材")
		videos := []providers.VideoReference{}
		images := [][]byte{}
		for _, id := range p.Snapshot.InputAssetIDs {
			if _, ok := p.Snapshot.VideoURLs[id]; ok {
				asset, e := a.validateVideoRewriteMaterial(v.RewriteID, id, "video")
				if e != nil {
					return e
				}
				path, e := a.assetPath(asset)
				if e != nil {
					return e
				}
				data, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				if digestVideoBytes(data) != p.Snapshot.InputHashes[id] {
					return errChanged
				}
				upload := submission.ReferenceUploads[id]
				expires, _ := time.Parse(time.RFC3339, upload.ExpiresAt)
				if upload.URL == "" || !expires.After(time.Now().Add(5*time.Minute)) {
					a.progress(j, "upload_sources", 10, "上传选中的参考视频")
					ext := ".mp4"
					if asset.MIME == "video/quicktime" {
						ext = ".mov"
					}
					upload, e = client.UploadVideoRewriteReference(ctx, data, ext)
					if e != nil {
						return e
					}
					if e = a.mutateVideoSubmission(v.ID, p.SubmissionID, func(_ *domain.VideoProject, sub *domain.VideoSubmission) error {
						if sub.ReferenceUploads == nil {
							sub.ReferenceUploads = map[string]domain.VideoReferenceUpload{}
						}
						sub.ReferenceUploads[id] = upload
						return nil
					}); e != nil {
						return e
					}
				}
				videos = append(videos, providers.VideoReference{URL: upload.URL, Duration: asset.Duration})
			} else {
				asset, e := a.validateVideoRewriteMaterial(v.RewriteID, id, "image")
				if e != nil {
					return e
				}
				path, e := a.assetPath(asset)
				if e != nil {
					return e
				}
				data, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				if digestVideoBytes(data) != p.Snapshot.InputHashes[id] {
					return errChanged
				}
				images = append(images, data)
			}
		}
		if e := a.mutateVideoSubmission(v.ID, p.SubmissionID, func(_ *domain.VideoProject, sub *domain.VideoSubmission) error { sub.State = "submitting"; return nil }); e != nil {
			return e
		}
		a.progress(j, "submitting", 15, "提交视频生成，请勿重复提交")
		task, e := client.CreateVideoRewrite(ctx, providers.VideoRewriteRequest{Prompt: p.Snapshot.Prompt, Output: p.Snapshot.Output, Videos: videos, Images: images})
		if e != nil {
			return e
		}
		j.RemoteTaskID = task
		j.RemoteTaskIDs = append(j.RemoteTaskIDs, task)
		if e = a.Store.Put("jobs", j.ID, j); e != nil {
			return providers.ErrSubmissionUnknown
		}
		if e = a.mutateVideoSubmission(v.ID, p.SubmissionID, func(_ *domain.VideoProject, sub *domain.VideoSubmission) error {
			sub.RemoteTaskID = task
			sub.State = "polling"
			return nil
		}); e != nil {
			return providers.ErrSubmissionUnknown
		}
	}
	resultURL := submission.ResultURL
	if resultURL == "" {
		a.progress(j, "polling", 35, "等待视频生成，可关闭页面")
		for {
			result, e := client.PollVideo(ctx, j.RemoteTaskID)
			if result.State == "failed" {
				a.progress(j, "remote_failed", 40, "视频服务已明确返回失败")
				if e != nil {
					return e
				}
				return errors.New("视频服务已明确返回生成失败")
			}
			if e != nil {
				return e
			}
			if result.State == "succeeded" {
				if result.URL == "" {
					return errors.New("视频服务未返回结果地址")
				}
				resultURL = result.URL
				submission.ProviderResolution = result.Resolution
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(15 * time.Second):
			}
		}
		if e := a.mutateVideoSubmission(v.ID, p.SubmissionID, func(_ *domain.VideoProject, sub *domain.VideoSubmission) error {
			sub.ResultURL = resultURL
			sub.ProviderResolution = submission.ProviderResolution
			sub.State = "download"
			return nil
		}); e != nil {
			return e
		}
	}
	a.progress(j, "download", 80, "下载生成视频与播放副本")
	assetID, previewID, coverID, warning, e := a.downloadVideoRewriteResult(ctx, j, resultURL, p.Snapshot.Output, submission.ProviderResolution)
	if e != nil {
		return e
	}
	j.ResultAssetIDs = []string{assetID}
	if previewID != "" {
		j.ResultAssetIDs = append(j.ResultAssetIDs, previewID)
	}
	if coverID != "" {
		j.ResultAssetIDs = append(j.ResultAssetIDs, coverID)
	}
	return a.mutateVideoSubmission(v.ID, p.SubmissionID, func(v *domain.VideoProject, sub *domain.VideoSubmission) error {
		for _, result := range v.VideoRewrite.Results {
			if result.SubmissionID == sub.ID {
				sub.State = "succeeded"
				return nil
			}
		}
		v.VideoRewrite.Results = append(v.VideoRewrite.Results, domain.VideoRewriteResult{AssetID: assetID, PreviewAssetID: previewID, CoverAssetID: coverID, JobID: j.ID, SubmissionID: sub.ID, Snapshot: p.Snapshot, Warning: warning, CreatedAt: now()})
		sub.State = "succeeded"
		v.Status = "ready"
		return nil
	})
}

func (a *App) videoRewriteEntityState(j domain.Job) {
	var v domain.VideoProject
	if a.Store.Get("videos", j.EntityID, &v) != nil || v.VideoRewrite == nil {
		return
	}
	if j.Kind == "video_script" {
		if j.State != "succeeded" {
			v.Status = j.State
		}
	} else {
		var p videoRewriteJobPayload
		_ = json.Unmarshal(j.Payload, &p)
		for i := range v.VideoRewrite.Submissions {
			sub := &v.VideoRewrite.Submissions[i]
			if sub.ID == p.SubmissionID {
				sub.State = j.State
				if j.RemoteTaskID != "" {
					sub.RemoteTaskID = j.RemoteTaskID
				}
			}
		}
		v.Status = j.State
		if j.State == "succeeded" {
			v.Status = "ready"
		}
	}
	v.UpdatedAt = now()
	_ = a.Store.Put("videos", v.ID, v)
}

func (a *App) recoverVideoRewriteJob(j *domain.Job) {
	if j.Kind != "video_rewrite" {
		return
	}
	var p videoRewriteJobPayload
	if json.Unmarshal(j.Payload, &p) != nil {
		return
	}
	var v domain.VideoProject
	if a.Store.Get("videos", j.EntityID, &v) != nil || v.VideoRewrite == nil {
		return
	}
	for _, sub := range v.VideoRewrite.Submissions {
		if sub.ID != p.SubmissionID {
			continue
		}
		for _, result := range v.VideoRewrite.Results {
			if result.SubmissionID == sub.ID {
				j.State = "succeeded"
				j.Phase = "complete"
				j.Progress = 100
				j.Error = ""
				j.ResultAssetIDs = []string{result.AssetID}
				_ = a.Store.Put("jobs", j.ID, j)
				return
			}
		}
		if sub.State == "cancelled" {
			return
		}
		if sub.Snapshot.Provider != videoRewriteProvider && sub.ResultURL == "" && (j.State == "queued" || j.State == "submission_unknown") {
			j.State = "failed"
			j.Error = "原任务属于已替换的供应商，请在原控制台核实；不会自动向百炼重投"
			_ = a.Store.Put("jobs", j.ID, j)
			return
		}
		if (j.State == "submission_unknown" || j.State == "queued") && (sub.RemoteTaskID != "" || j.RemoteTaskID != "" || sub.ResultURL != "") {
			if sub.RemoteTaskID != "" {
				j.RemoteTaskID = sub.RemoteTaskID
			}
			j.State = "queued"
			j.Phase = "polling"
			j.Error = ""
			_ = a.Store.Put("jobs", j.ID, j)
		}
		if j.State == "queued" && sub.State == "submitting" && j.RemoteTaskID == "" && sub.ResultURL == "" {
			j.State = "submission_unknown"
			j.Error = "提交结果不明，请核实后恢复"
			_ = a.Store.Put("jobs", j.ID, j)
		}
	}
}

func (a *App) resumeVideoRewrite(c *gin.Context) {
	var b struct {
		SubmissionID    string `json:"submission_id"`
		Action          string `json:"action"`
		RemoteTaskID    string `json:"remote_task_id"`
		ConfirmNoResult bool   `json:"confirm_no_result"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var v domain.VideoProject
	if !a.get(c, "videos", c.Param("id"), &v) {
		return
	}
	if v.VideoRewrite == nil {
		fail(c, 409, "视频工作流不匹配")
		return
	}
	index := -1
	for i, sub := range v.VideoRewrite.Submissions {
		if sub.ID == b.SubmissionID {
			index = i
		}
	}
	if index < 0 {
		fail(c, 404, "提交记录不存在")
		return
	}
	sub := &v.VideoRewrite.Submissions[index]
	var j domain.Job
	if err := a.Store.Get("jobs", sub.JobID, &j); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			fail(c, 500, "读取视频任务失败")
			return
		}
		if b.Action == "cancel" {
			sub.State = "cancelled"
			v.Status = "draft"
			v.UpdatedAt = now()
			if a.put(c, "videos", v.ID, v) {
				c.JSON(202, gin.H{"job_id": ""})
			}
			return
		}
		settings, e := a.settings()
		if e != nil {
			fail(c, 500, "读取配置失败")
			return
		}
		j, e = a.newVideoRewriteJobLocked("video_rewrite", v.ID, videoRewriteJobPayload{Revision: sub.Snapshot.Revision, SubmissionID: sub.ID, Snapshot: sub.Snapshot}, settings)
		if e != nil {
			fail(c, 500, "恢复任务失败")
			return
		}
		if len(sub.ProviderSnapshot) > 0 {
			j.ProviderSnapshot = sub.ProviderSnapshot
		} else if videoRewriteModelSignature(settings) != sub.Snapshot.ModelSignature {
			fail(c, 409, "原任务配置已变化，请恢复原配置后再查询")
			return
		}
		j.State = sub.State
		j.RemoteTaskID = sub.RemoteTaskID
		sub.JobID = j.ID
	}
	if j.State == "running" || j.State == "queued" {
		fail(c, 409, "视频任务正在执行")
		return
	}
	if j.State == "succeeded" {
		fail(c, 409, "该视频已经生成")
		return
	}
	if b.Action != "cancel" && sub.Snapshot.Provider != videoRewriteProvider && sub.ResultURL == "" {
		fail(c, 409, "原供应商任务不能转移到百炼，请到原控制台核实；取消原任务后重新编排")
		return
	}
	switch b.Action {
	case "cancel":
		sub.State = "cancelled"
		j.State = "cancelled"
		j.Error = "用户取消恢复"
		v.Status = "draft"
	case "restore":
		if strings.TrimSpace(b.RemoteTaskID) != "" {
			sub.RemoteTaskID = strings.TrimSpace(b.RemoteTaskID)
			j.RemoteTaskID = sub.RemoteTaskID
		}
		if sub.RemoteTaskID == "" && j.RemoteTaskID == "" && sub.ResultURL == "" {
			fail(c, 409, "请先填写供应商任务 ID，再恢复查询")
			return
		}
		sub.State = "queued"
		j.State = "queued"
		j.Phase = "polling"
		j.Error = ""
	case "retry":
		if sub.Snapshot.Provider != videoRewriteProvider {
			fail(c, 409, "供应商已更换，请取消原任务并重新编排、确认百炼脚本")
			return
		}
		if j.State == "submission_unknown" && !b.ConfirmNoResult {
			fail(c, 409, "请先在供应商控制台确认没有生成结果")
			return
		}
		if sub.RemoteTaskID != "" && j.Phase != "remote_failed" {
			fail(c, 409, "已记录供应商任务，请恢复查询和下载")
			return
		}
		sub.RemoteTaskID = ""
		sub.ResultURL = ""
		j.RemoteTaskID = ""
		j.State = "queued"
		j.Phase = "queued"
		j.Error = ""
		sub.State = "queued"
	default:
		fail(c, 400, "恢复操作无效")
		return
	}
	v.UpdatedAt = now()
	j.UpdatedAt = now()
	e := a.Store.Atomic(func(tx *store.Transaction) error {
		if e := tx.Put("videos", v.ID, v); e != nil {
			return e
		}
		return tx.Put("jobs", j.ID, j)
	})
	if e != nil {
		fail(c, 500, "保存恢复状态失败")
		return
	}
	c.JSON(202, gin.H{"job_id": j.ID})
}

func (a *App) applyVideoRewrite(c *gin.Context) {
	var b struct {
		AssetID   string `json:"asset_id"`
		VersionID string `json:"version_id"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var project domain.VideoProject
	if !a.get(c, "videos", c.Param("id"), &project) {
		return
	}
	if project.VideoRewrite == nil {
		fail(c, 409, "视频工作流不匹配")
		return
	}
	var selected *domain.VideoRewriteResult
	for i := range project.VideoRewrite.Results {
		if project.VideoRewrite.Results[i].AssetID == b.AssetID {
			selected = &project.VideoRewrite.Results[i]
		}
	}
	if selected == nil {
		fail(c, 400, "请选择本项目已生成的视频")
		return
	}
	var asset domain.Asset
	if a.Store.Get("assets", b.AssetID, &asset) != nil || asset.Status != "ready" || asset.OwnerID != project.ID {
		fail(c, 400, "生成视频不可用")
		return
	}
	var r domain.Rewrite
	if !a.get(c, "rewrites", project.RewriteID, &r) {
		return
	}
	versionID := b.VersionID
	if versionID == "" {
		versionID = project.VersionID
	}
	v, ok := getVersion(r, versionID)
	if !ok {
		fail(c, 404, "目标仿写版本不存在")
		return
	}
	if has(v.AssetIDs, b.AssetID) {
		c.JSON(200, v)
		return
	}
	if previous, ok := getVersion(r, project.VideoRewrite.AppliedVersionID); ok && previous.ParentVersionID == v.ID && has(previous.AssetIDs, b.AssetID) {
		c.JSON(200, previous)
		return
	}
	primary := selected.Snapshot.PrimaryAssetID
	ids := append([]string{}, v.AssetIDs...)
	replaced := false
	for i, id := range ids {
		match := id == primary && primary != ""
		for _, result := range project.VideoRewrite.Results {
			match = match || (primary != "" && result.Snapshot.PrimaryAssetID == primary && result.AssetID == id)
		}
		if match {
			ids[i] = b.AssetID
			replaced = true
			break
		}
	}
	_ = replaced // A deselected source stays deselected; adopting must not add it back.
	if primary == "" {
		ids = append(ids, b.AssetID)
	}
	v.ParentVersionID = v.ID
	v.ID = ID()
	v.Number = len(r.Versions) + 1
	v.AssetIDs = ids
	v.Confirmed = false
	v.CreatedAt = now()
	r.Versions = append(r.Versions, v)
	r.UpdatedAt = now()
	project.VideoRewrite.AppliedVersionID = v.ID
	project.UpdatedAt = now()
	e := a.Store.Atomic(func(tx *store.Transaction) error {
		if e := tx.Put("rewrites", r.ID, r); e != nil {
			return e
		}
		return tx.Put("videos", project.ID, project)
	})
	if e != nil {
		fail(c, 500, "采用视频失败")
		return
	}
	c.JSON(201, v)
}

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

type ImageV2Create struct {
	WorkflowVersion int                     `json:"workflow_version"`
	PrimaryAssetID  string                  `json:"primary_asset_id"`
	References      []domain.ImageReference `json:"references"`
}
type imageV2Payload struct {
	WorkflowVersion  int                     `json:"workflow_version"`
	Revision         int                     `json:"revision"`
	AssetID          string                  `json:"asset_id,omitempty"`
	SuggestProposals bool                    `json:"suggest_proposals,omitempty"`
	InputDigest      string                  `json:"input_digest,omitempty"`
	PromptInput      map[string]any          `json:"prompt_input,omitempty"`
	BatchID          string                  `json:"batch_id,omitempty"`
	Index            int                     `json:"index"`
	Snapshot         domain.ImageBatch       `json:"snapshot,omitempty"`
	Receipt          *providers.ImageReceipt `json:"receipt,omitempty"`
}

// Changing the composition contract invalidates reviewed prompts without
// rewriting earlier results or their original generation snapshots.
const imagePromptRulesVersion = 6

// Legacy schemes keep their seven combined dimensions. The flexible workflow
// separates identity from action without rewriting saved generation history.
var imageFlexibleFeatureNames = []string{"subject", "action", "scene", "composition", "palette", "lighting", "texture", "text"}

func copyImageFeatureControls(controls map[string]domain.ImageFeatureControl) map[string]domain.ImageFeatureControl {
	if controls == nil {
		return nil
	}
	out := make(map[string]domain.ImageFeatureControl, len(controls))
	for key, control := range controls {
		out[key] = control
	}
	return out
}

func imageFeatureControlsSources(controls map[string]domain.ImageFeatureControl) map[string]string {
	out := map[string]string{}
	for key, control := range controls {
		if control.Mode == "image" && control.AssetID != "" {
			out[key] = control.AssetID
		}
	}
	return out
}

func effectiveImageFeatureControls(u domain.ImageRewrite) map[string]domain.ImageFeatureControl {
	if normalizeImageEditIntent(u.EditIntent).Mode != "flexible" {
		return nil
	}
	return copyImageFeatureControls(u.FeatureControls)
}

type imageAnalysisCache struct {
	Analysis domain.ImageAnalysis `json:"analysis"`
	TextRead bool                 `json:"text_read"`
}

func digestImageValue(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func imageConfigSignature(p domain.ProviderConfig) string {
	return digestImageValue([]string{p.BaseURL, p.Model})
}
func imageV2Size(o domain.ImageOutput) (int, int, error) {
	sizes := map[string][2]int{"3:4:standard": {960, 1280}, "3:4:high": {1536, 2048}, "1:1:standard": {1024, 1024}, "1:1:high": {2048, 2048}, "9:16:standard": {720, 1280}, "9:16:high": {1152, 2048}}
	s, ok := sizes[o.Ratio+":"+o.Clarity]
	if !ok || o.Count < 1 || o.Count > 4 {
		return 0, 0, errors.New("请选择支持的比例、清晰度及 1–4 张图片")
	}
	return s[0], s[1], nil
}
func imageV2IDs(u domain.ImageRewrite) []string {
	ids := []string{}
	if u.PrimaryAssetID != "" {
		ids = append(ids, u.PrimaryAssetID)
	}
	for _, r := range u.References {
		ids = append(ids, r.AssetID)
	}
	return ids
}

func copyImageFeatureSources(sources map[string]string) map[string]string {
	out := map[string]string{}
	for key, id := range sources {
		if id != "" {
			out[key] = id
		}
	}
	return out
}

func effectiveImageFeatureSources(u domain.ImageRewrite) map[string]string {
	intent := normalizeImageEditIntent(u.EditIntent)
	if intent.Mode == "flexible" {
		return imageFeatureControlsSources(u.FeatureControls)
	}
	sources := copyImageFeatureSources(u.FeatureSources)
	if intent.Mode == "preserve_content" {
		if intent.Style != "" {
			delete(sources, "texture")
		}
		if intent.Palette != "" {
			delete(sources, "palette")
		}
	}
	return sources
}

func imageV2SemanticDigest(u domain.ImageRewrite) string {
	// JSON storage omits empty references, while the editor sends an empty
	// array. Compare ordered image identities, not that representation or
	// display metadata, so a normal save cannot invalidate prompt review.
	references := make([]string, 0, len(u.References))
	for _, ref := range u.References {
		references = append(references, ref.AssetID)
	}
	if normalizeImageEditIntent(u.EditIntent).Mode == "flexible" {
		return digestImageValue([]any{u.PrimaryAssetID, references, u.FeatureControls, u.Output.Ratio, "flexible", u.SelectedProposalID, u.ExtraRequirements})
	}
	features := map[string]string{}
	for key, id := range u.FeatureSources {
		if id != "" {
			features[key] = id
		}
	}
	return digestImageValue([]any{u.PrimaryAssetID, references, features, u.Output.Ratio, normalizeImageEditIntent(u.EditIntent), u.SelectedProposalID, u.ExtraRequirements})
}

func normalizeImageEditIntent(intent domain.ImageEditIntent) domain.ImageEditIntent {
	intent.Mode = strings.TrimSpace(intent.Mode)
	if intent.Mode == "" {
		intent.Mode = "feature_mix"
	}
	intent.Style = strings.TrimSpace(intent.Style)
	intent.Palette = strings.TrimSpace(intent.Palette)
	return intent
}

var imagePreservedFeatures = []string{"subject", "scene", "composition", "text"}

func featureValue(f domain.ImageFeatures, k string) string {
	values := map[string]string{"subject": f.Subject, "action": f.Action, "scene": f.Scene, "composition": f.Composition, "palette": f.Palette, "lighting": f.Lighting, "texture": f.Texture, "text": f.Text}
	return values[k]
}

var quotedImageTypography = regexp.MustCompile("‘[^’]*’|“[^”]*”|「[^」]*」|『[^』]*』|\"[^\"]*\"|'[^']*'")

func imageTypographyOnly(description string, knownText ...string) string {
	// Older visual reports sometimes put literal OCR inside the typography
	// field despite its schema. Remove quoted content and known OCR lines
	// before passing the remaining font/alignment description to composition.
	description = quotedImageTypography.ReplaceAllString(description, "")
	for _, text := range knownText {
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				description = strings.ReplaceAll(description, line, "")
			}
		}
	}
	return strings.TrimSpace(description)
}
func imageV2Analysis(u domain.ImageRewrite, id string) (domain.ImageAnalysis, bool) {
	for _, r := range u.Analyses {
		if r.AssetID == id {
			return r, true
		}
	}
	return domain.ImageAnalysis{}, false
}
func (a *App) isImageV2(c *gin.Context) bool {
	var r domain.Rewrite
	if a.Store.Get("rewrites", c.Param("id"), &r) != nil {
		return false
	}
	u, _, ok := getImageRewrite(r, c.Param("image_id"))
	return ok && u.WorkflowVersion == 2
}
func (a *App) jobIsImageV2(j *domain.Job) bool {
	var p imageV2Payload
	return json.Unmarshal(j.Payload, &p) == nil && p.WorkflowVersion == 2
}
func (a *App) imageV2Busy(rewriteID, imageID string) bool {
	var r domain.Rewrite
	if a.Store.Get("rewrites", rewriteID, &r) != nil {
		return false
	}
	u, _, ok := getImageRewrite(r, imageID)
	if !ok || u.WorkflowVersion != 2 {
		return false
	}
	for _, b := range u.Batches {
		if b.State == "running" || b.State == "paused" || b.State == "submission_unknown" {
			return true
		}
	}
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		log.Printf("list jobs for image workflow state: %v", err)
		return true
	}
	for _, j := range jobs {
		if j.EntityID == rewriteID && j.SceneID == imageID && (j.Kind == "image_compose" || j.Kind == "image_analyze") && (j.State == "queued" || j.State == "running") {
			return true
		}
	}
	return false
}
func (a *App) imageV2Files(rewriteID string, u domain.ImageRewrite) ([]string, error) {
	ids := imageV2IDs(u)
	seen := map[string]bool{}
	hashes := []string{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, errors.New("图片不能重复选择")
		}
		seen[id] = true
		data, asset, err := a.readImageV2File(rewriteID, id)
		if err != nil {
			return nil, err
		}
		if id == u.PrimaryAssetID && asset.Kind == "style_reference" {
			return nil, errors.New("官方参考图不能作为被仿写图")
		}
		h := sha256.Sum256(data)
		hashes = append(hashes, hex.EncodeToString(h[:]))
	}
	return hashes, nil
}

// The legacy video helper permits reuse within a platform. V2 personal image
// inputs are narrower: this note, this rewrite, or a video owned by this rewrite.
func (a *App) readImageV2File(rewriteID, assetID string) ([]byte, domain.Asset, error) {
	var r domain.Rewrite
	var asset domain.Asset
	if a.Store.Get("rewrites", rewriteID, &r) != nil || a.Store.Get("assets", assetID, &asset) != nil {
		return nil, asset, errors.New("图片不属于当前项目")
	}
	allowed := asset.OwnerID == r.ID || asset.OwnerID == r.NoteID
	if !allowed {
		var video domain.VideoProject
		allowed = a.Store.Get("videos", asset.OwnerID, &video) == nil && video.RewriteID == r.ID
	}
	if !allowed {
		return nil, asset, errors.New("图片不属于当前项目")
	}
	return a.readVisualImage(rewriteID, assetID)
}
func validateImageV2(u domain.ImageRewrite) error {
	return validateImageV2Controls(u, false)
}

func validateImageV2Controls(u domain.ImageRewrite, allowIncomplete bool) error {
	intent := normalizeImageEditIntent(u.EditIntent)
	if intent.Mode != "feature_mix" && intent.Mode != "preserve_content" && intent.Mode != "flexible" {
		return errors.New("图片编辑方式不可用")
	}
	if intent.Mode == "flexible" {
		if intent.Style != "" || intent.Palette != "" {
			return errors.New("请在对应维度中指定画法和配色")
		}
		if len(u.FeatureControls) != len(imageFlexibleFeatureNames) {
			return errors.New("请完整选择八项画面要求")
		}
		for key, control := range u.FeatureControls {
			if !has(imageFlexibleFeatureNames, key) {
				return errors.New("画面要求包含未知维度")
			}
			switch control.Mode {
			case "image":
				if control.Value != "" || (control.AssetID == "" && !allowIncomplete) || (control.AssetID != "" && !has(imageV2IDs(u), control.AssetID)) {
					return errors.New("图片来源不属于当前方案，或包含冲突的自定义要求")
				}
			case "custom":
				if control.AssetID != "" || (strings.TrimSpace(control.Value) == "" && !allowIncomplete) || len([]rune(strings.TrimSpace(control.Value))) > 500 {
					return errors.New("自定义要求须为 1–500 字，且不能同时选择图片来源")
				}
			case "auto":
				if control.AssetID != "" || control.Value != "" {
					return errors.New("交给模型设计的维度不能保留图片来源或自定义要求")
				}
			default:
				return errors.New("请选择图片来源、自定义或交给模型设计")
			}
		}
	}
	if len([]rune(intent.Style)) > 80 || len([]rune(intent.Palette)) > 80 {
		return errors.New("画法和配色要求分别不能超过 80 字")
	}
	if intent.Mode == "feature_mix" && (intent.Style != "" || intent.Palette != "") {
		return errors.New("直接指定画法和配色时，请使用保留主图内容模式")
	}
	if intent.Mode == "preserve_content" && u.PrimaryAssetID != "" {
		for _, key := range imagePreservedFeatures {
			if u.FeatureSources[key] != u.PrimaryAssetID {
				return errors.New("保留主图内容时，主体、场景、布局和文字版式必须来自主图")
			}
		}
	}
	if len(u.References) > 2 {
		return errors.New("最多选择两张辅助参考图")
	}
	if _, _, err := imageV2Size(u.Output); err != nil {
		return err
	}
	if intent.Mode != "flexible" {
		for k, id := range u.FeatureSources {
			if !has(imageFeatureNames, k) || (id != "" && !has(imageV2IDs(u), id)) {
				return errors.New("特征来源不属于所选图片")
			}
		}
	}
	return nil
}
func (a *App) resolveImageV2References(rewriteID string, refs []domain.ImageReference) ([]domain.ImageReference, []domain.Asset, error) {
	if len(refs) > 2 {
		return nil, nil, errors.New("最多选择两张辅助参考图")
	}
	out := []domain.ImageReference{}
	created := []domain.Asset{}
	for _, ref := range refs {
		if ref.AssetID != "" {
			if _, _, err := a.readImageV2File(rewriteID, ref.AssetID); err != nil {
				return nil, created, err
			}
			ref.Title = ""
			var asset domain.Asset
			_ = a.Store.Get("assets", ref.AssetID, &asset)
			if strings.HasPrefix(asset.SourceID, "style-library:") {
				ref.LibraryID = strings.TrimPrefix(asset.SourceID, "style-library:")
			} else {
				ref.LibraryID = ""
			}
			out = append(out, ref)
			continue
		}
		if ref.LibraryID == "" {
			return nil, created, errors.New("辅助图片不可用")
		}
		asset, entry, err := a.snapshotStyle(rewriteID, ref.LibraryID)
		if err != nil {
			return nil, created, err
		}
		created = append(created, asset)
		out = append(out, domain.ImageReference{AssetID: asset.ID, LibraryID: ref.LibraryID, Title: entry.Title})
	}
	return out, created, nil
}
func (a *App) cleanupImageV2Refs(refs []domain.Asset) {
	for _, v := range refs {
		_ = a.Store.Delete("assets", v.ID)
		a.removeUnstoredUpload(filepath.Join(a.Data, v.LocalPath))
	}
}
func (a *App) createImageV2(c *gin.Context, b ImageV2Create, versionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.visualRewrite(c)
	if !ok {
		return
	}
	if _, ok := getVersion(r, versionID); !ok {
		fail(c, 404, "仿写版本不存在")
		return
	}
	if b.PrimaryAssetID == "" {
		fail(c, 400, "请选择被仿写图")
		return
	}
	if _, asset, err := a.readImageV2File(r.ID, b.PrimaryAssetID); err != nil || asset.Kind == "style_reference" {
		fail(c, 400, "被仿写图不可用")
		return
	}
	refs, created, err := a.resolveImageV2References(r.ID, b.References)
	if err != nil {
		a.cleanupImageV2Refs(created)
		fail(c, 400, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			a.cleanupImageV2Refs(created)
		}
	}()
	for _, v := range created {
		if err = a.Store.Put("assets", v.ID, v); err != nil {
			fail(c, 500, "保存参考图失败")
			return
		}
	}
	features := map[string]string{}
	controls := map[string]domain.ImageFeatureControl{}
	for _, key := range imageFlexibleFeatureNames {
		features[key] = b.PrimaryAssetID
		controls[key] = domain.ImageFeatureControl{Mode: "image", AssetID: b.PrimaryAssetID}
	}
	u := domain.ImageRewrite{ID: ID(), VersionID: versionID, WorkflowVersion: 2, PrimaryAssetID: b.PrimaryAssetID, References: refs, EditIntent: domain.ImageEditIntent{Mode: "flexible"}, SourceAssetIDs: []string{b.PrimaryAssetID}, FeatureSources: features, FeatureControls: controls, Output: domain.ImageOutput{Ratio: "3:4", Clarity: "standard", Count: 1}, Style: domain.ImageStyle{Ratio: "3:4"}, TextMode: "none", Revision: 1, Status: "draft", Analyses: []domain.ImageAnalysis{}, Results: []domain.ImageResult{}, Batches: []domain.ImageBatch{}, CreatedAt: now(), UpdatedAt: now()}
	if _, err = a.imageV2Files(r.ID, u); err != nil {
		fail(c, 400, err.Error())
		return
	}
	r.ImageRewrites = append(r.ImageRewrites, u)
	r.UpdatedAt = now()
	if !a.put(c, "rewrites", r.ID, r) {
		return
	}
	committed = true
	c.JSON(201, u)
}
func (a *App) patchImageV2(c *gin.Context) {
	var b struct {
		Revision           int                                    `json:"revision"`
		PrimaryAssetID     *string                                `json:"primary_asset_id"`
		References         *[]domain.ImageReference               `json:"references"`
		FeatureSources     *map[string]string                     `json:"feature_sources"`
		FeatureControls    *map[string]domain.ImageFeatureControl `json:"feature_controls"`
		EditIntent         *domain.ImageEditIntent                `json:"edit_intent"`
		Output             *domain.ImageOutput                    `json:"output"`
		TextMode           *string                                `json:"text_mode"`
		ImageText          *string                                `json:"image_text"`
		Prompt             *string                                `json:"prompt"`
		SelectedProposalID *string                                `json:"selected_proposal_id"`
		ExtraRequirements  json.RawMessage                        `json:"extra_requirements"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.visualRewrite(c)
	if !ok {
		return
	}
	u, index, ok := getImageRewrite(r, c.Param("image_id"))
	if !ok {
		fail(c, 404, "图片方案不存在")
		return
	}
	if b.Revision != u.Revision || a.imageRewriteBusy(r.ID, u.ID) {
		fail(c, 409, "方案已更新或任务尚未结束，请刷新或处理任务")
		return
	}
	old := u
	created := []domain.Asset{}
	committed := false
	defer func() {
		if !committed {
			a.cleanupImageV2Refs(created)
		}
	}()
	if b.PrimaryAssetID != nil {
		u.PrimaryAssetID = *b.PrimaryAssetID
		if u.PrimaryAssetID != old.PrimaryAssetID {
			u.TextMode = "pending"
			u.ImageText = ""
		}
	}
	if b.References != nil {
		var err error
		u.References, created, err = a.resolveImageV2References(r.ID, *b.References)
		if err != nil {
			fail(c, 400, err.Error())
			return
		}
	}
	for _, v := range created {
		if err := a.Store.Put("assets", v.ID, v); err != nil {
			fail(c, 500, "保存参考图失败")
			return
		}
	}
	if b.FeatureSources != nil {
		u.FeatureSources = *b.FeatureSources
	}
	if b.EditIntent != nil {
		u.EditIntent = normalizeImageEditIntent(*b.EditIntent)
	}
	if b.FeatureControls != nil {
		u.FeatureControls = copyImageFeatureControls(*b.FeatureControls)
		for key, control := range u.FeatureControls {
			if control.Mode == "custom" {
				control.Value = strings.TrimSpace(control.Value)
				u.FeatureControls[key] = control
			}
		}
	}
	if normalizeImageEditIntent(u.EditIntent).Mode == "flexible" {
		// The legacy compatibility map is derived, never a second authority.
		// Switching a dimension to custom/auto cannot leave its former image.
		u.FeatureSources = imageFeatureControlsSources(u.FeatureControls)
	}
	if b.Output != nil {
		u.Output = *b.Output
	}
	if b.TextMode != nil {
		u.TextMode = *b.TextMode
	}
	if b.ImageText != nil {
		u.ImageText = *b.ImageText
	}
	if len(b.ExtraRequirements) > 0 {
		var extra *string
		if err := json.Unmarshal(b.ExtraRequirements, &extra); err != nil {
			fail(c, 400, "附加要求须为文字或留空")
			return
		}
		u.ExtraRequirements = ""
		if extra != nil {
			u.ExtraRequirements = strings.TrimSpace(*extra)
		}
		if len([]rune(u.ExtraRequirements)) > 1000 {
			fail(c, 400, "附加要求不能超过 1000 字")
			return
		}
	}
	imagesChanged := digestImageValue(imageV2IDs(old)) != digestImageValue(imageV2IDs(u))
	if imagesChanged {
		clearImageProposals(&u)
		if b.FeatureControls == nil && normalizeImageEditIntent(u.EditIntent).Mode == "flexible" {
			u.FeatureControls = copyImageFeatureControls(u.FeatureControls)
			for key, control := range u.FeatureControls {
				if control.Mode == "image" && !has(imageV2IDs(u), control.AssetID) {
					u.FeatureControls[key] = domain.ImageFeatureControl{Mode: "image", AssetID: u.PrimaryAssetID}
				}
			}
			u.FeatureSources = imageFeatureControlsSources(u.FeatureControls)
		}
	}
	if b.SelectedProposalID != nil {
		u.SelectedProposalID = *b.SelectedProposalID
		if u.SelectedProposalID != "" {
			proposal := selectedImageProposal(u)
			if proposal == nil {
				fail(c, 409, "所选方案已失效，请重新分析图片")
				return
			}
			u.EditIntent = domain.ImageEditIntent{Mode: "flexible"}
			u.FeatureControls = copyImageFeatureControls(proposal.FeatureControls)
			u.FeatureSources = imageFeatureControlsSources(u.FeatureControls)
			if err := a.validateImageProposalSelection(r.ID, u); err != nil {
				fail(c, 409, err.Error())
				return
			}
		}
	} else if b.FeatureControls != nil && u.SelectedProposalID != "" {
		proposal := selectedImageProposal(u)
		if proposal == nil || digestImageValue(proposal.FeatureControls) != digestImageValue(u.FeatureControls) {
			u.SelectedProposalID = ""
		}
	}
	if err := validateImageV2Controls(u, true); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if _, err := a.imageV2Files(r.ID, u); err != nil {
		fail(c, 400, err.Error())
		return
	}
	semantic := imageV2SemanticDigest(old) != imageV2SemanticDigest(u)
	if semantic {
		u.PromptInputDigest = ""
		u.Status = "analyzed"
		u.AppliedVersionID = ""
	} else if b.Prompt != nil && *b.Prompt != u.Prompt {
		if u.PromptInputDigest == "" {
			fail(c, 409, "请先根据当前素材生成提示词")
			return
		}
		u.Prompt = *b.Prompt
		u.Status = "prompt_ready"
	}
	if len([]rune(u.Prompt)) > 4000 {
		fail(c, 400, "提示词超过 4000 字，请精简或拆图")
		return
	}
	kept := []domain.ImageAnalysis{}
	for _, row := range u.Analyses {
		if has(imageV2IDs(u), row.AssetID) {
			kept = append(kept, row)
		}
	}
	u.Analyses = kept
	u.SourceAssetIDs = []string{}
	if u.PrimaryAssetID != "" {
		u.SourceAssetIDs = []string{u.PrimaryAssetID}
	}
	u.Style.Ratio = u.Output.Ratio
	u.Revision++
	u.Error = ""
	u.UpdatedAt = now()
	r.ImageRewrites[index] = u
	r.UpdatedAt = now()
	if !a.put(c, "rewrites", r.ID, r) {
		return
	}
	committed = true
	c.JSON(200, u)
}
func (a *App) imageV2PromptInput(rewriteID string, u domain.ImageRewrite) (map[string]any, []string, []string, string, error) {
	if u.PrimaryAssetID == "" {
		return nil, nil, nil, "", errors.New("请选择被仿写图")
	}
	if err := validateImageV2(u); err != nil {
		return nil, nil, nil, "", err
	}
	if len(u.Proposals) > 0 {
		if err := a.validateImageProposalSelection(rewriteID, u); err != nil {
			return nil, nil, nil, "", err
		}
	}
	flexible := normalizeImageEditIntent(u.EditIntent).Mode == "flexible"
	primarySelected := false
	for _, id := range u.FeatureSources {
		if id == u.PrimaryAssetID {
			primarySelected = true
		}
	}
	if !flexible && !primarySelected {
		return nil, nil, nil, "", errors.New("至少保留被仿写图的一项特征")
	}
	allHashes, err := a.imageV2Files(rewriteID, u)
	if err != nil {
		return nil, nil, nil, "", err
	}
	ids := []string{}
	hashes := []string{}
	images := []map[string]any{}
	sources := map[string]int{}
	effectiveSources := effectiveImageFeatureSources(u)
	featureNames := imageFeatureNames
	if flexible {
		featureNames = imageFlexibleFeatureNames
	}
	for i, id := range imageV2IDs(u) {
		selected := map[string]string{}
		row, found := imageV2Analysis(u, id)
		for _, key := range featureNames {
			if effectiveSources[key] != id {
				continue
			}
			value := strings.TrimSpace(featureValue(row.Features, key))
			if key == "text" && !flexible {
				value = imageTypographyOnly(value, row.RecognizedText, u.ImageText)
			}
			if !found || row.SHA256 != allHashes[i] || value == "" || value == "未知" || (flexible && strings.TrimSpace(row.Features.Action) == "") {
				return nil, nil, nil, "", errors.New("所选特征未完成分析或图片已变化，请重新分析")
			}
			selected[key] = value
		}
		if len(selected) == 0 {
			continue
		}
		ids = append(ids, id)
		hashes = append(hashes, allHashes[i])
		role := "reference"
		if id == u.PrimaryAssetID {
			role = "primary"
		}
		image := map[string]any{"number": len(images) + 1, "role": role, "features": selected}
		if flexible && effectiveSources["text"] == id {
			text := strings.TrimSpace(row.RecognizedText)
			if text == "" {
				var asset domain.Asset
				if a.Store.Get("assets", id, &asset) == nil {
					text = strings.TrimSpace(asset.Extraction.Text)
				}
			}
			if len([]rune(text)) > 4000 {
				return nil, nil, nil, "", errors.New("图中文字过长，请在文字维度中自定义精简内容")
			}
			if text != "" {
				image["recognized_text"] = text
			}
		}
		images = append(images, image)
		for key := range selected {
			sources[key] = len(images)
		}
	}
	// Text recognition remains available in collection/extraction data, but
	// image composition receives typography only. Legacy OCR controls must
	// not silently reintroduce captions, logos, or watermarks into a new plan.
	intent := normalizeImageEditIntent(u.EditIntent)
	// Use an object here, rather than a struct, so canonical JSON remains
	// identical after the durable task payload is decoded into map[string]any.
	input := map[string]any{"images": images, "feature_sources": sources, "ratio": u.Output.Ratio, "rules_version": imagePromptRulesVersion, "edit_intent": map[string]string{"mode": intent.Mode, "style": intent.Style, "palette": intent.Palette}}
	if flexible {
		controls := map[string]map[string]any{}
		for _, key := range imageFlexibleFeatureNames {
			control := u.FeatureControls[key]
			entry := map[string]any{"mode": control.Mode}
			if control.Mode == "image" {
				entry["image_number"] = sources[key]
			} else if control.Mode == "custom" {
				entry["value"] = control.Value
			}
			controls[key] = entry
		}
		input["feature_controls"] = controls
	}
	if proposal := selectedImageProposal(u); proposal != nil {
		input["selected_proposal"] = map[string]string{"id": proposal.ID, "title": proposal.Title, "summary": proposal.Summary}
	}
	if u.ExtraRequirements != "" {
		input["extra_requirements"] = u.ExtraRequirements
	}
	digest := digestImageValue([]any{input, ids, hashes})
	return input, ids, hashes, digest, nil
}
func imageV2ProviderSnapshot(s domain.Settings) map[string]domain.ProviderConfig {
	out := map[string]domain.ProviderConfig{}
	for k, p := range s.Providers {
		p.APIKey = ""
		p.KeyMask = ""
		p.Configured = false
		out[k] = p
	}
	return out
}
func makeImageV2Job(kind string, r domain.Rewrite, u domain.ImageRewrite, p imageV2Payload, snapshot map[string]domain.ProviderConfig) domain.Job {
	b, _ := json.Marshal(p)
	return domain.Job{ID: ID(), Kind: kind, EntityID: r.ID, AssetID: u.PrimaryAssetID, SceneID: u.ID, SourceVersionID: u.VersionID, State: "queued", Phase: "queued", CreatedAt: now(), UpdatedAt: now(), Payload: b, ProviderSnapshot: snapshot}
}
func (a *App) submitImageV2(c *gin.Context, kind string) {
	var b struct {
		Revision         int    `json:"revision"`
		Prompt           string `json:"prompt"`
		RequestID        string `json:"request_id"`
		AssetID          string `json:"asset_id"`
		SuggestProposals bool   `json:"suggest_proposals"`
	}
	if !bind(c, &b) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.visualRewrite(c)
	if !ok {
		return
	}
	u, index, ok := getImageRewrite(r, c.Param("image_id"))
	if !ok || u.WorkflowVersion != 2 {
		fail(c, 404, "新版图片方案不存在")
		return
	}
	if kind == "generate" {
		for _, batch := range u.Batches {
			if batch.RequestID == b.RequestID && b.RequestID != "" {
				if batch.Revision != b.Revision || batch.Prompt != b.Prompt {
					fail(c, 409, "请求标识已用于其他生成内容")
					return
				}
				c.JSON(202, gin.H{"job_id": batch.Items[0].JobID, "batch_id": batch.ID})
				return
			}
		}
	}
	if b.Revision != u.Revision || a.imageRewriteBusy(r.ID, u.ID) {
		fail(c, 409, "方案已更新或已有任务，请刷新并核对")
		return
	}
	s, err := a.settings()
	if err != nil {
		fail(c, 500, "读取配置失败")
		return
	}
	capability := "vision"
	jobKind := "image_analyze"
	if kind == "compose-prompt" {
		capability = "text"
		jobKind = "image_compose"
	}
	if kind == "generate" {
		capability = "image"
		jobKind = "image_rewrite"
	}
	if !providerComplete(s.Providers[capability]) {
		fail(c, 409, "服务待配置："+map[string]string{"vision": "图片特征分析", "text": "提示词编排", "image": "图片生成"}[capability])
		return
	}
	p := imageV2Payload{WorkflowVersion: 2, Revision: u.Revision, AssetID: b.AssetID, SuggestProposals: b.SuggestProposals}
	if kind == "analyze" {
		if u.PrimaryAssetID == "" {
			fail(c, 400, "请选择被仿写图")
			return
		}
		if b.AssetID != "" && !has(imageV2IDs(u), b.AssetID) {
			fail(c, 400, "分析图片不属于当前方案")
			return
		}
		if b.SuggestProposals {
			p.AssetID = "" // A recommendation needs the complete chosen image set.
			if !providerComplete(s.Providers["text"]) {
				fail(c, 409, "服务待配置：分析方案推荐")
				return
			}
		}
	}
	if kind != "analyze" {
		input, ids, hashes, digest, err := a.imageV2PromptInput(r.ID, u)
		if err != nil {
			fail(c, 409, err.Error())
			return
		}
		p.InputDigest = digest
		if kind == "compose-prompt" {
			p.PromptInput = input
		}
		if kind == "generate" {
			model := s.Providers["image"].Model
			if !strings.HasPrefix(model, "qwen-image-3.0") && !strings.HasPrefix(model, "qwen-image-2.1") {
				fail(c, 409, "当前模型不支持此图片仿写流程")
				return
			}
			if b.RequestID == "" || len(b.RequestID) > 100 {
				fail(c, 400, "缺少有效的生成请求标识")
				return
			}
			if u.Prompt == "" || b.Prompt != u.Prompt || digest != u.PromptInputDigest || u.PromptImageConfig != imageConfigSignature(s.Providers["image"]) || u.PromptRulesVersion != imagePromptRulesVersion {
				fail(c, 409, "素材、模型或提示词已变化，请重新审阅生成提示词")
				return
			}
			batch := domain.ImageBatch{SelectedProposal: selectedImageProposal(u), ExtraRequirements: u.ExtraRequirements, ID: ID(), RequestID: b.RequestID, Revision: u.Revision, State: "running", Output: u.Output, Prompt: u.Prompt, PrimaryAssetID: u.PrimaryAssetID, InputAssetIDs: ids, InputHashes: hashes, InputDigest: digest, FeatureSources: effectiveImageFeatureSources(u), FeatureControls: effectiveImageFeatureControls(u), EditIntent: normalizeImageEditIntent(u.EditIntent), PromptRulesVersion: u.PromptRulesVersion, Model: model, CreatedAt: now(), Items: []domain.ImageBatchItem{}}
			for i := 0; i < u.Output.Count; i++ {
				batch.Items = append(batch.Items, domain.ImageBatchItem{Index: i, State: "waiting"})
			}
			p.BatchID = batch.ID
			p.Snapshot = batch
			u.Batches = append(u.Batches, batch)
		}
	}
	j := makeImageV2Job(jobKind, r, u, p, imageV2ProviderSnapshot(s))
	if kind == "generate" {
		n := len(u.Batches) - 1
		u.Batches[n].Items[0].State = "queued"
		u.Batches[n].Items[0].JobID = j.ID
	}
	u.Status = "queued"
	u.Error = ""
	u.UpdatedAt = now()
	r.ImageRewrites[index] = u
	if err = a.Store.Atomic(func(tx *store.Transaction) error {
		if err := tx.Put("jobs", j.ID, j); err != nil {
			return err
		}
		return tx.Put("rewrites", r.ID, r)
	}); err != nil {
		fail(c, 500, "提交图片任务失败")
		return
	}
	c.JSON(202, gin.H{"job_id": j.ID, "batch_id": p.BatchID})
}
func (a *App) routesImageV2(g *gin.RouterGroup) {
	g.GET("/image-capabilities", func(c *gin.Context) {
		s, err := a.settings()
		if err != nil {
			fail(c, 500, "读取服务配置失败")
			return
		}
		model := s.Providers["image"].Model
		supported := strings.HasPrefix(model, "qwen-image-3.0") || strings.HasPrefix(model, "qwen-image-2.1")
		sizes := []gin.H{}
		for _, ratio := range []string{"3:4", "1:1", "9:16"} {
			for _, clarity := range []string{"standard", "high"} {
				w, h, _ := imageV2Size(domain.ImageOutput{Ratio: ratio, Clarity: clarity, Count: 1})
				sizes = append(sizes, gin.H{"ratio": ratio, "clarity": clarity, "width": w, "height": h})
			}
		}
		c.JSON(200, gin.H{"image_configured": providerComplete(s.Providers["image"]), "vision_configured": providerComplete(s.Providers["vision"]), "text_configured": providerComplete(s.Providers["text"]), "supported": supported, "max_inputs": 3, "max_count": 4, "sizes": sizes, "prompt_rules_version": imagePromptRulesVersion, "proposal_rules_version": imageProposalRulesVersion, "proposal_count": 3})
	})
	g.POST("/rewrites/:id/images/:image_id/compose-prompt", func(c *gin.Context) { a.submitImageV2(c, "compose-prompt") })
	g.POST("/rewrites/:id/images/:image_id/batches/:batch_id/resume", a.resumeImageV2Batch)
}

func (a *App) imageV2Analyze(ctx context.Context, j *domain.Job, client *providers.Client, u *domain.ImageRewrite, p imageV2Payload) error {
	u.PromptInputDigest = ""
	previousProposals, previousDigest := u.Proposals, u.ProposalsInputDigest
	if p.SuggestProposals {
		clearImageProposals(u)
		if err := a.saveImageV2Unit(j, *u); err != nil {
			return err
		}
	}
	selectedSources := effectiveImageFeatureSources(*u)
	flexible := normalizeImageEditIntent(u.EditIntent).Mode == "flexible"
	for _, id := range imageV2IDs(*u) {
		if p.AssetID != "" && p.AssetID != id {
			continue
		}
		if flexible && p.AssetID == "" && !p.SuggestProposals {
			selected := false
			for _, sourceID := range selectedSources {
				if sourceID == id {
					selected = true
					break
				}
			}
			if !selected {
				continue
			}
		}
		data, asset, err := a.readImageV2File(j.EntityID, id)
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])
		key := digestImageValue([]string{hash, imageConfigSignature(j.ProviderSnapshot["vision"]), "visual-features-v2.4-flexible"})
		cache := imageAnalysisCache{}
		cached := a.Store.Get("image_analysis_cache", key, &cache) == nil
		if !cached {
			features, err := client.AnalyzeImage(ctx, data)
			if err != nil {
				return err
			}
			cache.Analysis = domain.ImageAnalysis{SHA256: hash, Features: features}
		}
		row := cache.Analysis
		row.AssetID = id
		row.Width = asset.Width
		row.Height = asset.Height
		row.TextError = ""
		if previous, exists := imageV2Analysis(*u, id); exists && previous.SHA256 == hash && strings.TrimSpace(previous.RecognizedText) != "" {
			// Project-local corrections take precedence over cached OCR. Never
			// write those corrections into the shared content-analysis cache.
			row.RecognizedText = previous.RecognizedText
		}
		// OCR is a separate collection capability. This action only analyzes
		// visual features and never makes an extra text-recognition request.
		if err = a.Store.Put("image_analysis_cache", key, cache); err != nil {
			return err
		}
		found := false
		for i, old := range u.Analyses {
			if old.AssetID == id {
				u.Analyses[i] = row
				found = true
			}
		}
		if !found {
			u.Analyses = append(u.Analyses, row)
		}
		// Commit each completed image so retrying a failed reference keeps prior work.
		if err = a.saveImageV2Unit(j, *u); err != nil {
			return err
		}
	}
	if p.SuggestProposals {
		input, digest, err := a.imageV2ProposalInput(j.EntityID, *u, j.ProviderSnapshot)
		if err != nil {
			return err
		}
		proposals := previousProposals
		if previousDigest != digest || validateImageProposals(*u, proposals) != nil {
			j.Phase = "recommending"
			j.Message = "图片分析完成，正在整理三个方案"
			if err := a.Store.Put("jobs", j.ID, j); err != nil {
				return err
			}
			proposals, err = client.RecommendImageProposals(ctx, input)
			if err != nil {
				return err
			}
		}
		if err := validateImageProposals(*u, proposals); err != nil {
			return err
		}
		u.Proposals = proposals
		u.ProposalsInputDigest = digest
		u.SelectedProposalID = ""
		u.EditIntent = domain.ImageEditIntent{Mode: "flexible"}
	}
	u.PromptInputDigest = ""
	u.Status = "analyzed"
	u.Error = ""
	u.Revision++
	return a.saveImageV2Unit(j, *u)
}
func (a *App) saveImageV2Unit(j *domain.Job, u domain.ImageRewrite) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jobCleared(j.ID) {
		return context.Canceled
	}
	var r domain.Rewrite
	if err := a.Store.Get("rewrites", j.EntityID, &r); err != nil {
		return err
	}
	old, i, ok := getImageRewrite(r, j.SceneID)
	if !ok || old.Revision != jobRevision(j) {
		return errChanged
	}
	u.UpdatedAt = now()
	r.ImageRewrites[i] = u
	r.UpdatedAt = now()
	return a.Store.Put("rewrites", r.ID, r)
}
func (a *App) imageV2Job(ctx context.Context, j *domain.Job, client *providers.Client) (jobErr error) {
	var p imageV2Payload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return err
	}
	a.mu.Lock()
	if ctx.Err() != nil || a.jobCleared(j.ID) {
		a.mu.Unlock()
		return context.Canceled
	}
	a.imageV2EntityState(*j)
	a.mu.Unlock()
	var r domain.Rewrite
	if err := a.Store.Get("rewrites", j.EntityID, &r); err != nil {
		return err
	}
	u, _, ok := getImageRewrite(r, j.SceneID)
	if !ok || u.Revision != p.Revision {
		return errChanged
	}
	if j.Kind == "image_analyze" {
		return a.imageV2Analyze(ctx, j, client, &u, p)
	}
	if j.Kind == "image_compose" {
		input, _, _, digest, err := a.imageV2PromptInput(r.ID, u)
		if err != nil {
			return err
		}
		if digest != p.InputDigest {
			return errChanged
		}
		if p.PromptInput == nil || digestImageValue(input) != digestImageValue(p.PromptInput) {
			return errors.New("提示词编排依据已更新，请重新提交编排")
		}
		prompt, err := client.ComposeImagePrompt(ctx, p.PromptInput)
		if err != nil {
			return err
		}
		if len([]rune(prompt)) > 4000 {
			return errors.New("提示词超过 4000 字，请精简后重试")
		}
		u.Prompt = prompt
		u.PromptInputDigest = digest
		u.PromptImageConfig = imageConfigSignature(j.ProviderSnapshot["image"])
		u.PromptRulesVersion = imagePromptRulesVersion
		u.Status = "prompt_ready"
		u.Error = ""
		u.Revision++
		return a.saveImageV2Unit(j, u)
	}
	if j.Kind != "image_rewrite" {
		return errors.New("图片任务类型无效")
	}
	batch := p.Snapshot
	for _, result := range u.Results {
		if result.JobID == j.ID {
			if _, _, err := a.readVisualImage(r.ID, result.AssetID); err == nil {
				j.ResultAssetIDs = []string{result.AssetID}
				j.Phase = "image_saved"
				return a.Store.Put("jobs", j.ID, j)
			}
		}
	}
	images := [][]byte{}
	for i, id := range batch.InputAssetIDs {
		data, _, err := a.readImageV2File(r.ID, id)
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		if i >= len(batch.InputHashes) || hex.EncodeToString(h[:]) != batch.InputHashes[i] {
			return errors.New("输入图片内容已变化，已暂停生成")
		}
		images = append(images, data)
	}
	width, height, err := imageV2Size(batch.Output)
	if err != nil {
		return err
	}
	paid := p.Receipt != nil
	defer func() {
		if paid && jobErr != nil {
			jobErr = fmt.Errorf("图片结果已返回，需恢复保存而非重复生图: %w", providers.ErrSubmissionUnknown)
		}
	}()
	var data []byte
	var mime string
	if p.Receipt != nil {
		data, mime, err = client.RestoreImage(ctx, *p.Receipt)
	} else {
		j.Phase = "submitting"
		j.Message = "按已确认的图片与提示词生成"
		if err = a.Store.Put("jobs", j.ID, j); err != nil {
			return err
		}
		onReceipt := func(receipt providers.ImageReceipt) error {
			paid = true
			p.Receipt = &receipt
			j.Payload, _ = json.Marshal(p)
			return a.Store.Put("jobs", j.ID, j)
		}
		if len(images) == 0 {
			data, mime, err = client.GenerateImageAtSize(ctx, batch.Prompt, fmt.Sprintf("%dx%d", width, height), onReceipt)
		} else {
			data, mime, err = client.RewriteImageAtSize(ctx, batch.Prompt, images, fmt.Sprintf("%dx%d", width, height), onReceipt)
		}
	}
	if err != nil {
		return err
	}
	paid = true
	_, ext := mediaType(mime)
	if ext == "" {
		return errors.New("图片格式不支持")
	}
	path := filepath.Join(a.Data, "assets", ID()+ext)
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	asset, err := a.generatedAsset(r.ID, "image", path, mime)
	if err != nil {
		return err
	}
	asset.SourceID = "image-rewrite:" + j.ID
	if err = a.Store.Put("assets", asset.ID, asset); err != nil {
		return err
	}
	warning := ""
	if asset.Width != width || asset.Height != height {
		warning = fmt.Sprintf("实际尺寸 %d×%d 与要求 %d×%d 不一致，请审阅", asset.Width, asset.Height, width, height)
	}
	requestID := ""
	if p.Receipt != nil {
		requestID = p.Receipt.RequestID
	}
	u.Results = append(u.Results, domain.ImageResult{SelectedProposal: batch.SelectedProposal, ExtraRequirements: batch.ExtraRequirements, JobID: j.ID, BatchID: batch.ID, AssetID: asset.ID, PrimaryAssetID: batch.PrimaryAssetID, FeatureSources: copyImageFeatureSources(batch.FeatureSources), FeatureControls: copyImageFeatureControls(batch.FeatureControls), EditIntent: batch.EditIntent, PromptRulesVersion: batch.PromptRulesVersion, Revision: batch.Revision, Prompt: batch.Prompt, Model: batch.Model, Ratio: batch.Output.Ratio, SourceAssetIDs: batch.InputAssetIDs, InputHashes: batch.InputHashes, Width: asset.Width, Height: asset.Height, Warning: warning, ProviderRequestID: requestID, CreatedAt: now()})
	j.ResultAssetIDs = []string{asset.ID}
	j.Phase = "image_saved"
	a.mu.Lock()
	defer a.mu.Unlock()
	var fresh domain.Rewrite
	if err = a.Store.Get("rewrites", r.ID, &fresh); err != nil {
		return err
	}
	old, index, ok := getImageRewrite(fresh, u.ID)
	if !ok || old.Revision != p.Revision {
		return errChanged
	}
	u.UpdatedAt = now()
	fresh.ImageRewrites[index] = u
	return a.Store.Atomic(func(tx *store.Transaction) error {
		if err := tx.Put("rewrites", r.ID, fresh); err != nil {
			return err
		}
		return tx.Put("jobs", j.ID, j)
	})
}

// Called while the caller holds the application lock (or before workers start).
func (a *App) imageV2EntityState(j domain.Job) bool {
	if !a.jobIsImageV2(&j) {
		return false
	}
	var p imageV2Payload
	_ = json.Unmarshal(j.Payload, &p)
	var r domain.Rewrite
	if a.Store.Get("rewrites", j.EntityID, &r) != nil {
		return true
	}
	u, index, ok := getImageRewrite(r, j.SceneID)
	if !ok || u.Revision != p.Revision {
		return true
	}
	if j.Kind != "image_rewrite" {
		if j.State != "succeeded" {
			u.Status = j.State
			u.Error = j.Error
			r.ImageRewrites[index] = u
			_ = a.Store.Put("rewrites", r.ID, r)
		}
		return true
	}
	for bi := range u.Batches {
		batch := &u.Batches[bi]
		if batch.ID != p.BatchID || p.Index >= len(batch.Items) {
			continue
		}
		slot := &batch.Items[p.Index]
		if j.State == "cancelled" {
			batch.State = "cancelled"
			for i := range batch.Items {
				if batch.Items[i].State != "succeeded" {
					batch.Items[i].State = "cancelled"
					batch.Items[i].Error = j.Error
				}
			}
			u.Status, u.Error = "cancelled", j.Error
			break
		}
		// Replaying completed notifications during startup must not resume a
		// later failed item or create another paid task.
		if batch.State == "cancelled" || slot.State == "skipped" || slot.State == "cancelled" || (slot.State == j.State && (j.State == "succeeded" || j.State == "failed" || j.State == "submission_unknown" || j.State == "waiting_config")) {
			return true
		}
		slot.State = j.State
		slot.Error = j.Error
		if len(j.ResultAssetIDs) > 0 {
			slot.AssetID = j.ResultAssetIDs[0]
		}
		if j.State == "succeeded" {
			batch.State = "complete"
			for next := range batch.Items {
				if batch.Items[next].State != "waiting" {
					continue
				}
				payload := p
				payload.Index = next
				payload.Receipt = nil
				payload.Snapshot = *batch
				nextJob := makeImageV2Job("image_rewrite", r, u, payload, j.ProviderSnapshot)
				batch.Items[next].State = "queued"
				batch.Items[next].JobID = nextJob.ID
				batch.State = "running"
				u.Status = "queued"
				u.Error = ""
				r.ImageRewrites[index] = u
				_ = a.Store.Atomic(func(tx *store.Transaction) error {
					if err := tx.Put("jobs", nextJob.ID, nextJob); err != nil {
						return err
					}
					return tx.Put("rewrites", r.ID, r)
				})
				return true
			}
			u.Status = "ready"
			u.Error = ""
		} else if j.State == "queued" || j.State == "running" {
			batch.State = "running"
			u.Status = j.State
		} else {
			batch.State = "paused"
			if j.State == "submission_unknown" {
				batch.State = "submission_unknown"
			}
			u.Status = batch.State
			u.Error = j.Error
		}
		break
	}
	u.UpdatedAt = now()
	r.ImageRewrites[index] = u
	_ = a.Store.Put("rewrites", r.ID, r)
	return true
}
func (a *App) recoverImageV2(j *domain.Job) bool {
	if !a.jobIsImageV2(j) {
		return false
	}
	if j.Kind != "image_rewrite" || j.State != "submission_unknown" {
		return true
	}
	var p imageV2Payload
	_ = json.Unmarshal(j.Payload, &p)
	var r domain.Rewrite
	_ = a.Store.Get("rewrites", j.EntityID, &r)
	u, _, ok := getImageRewrite(r, j.SceneID)
	if !ok || u.Revision != p.Revision {
		return true
	}
	found := false
	for _, result := range u.Results {
		if result.JobID == j.ID {
			if _, _, err := a.readVisualImage(r.ID, result.AssetID); err == nil {
				j.ResultAssetIDs = []string{result.AssetID}
				found = true
			}
		}
	}
	if found || p.Receipt != nil {
		j.State = "queued"
		j.Phase = "restoring_result"
		j.Error = ""
		_ = a.Store.Put("jobs", j.ID, j)
	}
	return true
}
func (a *App) resumeImageV2Batch(c *gin.Context) {
	var body struct {
		Action          string `json:"action"`
		ConfirmNoResult bool   `json:"confirm_no_result"`
	}
	if !bind(c, &body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.visualRewrite(c)
	if !ok {
		return
	}
	u, index, ok := getImageRewrite(r, c.Param("image_id"))
	if !ok || u.WorkflowVersion != 2 {
		fail(c, 404, "新版图片方案不存在")
		return
	}
	for bi := range u.Batches {
		batch := &u.Batches[bi]
		if batch.ID != c.Param("batch_id") {
			continue
		}
		if batch.State != "paused" && batch.State != "submission_unknown" {
			fail(c, 409, "当前批次不处于暂停状态")
			return
		}
		target := -1
		for i, s := range batch.Items {
			if s.State == "failed" || s.State == "submission_unknown" || s.State == "waiting_config" {
				target = i
				break
			}
		}
		if target < 0 {
			fail(c, 409, "没有可处理的暂停任务")
			return
		}
		slot := &batch.Items[target]
		var j domain.Job
		if a.Store.Get("jobs", slot.JobID, &j) != nil {
			fail(c, 409, "任务记录不存在")
			return
		}
		var p imageV2Payload
		_ = json.Unmarshal(j.Payload, &p)
		var resolvedJob *domain.Job
		if slot.State == "submission_unknown" && body.ConfirmNoResult && body.Action != "restore" {
			resolved := j
			resolved.State = "failed"
			resolved.Phase = "verified_no_result"
			resolved.Error = "用户已核实供应商没有生成结果"
			resolved.UpdatedAt = now()
			resolvedJob = &resolved
		}
		if body.Action == "restore" {
			if p.Receipt == nil {
				fail(c, 409, "没有已返回结果可恢复，请先核实供应商记录")
				return
			}
		}
		if slot.State == "submission_unknown" && body.Action != "restore" && !body.ConfirmNoResult {
			fail(c, 409, "结果不明，必须先在供应商核实没有结果")
			return
		}
		if body.Action == "cancel" {
			for i := range batch.Items {
				if batch.Items[i].State != "succeeded" {
					batch.Items[i].State = "cancelled"
				}
			}
			batch.State = "cancelled"
			u.Status = "prompt_ready"
			u.Error = ""
			r.ImageRewrites[index] = u
			if err := a.Store.Atomic(func(tx *store.Transaction) error {
				if resolvedJob != nil {
					if err := tx.Put("jobs", resolvedJob.ID, resolvedJob); err != nil {
						return err
					}
				}
				return tx.Put("rewrites", r.ID, r)
			}); err != nil {
				fail(c, 500, "结束批次失败")
				return
			}
			c.JSON(200, u)
			return
		}
		if body.Action == "continue" {
			slot.State = "skipped"
			target = -1
			for i, s := range batch.Items {
				if s.State == "waiting" {
					target = i
					break
				}
			}
			if target < 0 {
				batch.State = "complete"
				u.Status = "ready"
				u.Error = ""
				r.ImageRewrites[index] = u
				if err := a.Store.Atomic(func(tx *store.Transaction) error {
					if resolvedJob != nil {
						if err := tx.Put("jobs", resolvedJob.ID, resolvedJob); err != nil {
							return err
						}
					}
					return tx.Put("rewrites", r.ID, r)
				}); err != nil {
					fail(c, 500, "继续批次失败")
					return
				}
				c.JSON(200, u)
				return
			}
			p.Index = target
			p.Receipt = nil
			p.Snapshot = *batch
			j = makeImageV2Job("image_rewrite", r, u, p, j.ProviderSnapshot)
		} else if body.Action != "retry" && body.Action != "restore" {
			fail(c, 400, "请选择重试、恢复下载、继续或取消")
			return
		}
		j.State = "queued"
		j.Error = ""
		j.Message = "用户已确认继续"
		j.Phase = "queued"
		if p.Receipt != nil {
			j.Phase = "restoring_result"
		}
		if slot.State == "submission_unknown" && body.Action != "restore" {
			p.Receipt = nil
		}
		j.Payload, _ = json.Marshal(p)
		j.UpdatedAt = now()
		batch.Items[target].State = "queued"
		batch.Items[target].JobID = j.ID
		batch.Items[target].Error = ""
		batch.State = "running"
		u.Status = "queued"
		u.Error = ""
		r.ImageRewrites[index] = u
		if err := a.Store.Atomic(func(tx *store.Transaction) error {
			if resolvedJob != nil && resolvedJob.ID != j.ID {
				if err := tx.Put("jobs", resolvedJob.ID, resolvedJob); err != nil {
					return err
				}
			}
			if err := tx.Put("jobs", j.ID, j); err != nil {
				return err
			}
			return tx.Put("rewrites", r.ID, r)
		}); err != nil {
			fail(c, 500, "恢复任务失败")
			return
		}
		c.JSON(202, gin.H{"job_id": j.ID})
		return
	}
	fail(c, 404, "生成批次不存在")
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/tooling"
)

func (a *App) StartWorkers() {
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		log.Printf("load jobs for worker recovery: %v", err)
		return
	}
	a.refreshXHSAccountAsync()
	// An ID persisted before a crash is always polled, never submitted again.
	for _, j := range jobs {
		if a.retireWorkspaceJob(&j) {
			continue
		}
		if a.retireVideoJob(&j) {
			continue
		}
		a.recoverVideoRewriteJob(&j)
		if j.Kind == "video_script" || j.Kind == "video_rewrite" {
			a.videoRewriteEntityState(j)
		}
		a.recoverImageRewriteResult(&j)
		if j.Kind == "image_analyze" || j.Kind == "image_rewrite" || j.Kind == "image_compose" {
			a.imageRewriteEntityState(j)
		}

	}
	for _, kinds := range [][]string{{"collect"}, {"ocr"}, {"ocr"}, {"asr"}, {"rewrite", "image_analyze", "image_compose", "video_script"}, {"image_rewrite", "video_rewrite"}, {"publish"}} {
		a.lifecycleMu.Lock()
		if a.closing {
			a.lifecycleMu.Unlock()
			return
		}
		a.wg.Add(1)
		go a.worker(kinds)
		a.lifecycleMu.Unlock()
	}
}
func (a *App) worker(kinds []string) {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}
		var j domain.Job
		claimed := false
		for _, kind := range kinds {
			a.mu.Lock()
			next, e := a.Store.Claim(kind)
			a.mu.Unlock()
			if e != nil {
				log.Printf("queue claim: %v", e)
				continue
			}
			if next != nil {
				j = *next
				claimed = true
				break
			}
		}
		if !claimed {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(400 * time.Millisecond):
			}
			continue
		}
		a.runJob(&j)
	}
}
func (a *App) runJob(j *domain.Job) {
	if a.retireWorkspaceJob(j) {
		return
	}
	if a.retireVideoJob(j) {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Minute)
	defer cancel()
	run := &jobRun{cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	if a.jobCleared(j.ID) {
		a.mu.Unlock()
		return
	}
	if a.jobRuns == nil {
		a.jobRuns = map[string]*jobRun{}
	}
	if a.jobRuns[j.ID] != nil {
		a.mu.Unlock()
		return
	}
	a.jobRuns[j.ID] = run
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.jobRuns, j.ID); a.mu.Unlock(); close(run.done) }()
	var e error
	if j.Kind == "publish" {
		var p domain.Publication
		if err := a.Store.Get("publications", j.EntityID, &p); err != nil {
			e = errors.New("发布稿不存在")
		} else if _, valid := a.publicationWorkspacePlatform(p, map[string]bool{}); !valid {
			e = errors.New("历史发布稿混用了不同来源或素材，已停止提交")
		} else {
			e = a.publishXHSJob(ctx, j)
		}
	} else {
		s, settingsErr := a.settings()
		e = settingsErr
		if e == nil {
			if len(j.ProviderSnapshot) == 0 {
				j.ProviderSnapshot = map[string]domain.ProviderConfig{}
				for k, p := range s.Providers {
					p.APIKey = ""
					p.Configured = false
					p.KeyMask = ""
					j.ProviderSnapshot[k] = p
				}
				a.Store.Put("jobs", j.ID, j)
			} else {
				for k, p := range j.ProviderSnapshot {
					p.APIKey = s.Providers[k].APIKey
					s.Providers[k] = p
				}
			}
			client := providers.New(s)
			switch j.Kind {
			case "collect":
				e = a.collectJob(ctx, j, client, s)
			case "ocr", "asr":
				e = a.extractJob(ctx, j, client, s)
			case "rewrite":
				e = a.rewriteJob(ctx, j, client)
			case "video_script", "video_rewrite":
				e = a.videoRewriteJob(ctx, j, client)
			case "image_analyze", "image_rewrite", "image_compose":
				e = a.imageRewriteJob(ctx, j, client)
			default:
				e = errors.New("未知任务类型")
			}
		}
	}
	state, msg := "succeeded", "已完成"
	if e != nil {
		msg = a.safeError(e)
		state = "failed"
		if errors.Is(e, providers.ErrNotConfigured) {
			state = "waiting_config"
		}
		if errors.Is(e, providers.ErrSubmissionUnknown) || errors.Is(e, errXHSPublishUnknown) {
			state = "submission_unknown"
		}
		if a.ctx.Err() != nil {
			state = "queued"
			if j.Phase == "submitting" {
				state = "submission_unknown"
			}
			msg = "服务已停止，启动后恢复"
			if j.Kind == "publish" && j.Phase == "submitting" {
				state = "submission_unknown"
				msg = errXHSPublishUnknown.Error()
			}
		}
	}
	if state == "submission_unknown" && j.Kind == "image_rewrite" {
		msg = "图片生成提交结果不明：请先到图片服务控制台核实结果；本任务已暂停，不会自动重复提交"
	}
	a.mu.Lock()
	if a.jobCleared(j.ID) {
		j.State, j.Error, j.Message, j.UpdatedAt = "cancelled", "任务已由用户中断", "任务已由用户中断", now()
		_ = a.Store.Put("job_tombstones", j.ID, clearedJob{Job: *j, CleanupDone: true})
		a.mu.Unlock()
		return
	}
	j.State = state
	j.Error = ""
	j.Message = msg
	j.UpdatedAt = now()
	if e != nil {
		j.Error = msg
	} else {
		j.Progress = 100
		j.Phase = "complete"
	}
	a.Store.Put("jobs", j.ID, j)
	a.jobEntityState(*j)
	a.mu.Unlock()
}
func (a *App) jobEntityState(j domain.Job) {
	if j.Kind == "video_script" || j.Kind == "video_rewrite" {
		a.videoRewriteEntityState(j)
		return
	}
	if j.Kind == "image_analyze" || j.Kind == "image_rewrite" || j.Kind == "image_compose" {
		a.imageRewriteEntityState(j)
		return
	}
	if j.Kind == "publish" {
		a.publishEntityState(j)
		return
	}
	if j.Kind == "collect" && j.State != "succeeded" {
		var n domain.Note
		if a.Store.Get("notes", j.EntityID, &n) == nil {
			if j.State == "queued" {
				n.Status = "queued"
				if n.MetadataStatus != "succeeded" {
					n.MetadataStatus = "queued"
				}
				a.Store.Put("notes", n.ID, n)
				return
			}
			n.Status = "failed"
			if n.MetadataStatus == "succeeded" {
				n.Status = "partial"
			} else {
				n.MetadataStatus = "failed"
			}
			n.Error = j.Error
			if n.MetadataStatus != "succeeded" && (n.Title == "等待采集" || n.Title == "" || strings.HasPrefix(n.Title, "采集失败")) {
				n.Title = failedCollectionTitle(n)
			}
			a.Store.Put("notes", n.ID, n)
		}
	}
	if j.Kind == "ocr" || j.Kind == "asr" {
		var v domain.Asset
		if a.Store.Get("assets", j.AssetID, &v) == nil {
			if j.State != "succeeded" && !v.Extraction.Edited {
				v.Extraction.Status = j.State
				v.Extraction.Error = j.Error
				a.Store.Put("assets", v.ID, v)
			}
			a.updateExtractionState(j.EntityID)
		}
	}

}
func (a *App) updateExtractionState(noteID string) {
	var n domain.Note
	if a.Store.Get("notes", noteID, &n) != nil {
		return
	}
	state := "succeeded"
	for _, id := range n.AssetIDs {
		var v domain.Asset
		if a.Store.Get("assets", id, &v) != nil {
			continue
		}
		if v.Kind != "image" && v.Kind != "cover" && v.Kind != "video" {
			continue
		}
		switch v.Extraction.Status {
		case "failed", "cancelled":
			state = "partial"
		case "waiting_config":
			if state != "partial" {
				state = "waiting_config"
			}
		case "pending", "queued", "running", "":
			if state == "succeeded" {
				state = "pending"
			}
		}
	}
	n.ExtractionStatus = state
	a.Store.Put("notes", n.ID, n)
}
func (a *App) collectJob(ctx context.Context, j *domain.Job, client *providers.Client, s domain.Settings) error {
	var payload struct {
		URL string `json:"url"`
	}
	json.Unmarshal(j.Payload, &payload)
	a.progress(j, "metadata", 10, "读取笔记内容")
	var n domain.Note
	var templates []domain.Asset
	var e error
	n, templates, e = client.Collect(ctx, payload.URL, s.Cookie)
	n.Platform = "xiaohongshu"
	if e != nil {
		return objectError("读取笔记", e)
	}
	if n.NoteID == "" {
		return errors.New("采集服务未返回有效笔记 ID")
	}
	a.mu.Lock()
	if ctx.Err() != nil || a.jobCleared(j.ID) {
		a.mu.Unlock()
		return context.Canceled
	}
	var old domain.Note
	a.Store.Get("notes", j.EntityID, &old)
	notes, err := readList[domain.Note](a, "notes")
	if err != nil {
		a.mu.Unlock()
		return err
	}
	for _, other := range notes {
		if other.NoteID == n.NoteID && other.Platform == n.Platform && other.ID != j.EntityID {
			a.Store.Delete("notes", j.EntityID)
			j.EntityID = other.ID
			old = other
			break
		}
	}
	n.ID = j.EntityID
	if n.URL == "" {
		n.URL = payload.URL
	}
	n.Tags = old.Tags
	if n.Tags == nil {
		n.Tags = []string{}
	}
	n.CollectedAt = now()
	n.MetadataStatus = "succeeded"
	n.MediaStatus = "running"
	n.ExtractionStatus = "pending"
	n.Status = "collecting"
	n.Error = ""
	n.Raw = nil
	n.AssetIDs = []string{}
	n.CoverAssetID = ""
	assets := []domain.Asset{}
	for i, t := range templates {
		v := domain.Asset{ID: ID(), OwnerID: n.ID, Kind: t.Kind, SourceURL: t.SourceURL, SourceID: t.SourceID, ExpectedSize: t.ExpectedSize, Width: t.Width, Height: t.Height, Duration: t.Duration, Position: i, Status: "pending", CreatedAt: now(), Extraction: domain.Extraction{Status: "pending", Segments: []domain.Segment{}}}
		for _, id := range old.AssetIDs {
			var x domain.Asset
			if a.Store.Get("assets", id, &x) == nil && sameCollectedResource(x, t) {
				v = x
				v.SourceURL = t.SourceURL
				v.SourceID, v.ExpectedSize = t.SourceID, t.ExpectedSize
				v.Position = i
				break
			}
		}
		assets = append(assets, v)
		n.AssetIDs = append(n.AssetIDs, v.ID)
		if v.Kind == "cover" || (v.Kind == "image" && n.CoverAssetID == "") {
			n.CoverAssetID = v.ID
		}
		if e = a.Store.Put("assets", v.ID, v); e != nil {
			break
		}
	}
	if e == nil {
		e = a.Store.Put("notes", n.ID, n)
	}
	a.Store.Put("jobs", j.ID, j)
	a.mu.Unlock()
	if e != nil {
		return e
	}
	failures := []string{}
	for i := range assets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		v := assets[i]
		e = nil
		a.progress(j, "media", 20+(i*60/max(1, len(assets))), fmt.Sprintf("保存素材 %d/%d", i+1, len(assets)))
		_, fileErr := a.assetPath(v)
		if v.Status != "ready" || fileErr != nil {
			v.Status = "downloading"
			if e = a.saveMediaAsset(ctx, j.ID, &v); e != nil {
				return e
			}
			e = a.downloadAsset(ctx, &v)
			if e != nil {
				v.Status = "failed"
				v.Error = a.safeError(e)
				failures = append(failures, v.Error)
			}
		}
		if e = a.saveMediaAsset(ctx, j.ID, &v); e != nil {
			return e
		}
		if ctx.Err() == nil && v.Status == "ready" && !v.Extraction.Edited && v.Extraction.Status != "succeeded" {
			kind := "ocr"
			if v.Kind == "video" {
				kind = "asr"
			}
			if v.Kind == "video" || v.Kind == "image" || v.Kind == "cover" {
				a.mu.Lock()
				if ctx.Err() != nil || a.jobCleared(j.ID) {
					e = context.Canceled
				} else {
					_, e = a.enqueueLocked(kind, n.ID, v.ID, "", nil)
				}
				a.mu.Unlock()
				if e != nil {
					return e
				}
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil || a.jobCleared(j.ID) {
		return context.Canceled
	}
	a.Store.Get("notes", n.ID, &n)
	n.CoverAssetID = a.readyCoverAssetID(n)
	n.MediaStatus = "succeeded"
	n.Status = "ready"
	if len(failures) > 0 {
		n.MediaStatus = "partial"
		n.Status = "partial"
		n.Error = strings.Join(failures, "；")
	}
	a.updateExtractionState(n.ID)
	var updated domain.Note
	a.Store.Get("notes", n.ID, &updated)
	n.ExtractionStatus = updated.ExtractionStatus
	if e = a.Store.Put("notes", n.ID, n); e != nil {
		return e
	}
	if rewrites, err := readList[domain.Rewrite](a, "rewrites"); err == nil {
		for _, rewrite := range rewrites {
			if rewrite.NoteID == n.ID {
				if err := a.initializeVideoRewritesLocked(rewrite, n); err != nil {
					log.Printf("complete collected video rewrites: %v", err)
				}
			}
		}
	} else {
		log.Printf("list video rewrites after collection: %v", err)
	}
	if len(failures) > 0 {
		return errors.New(n.Error)
	}
	return nil
}

// Signed query strings rotate, so the stored source URL is the identity that
// preserves edited extraction text without confusing replaced media with old files.
func sameCollectedResource(old, fresh domain.Asset) bool {
	return old.Kind == fresh.Kind && old.SourceURL == fresh.SourceURL
}

// Download completion must not overwrite OCR or manual text saved while the
// network request was running.
func (a *App) saveMediaAsset(ctx context.Context, jobID string, v *domain.Asset) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil || a.jobCleared(jobID) {
		return context.Canceled
	}
	var current domain.Asset
	if a.Store.Get("assets", v.ID, &current) == nil {
		v.Extraction = current.Extraction
	}
	return a.Store.Put("assets", v.ID, v)
}

func (a *App) extractJob(ctx context.Context, j *domain.Job, client *providers.Client, s domain.Settings) error {
	var asset domain.Asset
	if e := a.Store.Get("assets", j.AssetID, &asset); e != nil {
		return e
	}
	path, e := a.assetPath(asset)
	if e != nil {
		return e
	}
	a.progress(j, "extracting", 20, "提取素材文字")
	a.mu.Lock()
	if ctx.Err() != nil || a.jobCleared(j.ID) {
		a.mu.Unlock()
		return context.Canceled
	}
	// Re-read under the write lock: a user may have corrected the text after
	// the initial lookup. Retrying extraction must preserve that correction.
	if e = a.Store.Get("assets", asset.ID, &asset); e != nil {
		a.mu.Unlock()
		return e
	}
	if asset.Extraction.Edited {
		asset.Extraction.Status = "succeeded"
		asset.Extraction.Error = ""
		e = a.Store.Put("assets", asset.ID, asset)
		a.updateExtractionState(j.EntityID)
		a.mu.Unlock()
		return e
	}
	asset.Extraction.Status = "running"
	asset.Extraction.Error = ""
	e = a.Store.Put("assets", asset.ID, asset)
	a.updateExtractionState(j.EntityID)
	a.mu.Unlock()
	if e != nil {
		return e
	}
	var text string
	segments := []domain.Segment{}
	if j.Kind == "ocr" {
		if asset.MIME == "image/heic" {
			tmp := filepath.Join(a.Data, "assets", ID()+".png")
			defer os.Remove(tmp)
			if e = tooling.Command(ctx, a.Root, s.FFmpegPath, "ffmpeg", "-y", "-i", path, "-frames:v", "1", tmp).Run(); e != nil {
				return errors.New("HEIC 转换失败，请检查 FFmpeg")
			}
			path = tmp
		}
		text, e = client.OCR(ctx, path)
	} else {
		tmp := filepath.Join(a.Data, "assets", ID()+".pcm")
		defer os.Remove(tmp)
		a.progress(j, "audio", 15, "提取 16kHz 单声道音频")
		if e = tooling.Command(ctx, a.Root, s.FFmpegPath, "ffmpeg", "-y", "-i", path, "-vn", "-ac", "1", "-ar", "16000", "-f", "s16le", tmp).Run(); e != nil {
			return errors.New("音频提取失败，请检查 FFmpeg 和原视频音轨")
		}
		segments, e = client.Transcribe(ctx, tmp)
		parts := []string{}
		for _, seg := range segments {
			parts = append(parts, seg.Text)
		}
		text = strings.Join(parts, "\n")
	}
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var current domain.Asset
	if e = a.Store.Get("assets", asset.ID, &current); e != nil {
		return e
	}
	if ctx.Err() != nil || a.jobCleared(j.ID) {
		return context.Canceled
	}
	if current.Extraction.Edited {
		return errors.New("提取过程中已人工修改文字，保留人工内容")
	}
	current.Extraction = domain.Extraction{Text: text, Segments: segments, Status: "succeeded"}
	return a.Store.Put("assets", current.ID, current)
}
func (a *App) rewriteJob(ctx context.Context, j *domain.Job, client *providers.Client) error {
	var r domain.Rewrite
	if e := a.Store.Get("rewrites", j.EntityID, &r); e != nil {
		return e
	}
	var opts domain.RewriteOptions
	if e := json.Unmarshal(j.Payload, &opts); e != nil {
		return e
	}
	var note domain.Note
	if isBlankRewrite(r) {
		note = blankRewriteSource()
	} else {
		if e := a.Store.Get("notes", r.NoteID, &note); e != nil {
			return e
		}
	}
	opts, texts, extractions := a.rewriteInputs(note, opts)
	a.progress(j, "generating", 20, "生成新的仿写版本")
	v, e := client.Rewrite(ctx, note, texts, opts)
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil || a.jobCleared(j.ID) {
		return context.Canceled
	}
	if e = a.Store.Get("rewrites", r.ID, &r); e != nil {
		return e
	}
	v.ID = ID()
	v.SourceSnapshot = &note
	v.SourceExtractions = extractions
	if isBlankRewrite(r) {
		v.SourceSnapshot, v.SourceExtractions = nil, nil
	}
	v.Number = len(r.Versions) + 1
	v.CreatedAt = now()
	v.Confirmed = false
	if len(r.Versions) > 0 {
		v.AssetIDs = r.Versions[len(r.Versions)-1].AssetIDs
	} else {
		v.AssetIDs = note.AssetIDs
	}
	r.Versions = append(r.Versions, v)
	r.Options = opts
	if !r.TitleCustomized {
		r.Title = v.Title
	}
	r.UpdatedAt = now()
	return a.Store.Put("rewrites", r.ID, r)
}
func jobRevision(j *domain.Job) int {
	var p struct {
		Revision int `json:"revision"`
	}
	json.Unmarshal(j.Payload, &p)
	return p.Revision
}

package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

var errDeletionBusy = errors.New("请等待正在执行的相关任务完成后再删除；结果不明的任务请先核实并恢复")
var errAssetInUse = errors.New("素材仍被内容或历史版本使用，请删除相关内容后再删除素材")
var errUnsafeDeletionPath = errors.New("本机文件路径未通过安全检查，未删除任何内容")

var deletionBuckets = []string{"notes", "rewrites", "videos", "publications", "assets", "jobs"}

type deletionResponse struct {
	Deleted        map[string]int `json:"deleted"`
	FilesDeleted   int            `json:"files_deleted"`
	RetainedAssets int            `json:"retained_assets"`
	CleanupPending bool           `json:"cleanup_pending,omitempty"`
}

type deletionSnapshot struct {
	notes        map[string]domain.Note
	rewrites     map[string]domain.Rewrite
	videos       map[string]domain.VideoProject
	publications map[string]domain.Publication
	assets       map[string]domain.Asset
	jobs         map[string]domain.Job
}

func readDeletionSnapshot(tx *store.Transaction) (*deletionSnapshot, error) {
	s := &deletionSnapshot{}
	var err error
	if s.notes, err = deletionRecords[domain.Note](tx, "notes"); err != nil {
		return nil, err
	}
	if s.rewrites, err = deletionRecords[domain.Rewrite](tx, "rewrites"); err != nil {
		return nil, err
	}
	if s.videos, err = deletionRecords[domain.VideoProject](tx, "videos"); err != nil {
		return nil, err
	}
	if s.publications, err = deletionRecords[domain.Publication](tx, "publications"); err != nil {
		return nil, err
	}
	if s.assets, err = deletionRecords[domain.Asset](tx, "assets"); err != nil {
		return nil, err
	}
	if s.jobs, err = deletionRecords[domain.Job](tx, "jobs"); err != nil {
		return nil, err
	}
	return s, nil
}

func deletionRecords[T any](tx *store.Transaction, bucket string) (map[string]T, error) {
	rows, err := tx.List(bucket)
	if err != nil {
		return nil, err
	}
	out := map[string]T{}
	for _, raw := range rows {
		var value T
		var identity struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &identity); err != nil {
			return nil, err
		}
		if identity.ID == "" {
			return nil, errors.New("stored record has no ID")
		}
		out[identity.ID] = value
	}
	return out, nil
}

// Deletion follows actual source lineage, even for legacy publications whose
// target platform is inconsistent and therefore hidden by ordinary list APIs.
func (s *deletionSnapshot) platform(bucket, id string, seen map[string]bool) (string, bool) {
	key := bucket + ":" + id
	if id == "" || seen[key] {
		return "", false
	}
	seen[key] = true
	switch bucket {
	case "notes":
		v, ok := s.notes[id]
		return noteWorkspacePlatform(v), ok
	case "rewrites":
		v, ok := s.rewrites[id]
		if !ok {
			return "", false
		}
		if p, ok := s.platform("notes", v.NoteID, seen); ok {
			return p, true
		}
		for _, version := range v.Versions {
			if version.SourceSnapshot != nil {
				return noteWorkspacePlatform(*version.SourceSnapshot), true
			}
		}
		return "xiaohongshu", true
	case "videos":
		v, ok := s.videos[id]
		if !ok {
			return "", false
		}
		if p, ok := s.platform("rewrites", v.RewriteID, seen); ok {
			return p, true
		}
		return "xiaohongshu", true
	case "publications":
		v, ok := s.publications[id]
		if !ok {
			return "", false
		}
		if p, ok := s.platform("rewrites", v.RewriteID, seen); ok {
			return p, true
		}
		if p, ok := s.platform("videos", v.VideoProjectID, seen); ok {
			return p, true
		}
		return "", false
	case "assets":
		v, ok := s.assets[id]
		if !ok {
			return "", false
		}
		return s.ownerPlatform(v.OwnerID, seen)
	case "jobs":
		v, ok := s.jobs[id]
		if !ok {
			return "", false
		}
		if p, ok := s.ownerPlatform(v.EntityID, seen); ok {
			return p, true
		}
		if p, ok := s.platform("assets", v.AssetID, seen); ok {
			return p, true
		}
		return "xiaohongshu", true
	}
	return "", false
}

func (s *deletionSnapshot) ownerPlatform(id string, seen map[string]bool) (string, bool) {
	for _, bucket := range []string{"notes", "rewrites", "videos", "publications", "assets"} {
		if p, ok := s.platform(bucket, id, workspaceAncestors(seen)); ok {
			return p, true
		}
	}
	return "", false
}

type deletionPlan struct {
	ids      map[string]map[string]bool
	versions map[string]bool
	retained map[string]bool
	reparent map[string]string
}

func newDeletionPlan() *deletionPlan {
	p := &deletionPlan{ids: map[string]map[string]bool{}, versions: map[string]bool{}, retained: map[string]bool{}, reparent: map[string]string{}}
	for _, bucket := range deletionBuckets {
		p.ids[bucket] = map[string]bool{}
	}
	return p
}

func (p *deletionPlan) ownerDeleted(id string) bool {
	for _, bucket := range []string{"notes", "rewrites", "videos", "publications", "assets"} {
		if p.ids[bucket][id] {
			return true
		}
	}
	return false
}

func noteAssetRefs(n domain.Note) []string {
	return append(append([]string{}, n.AssetIDs...), n.CoverAssetID)
}
func rewriteAssetRefs(r domain.Rewrite) []string {
	ids := append([]string{}, r.Options.ExtractionAssetIDs...)
	for _, item := range r.ImageRewrites {
		ids = append(ids, imageV2IDs(item)...)
		for _, batch := range item.Batches {
			ids = append(ids, batch.PrimaryAssetID)
			ids = append(ids, batch.InputAssetIDs...)
		}
		ids = append(ids, item.SourceAssetIDs...)
		ids = append(ids, item.StyleReferenceID)
		for _, result := range item.Results {
			ids = append(ids, result.AssetID)
			ids = append(ids, result.SourceAssetIDs...)
			ids = append(ids, result.PrimaryAssetID, result.StyleReferenceID)
		}
	}
	for _, v := range r.Versions {
		ids = append(ids, v.AssetIDs...)
		for id := range v.SourceExtractions {
			ids = append(ids, id)
		}
		if v.SourceSnapshot != nil {
			ids = append(ids, noteAssetRefs(*v.SourceSnapshot)...)
		}
	}
	return ids
}
func sceneAssetRefs(sc domain.Scene) []string {
	ids := []string{sc.VideoAssetID, sc.ImageAssetID}
	for _, voice := range sc.Voices {
		ids = append(ids, voice.AssetID)
	}
	return ids
}
func videoAssetRefs(v domain.VideoProject) []string {
	ids := []string{}
	if v.VideoRewrite != nil {
		state := v.VideoRewrite
		ids = append(ids, state.InitialSourceAssetID, state.PrimaryAssetID, state.ReferenceVideoID, state.ReferenceImageID)
		for _, analysis := range state.Analyses {
			ids = append(ids, analysis.AssetID)
		}
		for _, submission := range state.Submissions {
			ids = append(ids, videoGenerationAssetRefs(submission.Snapshot)...)
		}
		for _, result := range state.Results {
			ids = append(ids, result.AssetID, result.PreviewAssetID, result.CoverAssetID)
			ids = append(ids, videoGenerationAssetRefs(result.Snapshot)...)
		}
	}
	if v.Plan != nil {
		ids = append(ids, v.Plan.Options.ImageAssetIDs...)
	}
	for _, plan := range v.PlanHistory {
		ids = append(ids, plan.Options.ImageAssetIDs...)
	}
	for _, sc := range v.Scenes {
		ids = append(ids, sceneAssetRefs(sc)...)
	}
	for _, r := range v.Renders {
		ids = append(ids, r.AssetID, r.CoverAssetID, r.SRTAssetID)
	}
	return ids
}
func videoGenerationAssetRefs(snapshot domain.VideoGenerationSnapshot) []string {
	ids := append([]string{snapshot.PrimaryAssetID}, snapshot.InputAssetIDs...)
	for _, id := range snapshot.PurposeSources {
		ids = append(ids, id)
	}
	for id := range snapshot.InputHashes {
		ids = append(ids, id)
	}
	return ids
}
func jobAssetRefs(j domain.Job) []string {
	ids := append([]string{j.AssetID}, j.ResultAssetIDs...)
	var payload imageV2Payload
	if json.Unmarshal(j.Payload, &payload) == nil && payload.WorkflowVersion == 2 {
		ids = append(ids, payload.Snapshot.PrimaryAssetID)
		ids = append(ids, payload.Snapshot.InputAssetIDs...)
	}
	if j.InputScene != nil {
		ids = append(ids, sceneAssetRefs(*j.InputScene)...)
	}
	if j.Kind == "video_script" || j.Kind == "video_rewrite" {
		var videoPayload struct {
			Snapshot domain.VideoGenerationSnapshot `json:"snapshot"`
		}
		if json.Unmarshal(j.Payload, &videoPayload) == nil {
			ids = append(ids, videoGenerationAssetRefs(videoPayload.Snapshot)...)
		}
	}
	return ids
}
func protectedDeletionJob(j domain.Job) bool {
	return j.State == "running" || j.State == "submission_unknown"
}

func (s *deletionSnapshot) plan(bucket string, ids []string, platform, workspace string) (*deletionPlan, error) {
	p := newDeletionPlan()
	if workspace != "" {
		for _, b := range deletionBuckets {
			var all []string
			switch b {
			case "notes":
				for id := range s.notes {
					all = append(all, id)
				}
			case "rewrites":
				for id := range s.rewrites {
					all = append(all, id)
				}
			case "videos":
				for id := range s.videos {
					all = append(all, id)
				}
			case "publications":
				for id := range s.publications {
					all = append(all, id)
				}
			case "assets":
				for id := range s.assets {
					all = append(all, id)
				}
			case "jobs":
				for id := range s.jobs {
					all = append(all, id)
				}
			}
			for _, id := range all {
				if source, ok := s.platform(b, id, map[string]bool{}); ok && source == workspace {
					p.ids[b][id] = true
				}
			}
		}
	} else {
		for _, id := range ids {
			source, ok := s.platform(bucket, id, map[string]bool{})
			if !ok || platform != "" && source != platform {
				return nil, store.ErrNotFound
			}
			p.ids[bucket][id] = true
		}
	}
	// Descendants are selected by foreign-key lineage, never by target labels.
	for id, r := range s.rewrites {
		if p.ids["notes"][r.NoteID] {
			p.ids["rewrites"][id] = true
		}
		for _, v := range r.Versions {
			_, sourceExists := s.notes[r.NoteID]
			if !sourceExists && v.SourceSnapshot != nil && p.ids["notes"][v.SourceSnapshot.ID] {
				p.ids["rewrites"][id] = true
			}
		}
		if p.ids["rewrites"][id] {
			for _, v := range r.Versions {
				if v.ID != "" {
					p.versions[v.ID] = true
				}
			}
		}
	}
	for id, v := range s.videos {
		if p.ids["rewrites"][v.RewriteID] {
			p.ids["videos"][id] = true
		}
	}
	for id, v := range s.publications {
		if p.ids["rewrites"][v.RewriteID] {
			p.ids["publications"][id] = true
		} else if p.ids["videos"][v.VideoProjectID] {
			// A malformed historical draft can point at another workspace's
			// video. Its true rewrite lineage still protects that foreign record.
			publicationSource, publicationKnown := s.platform("publications", id, map[string]bool{})
			videoSource, videoKnown := s.platform("videos", v.VideoProjectID, map[string]bool{})
			if publicationKnown && videoKnown && publicationSource == videoSource {
				p.ids["publications"][id] = true
			}
		}
	}
	// Ownership may form nested generated assets; continue to a fixed point.
	for changed := true; changed; {
		changed = false
		for id, v := range s.assets {
			if !p.ids["assets"][id] && p.ownerDeleted(v.OwnerID) {
				p.ids["assets"][id] = true
				changed = true
			}
		}
	}
	for id, j := range s.jobs {
		owned := p.ids["jobs"][id] || p.ownerDeleted(j.EntityID)
		related := owned || p.versions[j.SourceVersionID]
		for _, assetID := range jobAssetRefs(j) {
			related = related || p.ids["assets"][assetID]
		}
		if related {
			if protectedDeletionJob(j) {
				return nil, errDeletionBusy
			}
			// Do not erase another surviving entity's history merely because it
			// references shared media. Its reference also keeps that media alive.
			if owned || j.EntityID == "" {
				p.ids["jobs"][id] = true
			}
		}
	}
	// A surviving consumer preserves both the asset record and its physical file.
	retain := func(refs []string, owner string) {
		for _, id := range refs {
			if p.ids["assets"][id] {
				p.retained[id] = true
				if owner != "" {
					p.reparent[id] = owner
				}
			}
		}
	}
	for id, v := range s.notes {
		if !p.ids["notes"][id] {
			retain(noteAssetRefs(v), id)
		}
	}
	for id, v := range s.rewrites {
		if !p.ids["rewrites"][id] {
			retain(rewriteAssetRefs(v), id)
		}
	}
	for id, v := range s.videos {
		if !p.ids["videos"][id] {
			retain(videoAssetRefs(v), id)
		}
	}
	for id, v := range s.publications {
		if !p.ids["publications"][id] {
			retain(v.AssetIDs, id)
		}
	}
	for id, v := range s.assets {
		if !p.ids["assets"][id] {
			retain([]string{v.OwnerID}, id)
		}
	}
	for id, v := range s.jobs {
		if !p.ids["jobs"][id] {
			retain(jobAssetRefs(v), v.EntityID)
		}
	}
	for changed := true; changed; {
		changed = false
		for id := range p.retained {
			owner := s.assets[id].OwnerID
			if p.ids["assets"][owner] && !p.retained[owner] {
				p.retained[owner] = true
				p.reparent[owner] = p.reparent[id]
				changed = true
			}
		}
	}
	if bucket == "assets" && len(p.retained) > 0 {
		return nil, errAssetInUse
	}
	for id := range p.retained {
		delete(p.ids["assets"], id)
	}
	return p, nil
}

func (a *App) routesDeletions(g *gin.RouterGroup) {
	for _, route := range []struct{ path, bucket string }{{"collections", "notes"}, {"rewrites", "rewrites"}, {"video-projects", "videos"}, {"publications", "publications"}, {"assets", "assets"}} {
		bucket := route.bucket
		g.DELETE("/"+route.path+"/:id", func(c *gin.Context) {
			var body struct {
				Confirm bool `json:"confirm"`
			}
			if !bind(c, &body) {
				return
			}
			if !body.Confirm {
				fail(c, 400, "请再次确认删除后提交")
				return
			}
			a.deleteLocalRecords(c, bucket, []string{c.Param("id")}, "")
		})
	}
	g.DELETE("/collections", func(c *gin.Context) {
		var body struct {
			IDs     []string `json:"ids"`
			Confirm bool     `json:"confirm"`
		}
		if !bind(c, &body) {
			return
		}
		if !body.Confirm || len(body.IDs) == 0 || len(body.IDs) > 500 {
			fail(c, 400, "请选中 1–500 条内容并再次确认删除")
			return
		}
		a.deleteLocalRecords(c, "notes", body.IDs, "")
	})
}

func deletionFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "当前工作区没有此记录")
	case errors.Is(err, errDeletionBusy), errors.Is(err, errAssetInUse), errors.Is(err, errUnsafeDeletionPath):
		fail(c, http.StatusConflict, err.Error())
	default:
		fail(c, http.StatusInternalServerError, "删除失败，数据未提交，请稍后重试")
	}
}

func sortedDeletionIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (a *App) deleteLocalRecords(c *gin.Context, bucket string, ids []string, workspace string) {
	// Login/publish code takes the account operation lock before the app mutex.
	if workspace == "xiaohongshu" {
		a.xhs.op.Lock()
		defer a.xhs.op.Unlock()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	result := deletionResponse{Deleted: map[string]int{}}
	var staged *deletionStaging
	err := a.Store.Atomic(func(tx *store.Transaction) error {
		if workspace != "" {
			if active, err := workspaceRegistryActive(tx, workspace); err != nil {
				return err
			} else if !active {
				return store.ErrNotFound
			}
		}
		snapshot, err := readDeletionSnapshot(tx)
		if err != nil {
			return err
		}
		plan, err := snapshot.plan(bucket, ids, workspacePlatform(c), workspace)
		if err != nil {
			return err
		}
		paths, err := a.deletionFiles(snapshot, plan)
		if err != nil {
			return err
		}
		staged, err = a.stageDeletionFiles(paths)
		if err != nil {
			return err
		}
		for _, b := range deletionBuckets {
			for _, id := range sortedDeletionIDs(plan.ids[b]) {
				if err = tx.Delete(b, id); err != nil {
					return err
				}
			}
			result.Deleted[b] = len(plan.ids[b])
		}
		for id := range plan.retained {
			asset := snapshot.assets[id]
			if plan.ownerDeleted(asset.OwnerID) && plan.reparent[id] != "" {
				asset.OwnerID = plan.reparent[id]
				if err = tx.Put("assets", id, asset); err != nil {
					return err
				}
			}
		}
		result.RetainedAssets = len(plan.retained)
		if workspace != "" {
			if err = setWorkspaceRegistry(tx, workspace, false); err != nil {
				return err
			}
			if err = tx.Delete("xhs_accounts", "main"); err != nil {
				return err
			}
			var settings domain.Settings
			if err = tx.Get("settings", "main", &settings); err == nil {
				settings.Cookie = ""
				settings.CookieConfigured = false
				settings.CookieMask = ""
				err = tx.Put("settings", "main", settings)
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		if staged != nil {
			return tx.Put("deletion_cleanup", staged.ID, staged)
		}
		return nil
	})
	if err != nil {
		if staged != nil {
			if restoreErr := a.rollbackDeletionFiles(staged); restoreErr != nil {
				fail(c, 500, "删除未提交，部分文件恢复待处理，请重启服务恢复")
				return
			}
		}
		deletionFailure(c, err)
		return
	}
	if workspace != "" {
		a.clearDeletedWorkspaceAccount(c, workspace)
	}
	if staged != nil {
		result.FilesDeleted, err = a.finishDeletionFiles(staged)
		result.CleanupPending = err != nil
	}
	c.JSON(200, result)
}

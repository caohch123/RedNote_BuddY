package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

type jobRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type clearedJob struct {
	domain.Job
	CleanupDone bool `json:"cleanup_done"`
}

func (a *App) recoverClearedJobs() error {
	markers, err := readList[clearedJob](a, "job_tombstones")
	if err != nil {
		return err
	}
	for _, marker := range markers {
		if marker.CleanupDone {
			continue
		}
		if !completedJob(marker.State) || marker.State == "cancelled" {
			a.clearJobEntity(marker.Job)
		}
		marker.CleanupDone = true
		if err := a.Store.Put("job_tombstones", marker.ID, marker); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) clearJobEntity(job domain.Job) {
	job.State, job.Error, job.Message = "cancelled", "任务已由用户中断", "任务已由用户中断"
	if job.Kind == "publish" && job.Phase == "submitting" {
		job.State = "submission_unknown"
		job.Error = "任务已中断，平台提交结果待核实"
	}
	a.jobEntityState(job)
}

func (a *App) jobCleared(id string) bool {
	var marker json.RawMessage
	return a.Store.Get("job_tombstones", id, &marker) == nil
}

// Only background records are cleared. Project results and provider receipts remain.
func (a *App) clearJobs(c *gin.Context) {
	if !validWorkspacePlatform(workspacePlatform(c)) {
		fail(c, 400, "请指定有效的工作区")
		return
	}
	a.mu.Lock()
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		a.mu.Unlock()
		fail(c, 500, "读取任务列表失败")
		return
	}
	selected := []domain.Job{}
	ids := []string{}
	for _, job := range jobs {
		if a.workspaceAllows(c, "jobs", job.ID, true) {
			selected = append(selected, job)
			ids = append(ids, job.ID)
		}
	}
	err = a.Store.Atomic(func(tx *store.Transaction) error {
		for _, job := range selected {
			if !completedJob(job.State) {
				job.State = "cancelled"
				job.Error = "任务已由用户中断"
				job.Message = job.Error
				job.UpdatedAt = now()
			}
			if e := tx.Put("job_tombstones", job.ID, clearedJob{Job: job}); e != nil {
				return e
			}
			if e := tx.Delete("jobs", job.ID); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		a.mu.Unlock()
		fail(c, 500, "清空任务失败")
		return
	}
	runs := []*jobRun{}
	interrupted := 0
	for _, job := range selected {
		if run := a.jobRuns[job.ID]; run != nil {
			run.cancel()
			runs = append(runs, run)
		}
		if !completedJob(job.State) {
			interrupted++
			a.clearJobEntity(job)
			job.State, job.Error, job.Message = "cancelled", "任务已由用户中断", "任务已由用户中断"
		}
		_ = a.Store.Put("job_tombstones", job.ID, clearedJob{Job: job, CleanupDone: true})
	}
	a.mu.Unlock()
	// Cancellation propagates to HTTP, WebSocket and local tool processes. Do not
	// hold App.mu while their finish paths release entity state.
	waitCtx, stop := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer stop()
	stopping := 0
	for _, run := range runs {
		select {
		case <-run.done:
		case <-waitCtx.Done():
			stopping++
		}
	}
	remoteUnstopped := 0
	cloudCtx, stopCloud := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer stopCloud()
	for _, job := range selected {
		if completedJob(job.State) {
			continue
		}
		if job.Kind == "image_rewrite" {
			if job.Phase == "submitting" || job.State == "submission_unknown" {
				remoteUnstopped++
			}
			continue
		}
		if job.Kind != "video_rewrite" {
			continue
		}
		var latest clearedJob
		if a.Store.Get("job_tombstones", job.ID, &latest) == nil {
			job.Phase = latest.Phase
			if latest.RemoteTaskID != "" {
				job.RemoteTaskID = latest.RemoteTaskID
			}
		}
		if job.RemoteTaskID == "" {
			if job.Phase == "submitting" || job.State == "submission_unknown" {
				remoteUnstopped++
			}
			continue
		}
		s, e := a.settings()
		if e == nil {
			if p, ok := job.ProviderSnapshot["video"]; ok {
				p.APIKey = s.Providers["video"].APIKey
				s.Providers["video"] = p
			}
			ctx, cancel := context.WithTimeout(cloudCtx, 3*time.Second)
			e = providers.New(s).CancelVideoTask(ctx, job.RemoteTaskID)
			cancel()
		}
		if e != nil {
			remoteUnstopped++
		}
	}
	c.JSON(200, gin.H{"deleted": len(ids), "ids": ids, "interrupted": interrupted, "stopping": stopping, "remote_unstopped": remoteUnstopped})
}

func completedJob(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}

func (a *App) clearCompletedJobs(c *gin.Context) {
	platform := workspacePlatform(c)
	if !validWorkspacePlatform(platform) {
		fail(c, http.StatusBadRequest, "请指定有效的工作区")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	rows, err := a.Store.List("jobs")
	if err != nil {
		log.Printf("list jobs for cleanup: %v", err)
		fail(c, http.StatusInternalServerError, "读取任务列表失败")
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		var job domain.Job
		if err := json.Unmarshal(row, &job); err != nil {
			log.Printf("decode job for cleanup: %v", err)
			fail(c, http.StatusInternalServerError, "读取任务列表失败")
			return
		}
		if completedJob(job.State) && a.workspaceAllows(c, "jobs", job.ID, true) {
			ids = append(ids, job.ID)
		}
	}

	deleted := 0
	err = a.Store.Atomic(func(tx *store.Transaction) error {
		for _, id := range ids {
			var job domain.Job
			if err := tx.Get("jobs", id, &job); err != nil {
				return err
			}
			if !completedJob(job.State) {
				continue
			}
			if err := tx.Delete("jobs", id); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	if err != nil {
		log.Printf("clear completed jobs: %v", err)
		fail(c, http.StatusInternalServerError, "清理已结束任务失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

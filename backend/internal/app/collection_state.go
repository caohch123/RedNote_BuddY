package app

import (
	"encoding/json"
	"errors"
	"strings"
	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func failedCollectionTitle(n domain.Note) string {
	if n.NoteID != "" {
		return "采集失败 · 小红书作品 " + n.NoteID
	}
	return "采集失败 · 小红书作品"
}

// The placeholder and its queue entry commit together. An interrupted insert
// cannot leave a note promising work which was never queued.
func (a *App) enqueueCollectionLocked(n domain.Note, payload any) (domain.Job, error) {
	b, e := json.Marshal(payload)
	if e != nil {
		return domain.Job{}, e
	}
	jobs, err := readList[domain.Job](a, "jobs")
	if err != nil {
		return domain.Job{}, err
	}
	for _, j := range jobs {
		if j.Kind == "collect" && j.EntityID == n.ID && (j.State == "queued" || j.State == "running") {
			return j, nil
		}
	}
	j := domain.Job{ID: ID(), Kind: "collect", EntityID: n.ID, State: "queued", Phase: "queued", CreatedAt: now(), UpdatedAt: now(), Payload: b}
	n.Status, n.Error = "queued", ""
	if n.MetadataStatus != "succeeded" {
		n.MetadataStatus = "queued"
		if n.Title == "" || strings.HasPrefix(n.Title, "采集失败") {
			n.Title = "等待采集"
		}
	}
	e = a.Store.Atomic(func(tx *store.Transaction) error {
		active, err := workspaceRegistryActive(tx, noteWorkspacePlatform(n))
		if err != nil {
			return err
		}
		if !active {
			return errors.New("采集工作区已删除")
		}
		if err = tx.Put("notes", n.ID, n); err != nil {
			return err
		}
		return tx.Put("jobs", j.ID, j)
	})
	return j, e
}

// Startup reconciles older placeholders against durable job states without
// inventing content or silently retrying failed remote operations.
func (a *App) recoverCollectionStates() error {
	return a.Store.Atomic(func(tx *store.Transaction) error {
		rows, err := tx.List("notes")
		if err != nil {
			return err
		}
		notes, err := store.Decode[domain.Note](rows)
		if err != nil {
			return err
		}
		rows, err = tx.List("jobs")
		if err != nil {
			return err
		}
		jobs, err := store.Decode[domain.Job](rows)
		if err != nil {
			return err
		}
		for _, n := range notes {
			active := false
			var last domain.Job
			for _, j := range jobs {
				if j.Kind != "collect" || j.EntityID != n.ID {
					continue
				}
				active = active || j.State == "queued" || j.State == "running"
				if last.ID == "" || j.UpdatedAt > last.UpdatedAt {
					last = j
				}
			}
			changed := false
			if !active && (n.Status == "queued" || n.Status == "collecting" || n.MetadataStatus == "queued" || n.MetadataStatus == "running") {
				changed = true
				if n.MetadataStatus == "succeeded" {
					n.Status = "partial"
					if n.MediaStatus == "succeeded" {
						n.Status = "ready"
					}
				} else {
					n.Status, n.MetadataStatus = "failed", "failed"
				}
				n.Error = last.Error
				if n.Error == "" && n.Status != "ready" {
					n.Error = "采集任务已中断或不存在，请重新采集"
				}
			}
			if !active && n.MetadataStatus != "succeeded" && (n.Status == "failed" || n.MetadataStatus == "failed") && (n.Title == "等待采集" || n.Title == "") {
				n.Title = failedCollectionTitle(n)
				changed = true
			}
			if changed {
				if err = tx.Put("notes", n.ID, n); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

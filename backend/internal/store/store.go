package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
	"xhs-workbench/internal/domain"
)

var ErrNotFound = errors.New("record not found")
var ErrJobCleared = errors.New("job has been cleared")
var ErrConflict = errors.New("publication already submitted")

//go:embed migrations/001_initial.sql
var initialMigration string

//go:embed migrations/002_billing.sql
var billingMigration string

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", initialMigration, billingMigration} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	// Legacy drafts were exclusively for Xiaohongshu. JSON records make the
	// additive platform migration compatible with old database snapshots.
	if _, err = db.Exec(`UPDATE records SET data=json_set(data,'$.target_platform','xiaohongshu') WHERE bucket='publications' AND COALESCE(json_extract(data,'$.target_platform'),'')=''`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error {
	var value int
	return s.db.QueryRowContext(ctx, "SELECT 1").Scan(&value)
}
func (s *Store) Put(bucket, id string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if bucket == "jobs" {
		result, writeErr := s.db.Exec(`INSERT INTO records(bucket,id,data,updated_at) SELECT ?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM records WHERE bucket='job_tombstones' AND id=?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, bucket, id, string(b), time.Now().UTC().Format(time.RFC3339Nano), id)
		if writeErr != nil {
			return writeErr
		}
		if count, e := result.RowsAffected(); e != nil {
			return e
		} else if count == 0 {
			return ErrJobCleared
		}
		return nil
	}
	_, err = s.db.Exec(`INSERT INTO records(bucket,id,data,updated_at) VALUES(?,?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, bucket, id, string(b), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) Get(bucket, id string, v any) error {
	var b string
	err := s.db.QueryRow(`SELECT data FROM records WHERE bucket=? AND id=?`, bucket, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(b), v)
}
func (s *Store) List(bucket string) ([]json.RawMessage, error) {
	rows, err := s.db.Query(`SELECT data FROM records WHERE bucket=? ORDER BY updated_at DESC`, bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func (s *Store) Delete(bucket, id string) error {
	_, err := s.db.Exec(`DELETE FROM records WHERE bucket=? AND id=?`, bucket, id)
	return err
}
func (s *Store) Claim(kind string) (*domain.Job, error) {
	var b string
	err := s.db.QueryRow(`UPDATE records SET data=json_set(data,'$.state','running','$.updated_at',?,'$.attempts',COALESCE(json_extract(data,'$.attempts'),0)+1),updated_at=? WHERE bucket='jobs' AND id=(SELECT id FROM records WHERE bucket='jobs' AND json_extract(data,'$.state')='queued' AND json_extract(data,'$.kind')=? ORDER BY updated_at ASC LIMIT 1) RETURNING data`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339Nano), kind).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j domain.Job
	if err = json.Unmarshal([]byte(b), &j); err != nil {
		return nil, err
	}
	return &j, nil
}
func (s *Store) Recover() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Publishing is a side effect without an idempotency key. A crash after a
	// request was sent must never turn into a second publish request.
	_, err = tx.Exec(`UPDATE records SET data=json_set(data,'$.state',CASE WHEN json_extract(data,'$.kind')='publish' OR json_extract(data,'$.phase')='submitting' THEN 'submission_unknown' ELSE 'queued' END,'$.error',CASE WHEN json_extract(data,'$.kind')='publish' THEN '服务停止时发布尚未确认，请到对应平台核实，禁止自动重复发布' WHEN json_extract(data,'$.kind')='image_rewrite' AND json_extract(data,'$.phase')='submitting' THEN '图片生成提交结果不明：请先到图片服务控制台核实结果；本任务已暂停，不会自动重复提交' WHEN json_extract(data,'$.phase')='submitting' THEN '提交结果不明，请填入远程任务 ID 恢复，或核实后手动重试' ELSE '' END) WHERE bucket='jobs' AND json_extract(data,'$.state')='running'`)
	if err != nil {
		return err
	}
	// A recorded platform note ID is sufficient evidence to recover the job as
	// successful even if the process stopped before its final job-state write.
	_, err = tx.Exec(`UPDATE records AS j SET data=json_set(j.data,'$.state','succeeded','$.phase','complete','$.progress',100,'$.error','','$.message','平台已确认提交') WHERE j.bucket='jobs' AND json_extract(j.data,'$.kind')='publish' AND EXISTS(SELECT 1 FROM records AS p WHERE p.bucket='publications' AND p.id=json_extract(j.data,'$.entity_id') AND json_extract(p.data,'$.publish_job_id')=j.id AND json_extract(p.data,'$.status')='published' AND COALESCE(json_extract(p.data,'$.xhs_note_id'),'')<>'')`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE records AS p SET data=json_set(p.data,'$.status','submission_unknown','$.publish_error','服务停止时发布尚未确认，请到对应平台核实，禁止自动重复发布') WHERE p.bucket='publications' AND json_extract(p.data,'$.status')='publishing' AND EXISTS(SELECT 1 FROM records AS j WHERE j.bucket='jobs' AND j.id=json_extract(p.data,'$.publish_job_id') AND json_extract(j.data,'$.state')='submission_unknown')`)
	if err != nil {
		return err
	}
	// Prepared browser editors and confirmations live in one process only.
	// Recovery may query known work IDs, but may not replay an old editor.
	_, err = tx.Exec(`UPDATE records AS j SET data=json_set(j.data,'$.state','failed','$.error','该发布流程已移除，请重新准备并确认') WHERE j.bucket='jobs' AND json_extract(j.data,'$.kind')='publish_prepare' AND json_extract(j.data,'$.state') IN ('queued','running','submission_unknown')`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE records SET data=json_set(data,'$.status','publish_failed','$.publish_error','该发布流程已移除，请重新准备并确认') WHERE bucket='publications' AND json_extract(data,'$.status') IN ('preparing_publish','awaiting_confirmation')`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// CreatePublishJob commits the snapshot state and queue entry together. A
// duplicate confirmation cannot create a job in the crash window between them.
func (s *Store) CreatePublishJob(p domain.Publication, j domain.Job) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow(`SELECT data FROM records WHERE bucket='publications' AND id=?`, p.ID).Scan(&raw); err != nil {
		return err
	}
	var current domain.Publication
	if err = json.Unmarshal([]byte(raw), &current); err != nil {
		return err
	}
	if current.Status == "published" || current.Status == "publishing" || current.Status == "submission_unknown" || current.Status == "submitted" || current.Status == "reviewing" || current.Status == "rejected" || current.PlatformWorkID != "" {
		return ErrConflict
	}
	var active int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM records WHERE bucket='jobs' AND json_extract(data,'$.kind')='publish' AND json_extract(data,'$.entity_id')=? AND json_extract(data,'$.state') IN ('queued','running','submission_unknown')`, p.ID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrConflict
	}
	for _, item := range []struct {
		bucket, id string
		value      any
	}{{"publications", p.ID, p}, {"jobs", j.ID, j}} {
		b, e := json.Marshal(item.value)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(`INSERT INTO records(bucket,id,data,updated_at) VALUES(?,?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, item.bucket, item.id, string(b), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func Decode[T any](rows []json.RawMessage) ([]T, error) {
	out := make([]T, 0, len(rows))
	for _, b := range rows {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, fmt.Errorf("invalid stored record: %w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

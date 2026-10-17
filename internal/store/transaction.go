package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Transaction exposes the same record operations on one SQLite transaction.
// Callers must only use this value (not Store) until Atomic returns.
type Transaction struct{ tx *sql.Tx }

func (s *Store) Atomic(fn func(*Transaction) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(&Transaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (t *Transaction) Get(bucket, id string, v any) error {
	var raw string
	err := t.tx.QueryRow(`SELECT data FROM records WHERE bucket=? AND id=?`, bucket, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

func (t *Transaction) List(bucket string) ([]json.RawMessage, error) {
	rows, err := t.tx.Query(`SELECT data FROM records WHERE bucket=? ORDER BY id`, bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}

func (t *Transaction) Put(bucket, id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if bucket == "jobs" {
		result, writeErr := t.tx.Exec(`INSERT INTO records(bucket,id,data,updated_at) SELECT ?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM records WHERE bucket='job_tombstones' AND id=?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, bucket, id, string(raw), time.Now().UTC().Format(time.RFC3339Nano), id)
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
	_, err = t.tx.Exec(`INSERT INTO records(bucket,id,data,updated_at) VALUES(?,?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at`, bucket, id, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (t *Transaction) Delete(bucket, id string) error {
	_, err := t.tx.Exec(`DELETE FROM records WHERE bucket=? AND id=?`, bucket, id)
	return err
}

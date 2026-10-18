package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"xhs-workbench/internal/domain"
)

func (s *Store) SaveUsage(v domain.UsageRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRow(`SELECT data FROM usage_events WHERE id=?`, v.ID).Scan(&raw)
	if err == nil {
		var old domain.UsageRecord
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return err
		}
		v.CreatedAt, v.Price = old.CreatedAt, old.Price
		v.CredentialID = old.CredentialID
		if old.JobID != "" {
			v.JobID, v.EntityID, v.Operation = old.JobID, old.EntityID, old.Operation
		}
		if v.TotalTokens == nil {
			v.TotalTokens, v.InputTokens, v.OutputTokens = old.TotalTokens, old.InputTokens, old.OutputTokens
		}
		if v.CachedTokens == nil {
			v.CachedTokens = old.CachedTokens
		}
		if v.ReasoningTokens == nil {
			v.ReasoningTokens = old.ReasoningTokens
		}
		if v.ImageCount == nil {
			v.ImageCount = old.ImageCount
		}
		if v.VideoSeconds == nil {
			v.VideoSeconds = old.VideoSeconds
		}
		if old.Status == "success" || old.Status == "failed" {
			v.Status = old.Status
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	v.EstimatedCNY = EstimateTokenCost(v)
	created, err := time.Parse(time.RFC3339Nano, v.CreatedAt)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO usage_events(id,created_at,capability,model,data) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, v.ID, created.UnixMilli(), v.Capability, v.Model, string(b))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func EstimateTokenCost(v domain.UsageRecord) *float64 {
	if v.Price == nil || v.InputTokens == nil || v.OutputTokens == nil {
		return nil
	}
	input := float64(*v.InputTokens) * v.Price.InputPerMillion
	if v.Price.CachedPerMillion != nil {
		if v.CachedTokens == nil {
			return nil
		}
		input = float64(*v.InputTokens-*v.CachedTokens)*v.Price.InputPerMillion + float64(*v.CachedTokens)*(*v.Price.CachedPerMillion)
	}
	value := (input + float64(*v.OutputTokens)*v.Price.OutputPerMillion) / 1_000_000
	return &value
}

func (s *Store) Usage(filter domain.UsageFilter) ([]domain.UsageRecord, error) {
	query, args := `SELECT data FROM usage_events WHERE 1=1`, []any{}
	if filter.From != 0 {
		query += ` AND created_at>=?`
		args = append(args, filter.From)
	}
	if filter.To != 0 {
		query += ` AND created_at<?`
		args = append(args, filter.To)
	}
	if filter.Model != "" {
		query += ` AND model=?`
		args = append(args, filter.Model)
	}
	if filter.Capability != "" {
		query += ` AND capability=?`
		args = append(args, filter.Capability)
	}
	query += ` ORDER BY created_at DESC,id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.UsageRecord{}
	for rows.Next() {
		var raw string
		var v domain.UsageRecord
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

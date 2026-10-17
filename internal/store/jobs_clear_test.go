package store

import (
	"errors"
	"path/filepath"
	"testing"
	"xhs-workbench/internal/domain"
)

func TestClearedJobCannotReturnAfterReopenOrTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	job := domain.Job{ID: "cleared", State: "running", Kind: "ocr"}
	if err = s.Put("job_tombstones", job.ID, job); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("jobs", job.ID, job); !errors.Is(err, ErrJobCleared) {
		t.Fatalf("job resurrected: %v", err)
	}
	err = s.Atomic(func(tx *Transaction) error {
		if e := tx.Put("notes", "late-result", map[string]string{"title": "late"}); e != nil {
			return e
		}
		return tx.Put("jobs", job.ID, job)
	})
	if !errors.Is(err, ErrJobCleared) {
		t.Fatal(err)
	}
	var note any
	if err = s.Get("notes", "late-result", &note); !errors.Is(err, ErrNotFound) {
		t.Fatal("late result transaction was not rolled back")
	}
	if err = s.Recover(); err != nil {
		t.Fatal(err)
	}
	if next, err := s.Claim("ocr"); err != nil || next != nil {
		t.Fatalf("cleared job recovered: %+v %v", next, err)
	}
}

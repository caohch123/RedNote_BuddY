package store

import (
	"errors"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestAtomicCascadeRollsBackEveryMutation(t *testing.T) {
	s := openTestStore(t)
	for _, item := range []struct{ bucket, id string }{{"notes", "note"}, {"assets", "asset"}, {"jobs", "job"}} {
		if err := s.Put(item.bucket, item.id, map[string]string{"id": item.id}); err != nil {
			t.Fatal(err)
		}
	}
	stop := errors.New("abort after all deletions")
	err := s.Atomic(func(tx *Transaction) error {
		for _, item := range []struct{ bucket, id string }{{"notes", "note"}, {"assets", "asset"}, {"jobs", "job"}} {
			if err := tx.Delete(item.bucket, item.id); err != nil {
				return err
			}
		}
		if err := tx.Put("workspace_registry", "main", map[string]any{"platforms": []string{}}); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	for _, item := range []struct{ bucket, id string }{{"notes", "note"}, {"assets", "asset"}, {"jobs", "job"}} {
		var value map[string]string
		if err := s.Get(item.bucket, item.id, &value); err != nil {
			t.Fatalf("partial delete of %s: %v", item.bucket, err)
		}
	}
	var registry any
	if err := s.Get("workspace_registry", "main", &registry); !errors.Is(err, ErrNotFound) {
		t.Fatalf("registry committed despite rollback: %v", err)
	}
}

func TestAtomicReadAndWriteCommitTogether(t *testing.T) {
	s := openTestStore(t)
	if err := s.Put("notes", "note", domain.Note{ID: "note", Platform: "douyin"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Atomic(func(tx *Transaction) error {
		rows, err := tx.List("notes")
		if err != nil || len(rows) != 1 {
			t.Fatalf("snapshot: %v (%d)", err, len(rows))
		}
		var value domain.Note
		if err := tx.Get("notes", "note", &value); err != nil {
			return err
		}
		if value.Platform != "douyin" {
			t.Fatal(value.Platform)
		}
		if err := tx.Delete("notes", value.ID); err != nil {
			return err
		}
		return tx.Put("deletion_cleanup", "op", map[string]string{"id": "op"})
	}); err != nil {
		t.Fatal(err)
	}
	var value any
	if err := s.Get("notes", "note", &value); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Get("deletion_cleanup", "op", &value); err != nil {
		t.Fatal(err)
	}
}

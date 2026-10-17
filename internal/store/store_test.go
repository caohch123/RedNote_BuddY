package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"xhs-workbench/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestNoteIdentitySurvivesUpdateAndRejectsDuplicate(t *testing.T) {
	s := openTestStore(t)
	note := domain.Note{ID: "first", Platform: "xiaohongshu", NoteID: "platform-note", Title: "Original"}
	if err := s.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	note.Title = "Recollected"
	if err := s.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	duplicate := note
	duplicate.ID = "second"
	if err := s.Put("notes", duplicate.ID, duplicate); err == nil {
		t.Fatal("different internal IDs must not duplicate one platform note")
	}
	duplicate.Platform = "another-platform"
	if err := s.Put("notes", duplicate.ID, duplicate); err != nil {
		t.Fatal(err)
	}
	var saved domain.Note
	if err := s.Get("notes", note.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Title != "Recollected" {
		t.Fatalf("update was lost: %+v", saved)
	}
	if err := s.Get("notes", "missing", &saved); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing record error = %v", err)
	}
}

func TestAtomicClaimOnlyReturnsEachQueuedJobOnce(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := s.Put("jobs", id, domain.Job{ID: id, Kind: "ocr", State: "queued", Phase: "queued"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put("jobs", "other", domain.Job{ID: "other", Kind: "render", State: "queued"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan *domain.Job, 12)
	errors_ := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); job, err := s.Claim("ocr"); results <- job; errors_ <- err }()
	}
	wg.Wait()
	close(results)
	close(errors_)
	for err := range errors_ {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for job := range results {
		if job == nil {
			continue
		}
		if seen[job.ID] {
			t.Fatalf("job was claimed twice: %s", job.ID)
		}
		if job.State != "running" || job.Attempts != 1 {
			t.Fatalf("claim did not persist state and attempt: %+v", job)
		}
		seen[job.ID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("claimed %d jobs, want 4", len(seen))
	}
	var other domain.Job
	if err := s.Get("jobs", "other", &other); err != nil {
		t.Fatal(err)
	}
	if other.State != "queued" {
		t.Fatalf("claim consumed another job kind: %s", other.State)
	}
}

func TestRecoveryPersistsAndNeverResubmitsUnknownPaidRequest(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "queue.db")
	s, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	jobs := []domain.Job{
		{ID: "download", Kind: "collect", State: "running", Phase: "media", Attempts: 1},
		{ID: "poll", Kind: "video", State: "running", Phase: "polling", Attempts: 1},
		{ID: "unknown", Kind: "video", State: "running", Phase: "submitting", Attempts: 1},
		{ID: "configured-later", Kind: "rewrite", State: "waiting_config", Phase: "generating"},
		{ID: "complete", Kind: "ocr", State: "succeeded", Phase: "complete"},
	}
	for _, job := range jobs {
		if err := s.Put("jobs", job.ID, job); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"download": "queued", "poll": "queued", "unknown": "submission_unknown", "configured-later": "waiting_config", "complete": "succeeded"}
	for id, state := range expected {
		var job domain.Job
		if err := s.Get("jobs", id, &job); err != nil {
			t.Fatal(err)
		}
		if job.State != state {
			t.Errorf("%s recovery state = %s, want %s", id, job.State, state)
		}
		if id == "unknown" && job.Error == "" {
			t.Error("unknown submission must explain how to recover")
		}
	}
	first, err := s.Claim("video")
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != "poll" {
		t.Fatalf("only existing polling work can resume: %+v", first)
	}
	next, err := s.Claim("video")
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatalf("unknown paid submission was claimed: %+v", next)
	}
}

func TestPublishQueueSnapshotIsAtomicAndRejectsDuplicate(t *testing.T) {
	s := openTestStore(t)
	p := domain.Publication{ID: "publication", Status: "ready", Title: "标题"}
	if e := s.Put("publications", p.ID, p); e != nil {
		t.Fatal(e)
	}
	j := domain.Job{ID: "first", Kind: "publish", EntityID: p.ID, State: "queued"}
	p.Status = "publishing"
	p.PublishJobID = j.ID
	if e := s.CreatePublishJob(p, j); e != nil {
		t.Fatal(e)
	}
	duplicate := j
	duplicate.ID = "second"
	if e := s.CreatePublishJob(p, duplicate); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate accepted: %v", e)
	}
	var saved domain.Publication
	if e := s.Get("publications", p.ID, &saved); e != nil {
		t.Fatal(e)
	}
	if saved.PublishJobID != j.ID || saved.Status != "publishing" {
		t.Fatal("snapshot and queue were not persisted together")
	}
	var orphan domain.Job
	if e := s.Get("jobs", duplicate.ID, &orphan); !errors.Is(e, ErrNotFound) {
		t.Fatal("duplicate job left an orphan")
	}
}

func TestPublishRecoveryNeverResubmitsInterruptedRequest(t *testing.T) {
	s := openTestStore(t)
	for _, phase := range []string{"queued", "validating", "submitting"} {
		j := domain.Job{ID: phase, Kind: "publish", EntityID: "pub-" + phase, State: "running", Phase: phase}
		p := domain.Publication{ID: j.EntityID, Status: "publishing", PublishJobID: j.ID}
		if e := s.Put("jobs", j.ID, j); e != nil {
			t.Fatal(e)
		}
		if e := s.Put("publications", p.ID, p); e != nil {
			t.Fatal(e)
		}
	}
	// A note ID already committed before a crash is real evidence of success.
	done := domain.Job{ID: "known-note", Kind: "publish", EntityID: "published", State: "running", Phase: "submitting"}
	p := domain.Publication{ID: done.EntityID, Status: "published", PublishJobID: done.ID, XHSNoteID: "6ac5ed550000000014039b6f"}
	s.Put("jobs", done.ID, done)
	s.Put("publications", p.ID, p)
	// An unclaimed task has not emitted a publish request and may still run.
	queued := domain.Job{ID: "unclaimed", Kind: "publish", EntityID: "unclaimed", State: "queued", Phase: "queued"}
	s.Put("jobs", queued.ID, queued)
	if e := s.Recover(); e != nil {
		t.Fatal(e)
	}
	for _, phase := range []string{"queued", "validating", "submitting"} {
		var j domain.Job
		var p domain.Publication
		s.Get("jobs", phase, &j)
		s.Get("publications", j.EntityID, &p)
		if j.State != "submission_unknown" || p.Status != "submission_unknown" || p.PublishError == "" {
			t.Fatalf("interrupted %s did not pause %+v %+v", phase, j, p)
		}
	}
	s.Get("jobs", done.ID, &done)
	if done.State != "succeeded" || done.Progress != 100 {
		t.Fatal("known platform note was not recovered")
	}
	claimed, e := s.Claim("publish")
	if e != nil || claimed == nil || claimed.ID != queued.ID {
		t.Fatal("unclaimed task not available")
	}
	if next, e := s.Claim("publish"); e != nil || next != nil {
		t.Fatal("interrupted publish was retried")
	}
}

func TestLegacyPublishPrepareRecoveryInvalidatesEditorsButKeepsUnknownPaused(t *testing.T) {
	s := openTestStore(t)
	entries := []struct{ id, kind, state, status, work string }{
		{"prepare", "publish_prepare", "running", "preparing_publish", ""},
		{"confirm", "publish_prepare", "succeeded", "awaiting_confirmation", ""},
		{"queue", "publish", "queued", "publishing", ""},
		{"unknown", "publish", "running", "publishing", ""},
		{"review", "publish", "running", "reviewing", "7123456789012345678"},
	}
	for _, v := range entries {
		j := domain.Job{ID: v.id, Kind: v.kind, State: v.state, EntityID: "pub-" + v.id, Phase: "submitting"}
		p := domain.Publication{ID: j.EntityID, TargetPlatform: "douyin", Status: v.status, PlatformWorkID: v.work}
		if v.kind == "publish_prepare" {
			p.PrepareJobID = j.ID
		} else {
			p.PublishJobID = j.ID
		}
		s.Put("jobs", j.ID, j)
		s.Put("publications", p.ID, p)
	}
	if e := s.Recover(); e != nil {
		t.Fatal(e)
	}
	for _, v := range entries {
		var j domain.Job
		var p domain.Publication
		s.Get("jobs", v.id, &j)
		s.Get("publications", "pub-"+v.id, &p)
		switch v.id {
		case "prepare":
			// A stale in-process editor can never resume after recovery.
			if j.State != "failed" || p.Status != "publish_failed" {
				t.Fatalf("stale prepare recovered %+v %+v", j, p)
			}
		case "confirm":
			if j.State != "succeeded" || p.Status != "publish_failed" {
				t.Fatalf("stale confirmation recovered %+v %+v", j, p)
			}
		case "queue":
			// An unclaimed publish request was never emitted and may still run.
			if j.State != "queued" || p.Status != "publishing" {
				t.Fatalf("unclaimed publish was disturbed %+v %+v", j, p)
			}
		case "unknown":
			if p.Status != "submission_unknown" || j.State != "submission_unknown" {
				t.Fatal("unknown would be replayed")
			}
		case "review":
			if p.Status != "reviewing" || j.State != "submission_unknown" || p.PublishedAt != "" {
				t.Fatal("reviewing conflated with published")
			}
		}
	}
}

func TestLegacyPublicationMigrationDefaultsOnlyMissingPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	s.Put("publications", "legacy", domain.Publication{ID: "legacy", Status: "exported"})
	s.Put("publications", "douyin", domain.Publication{ID: "douyin", TargetPlatform: "douyin", Status: "ready"})
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var legacy, douyin domain.Publication
	s.Get("publications", "legacy", &legacy)
	s.Get("publications", "douyin", &douyin)
	if legacy.TargetPlatform != "xiaohongshu" || legacy.Status != "exported" || douyin.TargetPlatform != "douyin" {
		t.Fatal("migration changed a platform snapshot")
	}
}

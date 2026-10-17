package app

import (
	"path/filepath"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
)

func TestJobsListReturnsServerErrorWhenStoredRecordCannotBeDecoded(t *testing.T) {
	fixture := newAPIFixture(t)
	if err := fixture.app.Store.Put("jobs", "bad", map[string]any{"kind": 42}); err != nil {
		t.Fatal(err)
	}

	response := fixture.call(t, "GET", "/jobs", nil, false)
	if response.Code != 500 || !strings.Contains(response.Body.String(), "读取任务列表失败") {
		t.Fatalf("expected explicit list failure, got HTTP %d: %s", response.Code, response.Body.String())
	}
	if _, err := readList[domain.Job](fixture.app, "jobs"); err == nil {
		t.Fatal("expected stored record decode error")
	}
}

func TestJobsListReturnsServerErrorWhenStoreReadFails(t *testing.T) {
	fixture := newAPIFixture(t)
	if err := fixture.app.Store.Close(); err != nil {
		t.Fatal(err)
	}

	response := fixture.call(t, "GET", "/jobs", nil, false)
	if response.Code != 500 || !strings.Contains(response.Body.String(), "读取任务列表失败") {
		t.Fatalf("expected explicit list failure, got HTTP %d: %s", response.Code, response.Body.String())
	}

	reopened, err := store.Open(filepath.Join(fixture.app.Data, "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.Store = reopened
}

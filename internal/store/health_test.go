package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDatabasePingReflectsClosedConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Ping(context.Background()); err == nil {
		t.Fatal("closed database reported healthy")
	}
}

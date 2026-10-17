package app

import (
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestImageCrashRecoveryUsesImageGuidance(t *testing.T) {
	f := newAPIFixture(t)
	j := domain.Job{ID: "crashed-image-submit", Kind: "image_rewrite", State: "running", Phase: "submitting"}
	if err := f.app.Store.Put("jobs", j.ID, j); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Recover(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Get("jobs", j.ID, &j); err != nil || j.State != "submission_unknown" || !strings.Contains(j.Error, "图片服务控制台") || strings.Contains(j.Error, "远程任务") {
		t.Fatalf("wrong crash state: %+v %v", j, err)
	}
}

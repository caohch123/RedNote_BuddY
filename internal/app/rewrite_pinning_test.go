package app

import (
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestRewritePinningPersistsWithoutChangingProjectContentOrRecency(t *testing.T) {
	f := seedWorkspaceFixture(t)
	r := f.xhsRewrite
	r.Title, r.TitleCustomized, r.UpdatedAt = "自定义项目", true, "2026-10-01T12:00:00Z"
	if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
		t.Fatal(err)
	}
	path := "/rewrites/" + r.ID + "?platform=xiaohongshu"
	for _, pinned := range []bool{true, true, false} {
		changed := decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", path, map[string]bool{"pinned": pinned}, true), 200)
		want := r
		want.Pinned = pinned
		if !reflect.DeepEqual(changed, want) {
			t.Fatalf("pin=%v changed project content or edit time: got %+v", pinned, changed)
		}
		var stored domain.Rewrite
		if err := f.app.Store.Get("rewrites", r.ID, &stored); err != nil || !reflect.DeepEqual(stored, want) {
			t.Fatalf("pin state was not persisted: %+v, %v", stored, err)
		}
		detail := decodeResponse[struct {
			Rewrite domain.Rewrite `json:"rewrite"`
		}](t, f.call(t, "GET", path, nil, false), 200)
		if !reflect.DeepEqual(detail.Rewrite, want) {
			t.Fatal("reloading lost the pin state")
		}
	}
}

func TestRewriteListPinsFirstAndUnpinRestoresContentRecency(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	// Insert out of content order to distinguish database writes from edits.
	for _, r := range []domain.Rewrite{
		{ID: "newest", NoteID: note.ID, UpdatedAt: "2026-10-03T12:00:00Z"},
		{ID: "oldest", NoteID: note.ID, UpdatedAt: "2026-10-01T12:00:00Z"},
		{ID: "middle", NoteID: note.ID, UpdatedAt: "2026-10-02T12:00:00Z"},
	} {
		if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	assertOrder := func(want ...string) {
		t.Helper()
		response := decodeResponse[struct {
			Items []domain.Rewrite `json:"items"`
			Total int              `json:"total"`
		}](t, f.call(t, "GET", "/rewrites?platform=xiaohongshu", nil, false), 200)
		var got []string
		for _, r := range response.Items {
			got = append(got, r.ID)
		}
		if !reflect.DeepEqual(got, want) || response.Total != len(want) {
			t.Fatalf("list order = %v (total %d), want %v", got, response.Total, want)
		}
	}
	assertOrder("newest", "middle", "oldest")
	setPin := func(id string, pinned bool) {
		t.Helper()
		decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", "/rewrites/"+id+"?platform=xiaohongshu", map[string]bool{"pinned": pinned}, true), 200)
	}
	setPin("oldest", true)
	assertOrder("oldest", "newest", "middle")
	setPin("middle", true)
	assertOrder("middle", "oldest", "newest")
	setPin("oldest", false)
	assertOrder("middle", "newest", "oldest")
	setPin("middle", false)
	assertOrder("newest", "middle", "oldest")
}

func TestRewritePinningValidatesWorkspaceCSRFAndCombinedRename(t *testing.T) {
	f := seedWorkspaceFixture(t)
	path := "/rewrites/" + f.xhsRewrite.ID + "?platform=xiaohongshu"
	for _, request := range []struct {
		path string
		body any
		auth bool
		code int
	}{
		{path, map[string]bool{"pinned": true}, false, 403},
		{"/rewrites/" + f.xhsRewrite.ID + "?platform=douyin", map[string]bool{"pinned": true}, true, 400},
		{path, map[string]any{}, true, 400},
		{path, map[string]any{"pinned": "yes"}, true, 400},
		{path, map[string]any{"pinned": true, "title": " "}, true, 400},
		{path, map[string]any{"pinned": true, "title": strings.Repeat("中", 101)}, true, 400},
	} {
		if res := f.call(t, "PATCH", request.path, request.body, request.auth); res.Code != request.code {
			t.Fatalf("PATCH %s returned %d, want %d: %s", request.path, res.Code, request.code, res.Body.String())
		}
	}
	var unchanged domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.xhsRewrite.ID, &unchanged); err != nil || !reflect.DeepEqual(unchanged, f.xhsRewrite) {
		t.Fatal("rejected pinning request changed the project")
	}
	renamed := decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", path, map[string]any{"pinned": true, "title": "  置顶项目  "}, true), 200)
	if !renamed.Pinned || !renamed.TitleCustomized || renamed.Title != "置顶项目" || renamed.UpdatedAt == "" {
		t.Fatalf("combined rename and pin did not persist: %+v", renamed)
	}
	renamed = decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", path, map[string]string{"title": "重新命名"}, true), 200)
	if !renamed.Pinned || renamed.Title != "重新命名" {
		t.Fatal("renaming reset the pin state")
	}
	var other domain.Rewrite
	if err := f.app.Store.Get("rewrites", f.dyRewrite.ID, &other); err != nil || !reflect.DeepEqual(other, f.dyRewrite) {
		t.Fatal("pinning changed another workspace")
	}
}

package app

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestRewriteSourceLimitReusesLatestAndDeleteFreesSlot(t *testing.T) {
	f := newAPIFixture(t)
	note, asset, _ := seedSource(t, f)
	path := "/rewrites?platform=xiaohongshu"
	body := map[string]string{"note_id": note.ID}
	first := decodeResponse[domain.Rewrite](t, f.call(t, "POST", path, body, true), 201)
	second := decodeResponse[domain.Rewrite](t, f.call(t, "POST", path, body, true), 201)
	if first.ID == second.ID {
		t.Fatal("first two additions must create independent projects")
	}
	first.Pinned, first.UpdatedAt = true, "2026-10-16T00:00:00Z"
	second.UpdatedAt = "2026-10-17T00:00:00Z"
	for _, r := range []domain.Rewrite{second, first} {
		if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		reused := decodeResponse[domain.Rewrite](t, f.call(t, "POST", path, body, true), 200)
		if !reflect.DeepEqual(reused, second) {
			t.Fatal("addition at the limit must open the latest project without changing it")
		}
	}
	if len(list[domain.Rewrite](f.app, "rewrites")) != 2 || len(list[domain.Job](f.app, "jobs")) != 0 {
		t.Fatal("reused project created extra records or work")
	}
	decodeResponse[deletionResponse](t, f.call(t, "DELETE", "/rewrites/"+first.ID+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	replacement := decodeResponse[domain.Rewrite](t, f.call(t, "POST", path, body, true), 201)
	if replacement.ID == first.ID || replacement.ID == second.ID || len(list[domain.Rewrite](f.app, "rewrites")) != 2 {
		t.Fatal("deleting one project did not free exactly one slot")
	}
	var saved domain.Rewrite
	var source domain.Asset
	if err := f.app.Store.Get("rewrites", second.ID, &saved); err != nil || !reflect.DeepEqual(saved, second) {
		t.Fatal("surviving project changed", err)
	}
	if err := f.app.Store.Get("assets", asset.ID, &source); err != nil {
		t.Fatal("source media was removed", err)
	}
	other := note
	other.ID, other.NoteID = "other-source", "other-platform-note"
	if err := f.app.Store.Put("notes", other.ID, other); err != nil {
		t.Fatal(err)
	}
	decodeResponse[domain.Rewrite](t, f.call(t, "POST", path, map[string]string{"note_id": other.ID}, true), 201)
}

func TestRewriteSourceLimitConcurrentAdds(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	responses := make(chan *httptest.ResponseRecorder, 10)
	for i := 0; i < cap(responses); i++ {
		go func() {
			responses <- f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true)
		}()
	}
	created := 0
	ids := map[string]bool{}
	for i := 0; i < cap(responses); i++ {
		response := <-responses
		if response.Code == 201 {
			created++
		} else if response.Code != 200 {
			t.Fatalf("concurrent addition: %d %s", response.Code, response.Body.String())
		}
		r := decodeResponse[domain.Rewrite](t, response, response.Code)
		ids[r.ID] = true
	}
	if created != 2 || len(ids) != 2 || len(list[domain.Rewrite](f.app, "rewrites")) != 2 {
		t.Fatalf("concurrent adds exceeded limit: created=%d ids=%d", created, len(ids))
	}
}

func TestRewriteSourceLimitKeepsExistingHistory(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	for _, id := range []string{"old-1", "old-2", "old-3"} {
		r := domain.Rewrite{ID: id, NoteID: note.ID, UpdatedAt: "2026-10-17T00:00:00Z"}
		if err := f.app.Store.Put("rewrites", id, r); err != nil {
			t.Fatal(err)
		}
	}
	r := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 200)
	if r.ID != "old-3" || len(list[domain.Rewrite](f.app, "rewrites")) != 3 {
		t.Fatal("legacy history was deleted or another project was added")
	}
}

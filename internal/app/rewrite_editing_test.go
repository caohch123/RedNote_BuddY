package app

import (
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestRewriteRenamePreservesVersionsSourcesAndPublications(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	r := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 201)
	r = decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites/"+r.ID+"/confirm?platform=xiaohongshu", map[string]string{"version_id": r.Versions[0].ID}, true), 200)
	p := decodeResponse[domain.Publication](t, f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]string{"rewrite_id": r.ID, "version_id": r.Versions[0].ID}, true), 201)
	url := "/rewrites/" + r.ID + "?platform=xiaohongshu"
	for _, request := range []struct {
		name string
		url  string
		auth bool
		code int
	}{
		{"未授权", url, false, 403},
		{"跨工作区", "/rewrites/" + r.ID + "?platform=douyin", true, 400},
		{"  ", url, true, 400},
		{strings.Repeat("😀", 101), url, true, 400},
	} {
		if response := f.call(t, "PATCH", request.url, map[string]string{"title": request.name}, request.auth); response.Code != request.code {
			t.Fatalf("rename status %d, want %d", response.Code, request.code)
		}
	}
	name := strings.Repeat("个人项目", 6)
	renamed := decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", url, map[string]string{"title": "  " + name + "  "}, true), 200)
	if renamed.Title != name || !renamed.TitleCustomized || !reflect.DeepEqual(renamed.Versions, r.Versions) || renamed.ConfirmedVersionID != r.ConfirmedVersionID {
		t.Fatal("renaming changed version content or confirmation")
	}
	var loadedNote domain.Note
	var loadedPublication domain.Publication
	if err := f.app.Store.Get("notes", note.ID, &loadedNote); err != nil || !reflect.DeepEqual(loadedNote, note) {
		t.Fatal("renaming changed source note")
	}
	if err := f.app.Store.Get("publications", p.ID, &loadedPublication); err != nil || !reflect.DeepEqual(loadedPublication, p) {
		t.Fatal("renaming changed publication snapshot")
	}
	decodeResponse[domain.Version](t, f.call(t, "POST", "/rewrites/"+r.ID+"/versions?platform=xiaohongshu", domain.Version{Title: "新的笔记标题", Body: "手动编辑正文", AssetIDs: note.AssetIDs}, true), 201)
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", r.ID, &saved); err != nil || saved.Title != name || len(saved.Versions) != 2 {
		t.Fatal("saving a version overwrote the custom project name")
	}
	items := decodeResponse[struct {
		Items []domain.Rewrite `json:"items"`
	}](t, f.call(t, "GET", "/rewrites?platform=xiaohongshu", nil, false), 200)
	if len(items.Items) != 1 || items.Items[0].Title != name {
		t.Fatal("project list did not persist the custom name")
	}
}

func TestXHSTitleLimitAtSaveConfirmationAndPublication(t *testing.T) {
	f := newAPIFixture(t)
	note, _, _ := seedSource(t, f)
	note.Title = strings.Repeat("长", 25)
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	r := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 201)
	if r.Versions[0].Title != note.Title {
		t.Fatal("source title was silently truncated")
	}
	confirmURL := "/rewrites/" + r.ID + "/confirm?platform=xiaohongshu"
	if response := f.call(t, "POST", confirmURL, map[string]string{"version_id": r.Versions[0].ID}, true); response.Code != 400 {
		t.Fatal("confirmation accepted an overlong imported title")
	}
	// Older confirmed versions must not bypass validation through publication creation.
	r.Versions[0].Confirmed = true
	if err := f.app.Store.Put("rewrites", r.ID, r); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]string{"rewrite_id": r.ID, "version_id": r.Versions[0].ID}, true); response.Code != 400 {
		t.Fatal("publication accepted an overlong historical title")
	}
	for _, request := range []struct {
		title string
		code  int
	}{
		{strings.Repeat("中", 20), 201}, {strings.Repeat("中", 21), 400},
		{strings.Repeat("😀", 20), 201}, {strings.Repeat("😀", 21), 400},
		{strings.Repeat("中", 19) + "!", 201}, {strings.Repeat("中", 20) + " ", 400}, {"  ", 400},
	} {
		response := f.call(t, "POST", "/rewrites/"+r.ID+"/versions?platform=xiaohongshu", domain.Version{Title: request.title, Body: "正文", AssetIDs: note.AssetIDs}, true)
		if response.Code != request.code {
			t.Fatalf("save status %d, want %d: %s", response.Code, request.code, response.Body.String())
		}
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", r.ID, &saved); err != nil || len(saved.Versions) != 4 || saved.Versions[0].Title != note.Title {
		t.Fatal("invalid save mutated history")
	}
	if len(list[domain.Publication](f.app, "publications")) != 0 {
		t.Fatal("invalid title created a publication")
	}
	// The retired Douyin workspace no longer has separate title rules.
	note.Platform = "douyin"
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, "POST", "/rewrites/"+r.ID+"/versions?platform=douyin", domain.Version{Title: strings.Repeat("中", 25), Body: "正文", AssetIDs: note.AssetIDs}, true); response.Code != 400 {
		t.Fatal("retired workspace remained selectable", response.Code)
	}
	var after domain.Rewrite
	if err := f.app.Store.Get("rewrites", r.ID, &after); err != nil || len(after.Versions) != 4 {
		t.Fatal("rejected save mutated history")
	}
}

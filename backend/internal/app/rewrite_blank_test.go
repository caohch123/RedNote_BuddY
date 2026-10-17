package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

type blankRewriteDetail struct {
	Rewrite domain.Rewrite        `json:"rewrite"`
	Note    domain.Note           `json:"note"`
	Assets  []domain.Asset        `json:"assets"`
	Videos  []domain.VideoProject `json:"videos"`
}

func createBlankFixture(t *testing.T, f apiFixture) domain.Rewrite {
	t.Helper()
	return decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]bool{"blank": true}, true), 201)
}

func TestBlankRewriteStartsIndependentWithNoSourceOrWork(t *testing.T) {
	f := newAPIFixture(t)
	note, asset, _ := seedSource(t, f)
	ids := map[string]bool{}
	for i := 0; i < 3; i++ {
		r := createBlankFixture(t, f)
		if ids[r.ID] || !r.Blank || r.NoteID != "" || len(r.Versions) != 1 || r.ConfirmedVersionID != "" {
			t.Fatalf("blank project is not independent: %+v", r)
		}
		ids[r.ID] = true
		v := r.Versions[0]
		if v.Title != "" || v.Body != "" || v.Script != "" || len(v.Topics)+len(v.AssetIDs)+len(v.SourceExtractions) != 0 || v.SourceSnapshot != nil {
			t.Fatalf("blank copy inherited content: %+v", v)
		}
		if len(r.ImageRewrites) != 1 {
			t.Fatal("blank project must offer an empty image scheme")
		}
		u := r.ImageRewrites[0]
		if u.PrimaryAssetID != "" || u.Prompt != "" || u.ExtraRequirements != "" || len(u.References)+len(u.SourceAssetIDs)+len(u.FeatureSources)+len(u.Analyses)+len(u.Results) != 0 {
			t.Fatalf("image scheme inherited references: %+v", u)
		}
		for _, control := range u.FeatureControls {
			if control.Mode != "auto" || control.AssetID != "" || control.Value != "" {
				t.Fatalf("image control inherited a source: %+v", control)
			}
		}
		path := "/rewrites/" + r.ID
		detail := decodeResponse[blankRewriteDetail](t, f.call(t, "GET", path+"?platform=xiaohongshu", nil, false), 200)
		if detail.Note.ID != "" || detail.Note.Title != "" || detail.Note.Body != "" || len(detail.Note.AssetIDs)+len(detail.Note.Topics)+len(detail.Assets) != 0 || len(detail.Videos) != 1 {
			t.Fatalf("blank detail inherited source content: %+v", detail)
		}
		video := detail.Videos[0].VideoRewrite
		if video == nil || video.PrimaryAssetID != "" || video.InitialSourceAssetID != "" || video.ReferenceVideoID != "" || video.ReferenceImageID != "" || video.Prompt != "" || video.Requirements != "" || len(video.PurposeSources)+len(video.Analyses)+len(video.Results) != 0 {
			t.Fatalf("video scheme inherited references: %+v", video)
		}
		for repeat := 0; repeat < 2; repeat++ {
			images := decodeResponse[[]domain.ImageRewrite](t, f.call(t, "POST", path+"/images/initialize?platform=xiaohongshu", nil, true), 200)
			videos := decodeResponse[[]domain.VideoProject](t, f.call(t, "POST", path+"/videos/initialize?platform=xiaohongshu", nil, true), 200)
			if len(images) != 1 || images[0].ID != u.ID || len(videos) != 1 || videos[0].ID != detail.Videos[0].ID {
				t.Fatal("reinitializing duplicated or replaced empty schemes")
			}
		}
	}
	if len(list[domain.Note](f.app, "notes")) != 1 || len(list[domain.Asset](f.app, "assets")) != 1 || len(list[domain.Job](f.app, "jobs")) != 0 {
		t.Fatal("blank creation created placeholder collections, copied assets, or model work")
	}
	var savedNote domain.Note
	var savedAsset domain.Asset
	if f.app.Store.Get("notes", note.ID, &savedNote) != nil || !reflect.DeepEqual(savedNote, note) || f.app.Store.Get("assets", asset.ID, &savedAsset) != nil || !reflect.DeepEqual(savedAsset, asset) {
		t.Fatal("blank creation modified an existing source")
	}
}

func TestBlankRewriteCanSaveConfirmAndUseItsOwnMedia(t *testing.T) {
	f := newAPIFixture(t)
	_, source, _ := seedSource(t, f)
	r, other := createBlankFixture(t, f), createBlankFixture(t, f)
	path := "/rewrites/" + r.ID
	unit := r.ImageRewrites[0]
	imagePath := path + "/images/" + unit.ID + "?platform=xiaohongshu"
	if response := f.call(t, "PATCH", imagePath, map[string]any{"revision": unit.Revision, "primary_asset_id": source.ID}, true); response.Code != 400 {
		t.Fatalf("blank project accepted another source's private media: %d %s", response.Code, response.Body.String())
	}
	owned := source
	owned.ID, owned.OwnerID, owned.Extraction = "blank-owned", r.ID, domain.Extraction{}
	if err := f.app.Store.Put("assets", owned.ID, owned); err != nil {
		t.Fatal(err)
	}
	unit = decodeResponse[domain.ImageRewrite](t, f.call(t, "PATCH", imagePath, map[string]any{"revision": unit.Revision, "primary_asset_id": owned.ID}, true), 200)
	if unit.PrimaryAssetID != owned.ID {
		t.Fatal("empty scheme could not accept its own uploaded media")
	}
	decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", path+"?platform=xiaohongshu", map[string]string{"title": "独立创作项目"}, true), 200)
	v := decodeResponse[domain.Version](t, f.call(t, "POST", path+"/versions?platform=xiaohongshu", domain.Version{Title: "自己填写的标题", Body: "自己填写的正文", Topics: []string{"创作"}, AssetIDs: []string{owned.ID}}, true), 201)
	if v.SourceSnapshot != nil || len(v.SourceExtractions) != 0 || v.Title != "自己填写的标题" || v.Number != 2 {
		t.Fatalf("blank save retained source provenance or lost copy: %+v", v)
	}
	confirmed := decodeResponse[domain.Rewrite](t, f.call(t, "POST", path+"/confirm?platform=xiaohongshu", map[string]string{"version_id": v.ID}, true), 200)
	if confirmed.ConfirmedVersionID != v.ID || confirmed.Title != "独立创作项目" {
		t.Fatal("blank save or confirm changed custom project metadata")
	}
	p := decodeResponse[domain.Publication](t, f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]any{"rewrite_id": r.ID, "version_id": v.ID, "asset_ids": []string{owned.ID}}, true), 201)
	if p.RewriteID != r.ID {
		t.Fatal("blank project cannot prepare a publication draft")
	}
	detail := decodeResponse[blankRewriteDetail](t, f.call(t, "GET", path+"?platform=xiaohongshu", nil, false), 200)
	if len(detail.Assets) != 1 || detail.Assets[0].ID != owned.ID {
		t.Fatal("blank detail included foreign media or omitted its own")
	}
	decodeResponse[deletionResponse](t, f.call(t, "DELETE", path+"?platform=xiaohongshu", map[string]bool{"confirm": true}, true), 200)
	deletionAssertExists(t, f.app, "rewrites", other.ID, true)
	deletionAssertExists(t, f.app, "assets", source.ID, true)
	deletionAssertExists(t, f.app, "videos", detail.Videos[0].ID, false)
}

func TestBlankRewriteValidationDoesNotCreateRecordsOrJobs(t *testing.T) {
	f := newAPIFixture(t)
	for _, request := range []struct {
		path string
		body map[string]any
		auth bool
		code int
	}{
		{"/rewrites?platform=xiaohongshu", map[string]any{"blank": true}, false, 403},
		{"/rewrites?platform=douyin", map[string]any{"blank": true}, true, 400},
		{"/rewrites?platform=xiaohongshu", map[string]any{"blank": true, "note_id": "source"}, true, 400},
	} {
		if response := f.call(t, "POST", request.path, request.body, request.auth); response.Code != request.code {
			t.Fatalf("invalid blank request: %d %s", response.Code, response.Body.String())
		}
	}
	if len(list[domain.Rewrite](f.app, "rewrites"))+len(list[domain.VideoProject](f.app, "videos")) != 0 {
		t.Fatal("rejected creation left project records")
	}
	r := createBlankFixture(t, f)
	if response := f.call(t, "POST", "/rewrites/"+r.ID+"/generate?platform=xiaohongshu", domain.RewriteOptions{}, true); response.Code != 400 {
		t.Fatalf("empty creation requirements accepted: %d %s", response.Code, response.Body.String())
	}
	if len(list[domain.Job](f.app, "jobs")) != 0 {
		t.Fatal("empty generation enqueued model work")
	}
}

func TestBlankRewriteGeneratesFromUserRequirementsWithoutOldSources(t *testing.T) {
	f := newAPIFixture(t)
	seedSource(t, f)
	r := createBlankFixture(t, f)
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Messages) != 2 {
			t.Error("invalid model request")
			w.WriteHeader(400)
			return
		}
		var input map[string]any
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		requests <- input
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"title":"春日散步","body":"公园散步的原创内容","topics":["春天"],"script":""}`}}}})
	}))
	defer server.Close()
	if err := f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{"text": {BaseURL: server.URL, Model: "fixture-model", APIKey: "fixture-secret"}}}); err != nil {
		t.Fatal(err)
	}
	decodeResponse[map[string]string](t, f.call(t, "POST", "/rewrites/"+r.ID+"/generate?platform=xiaohongshu", domain.RewriteOptions{Instructions: "写一篇春日在公园散步的笔记", IncludeBody: true}, true), 202)
	job, err := f.app.Store.Claim("rewrite")
	if err != nil || job == nil {
		t.Fatalf("claim blank generation: %v", err)
	}
	f.app.runJob(job)
	if job.State != "succeeded" {
		t.Fatalf("blank generation failed: %+v", job)
	}
	input := <-requests
	if input["source_title"] != "" || input["source_body"] != "" || input["requirements"] != "写一篇春日在公园散步的笔记" || len(input["extractions"].([]any)) != 0 {
		t.Fatalf("model input contained old references: %+v", input)
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", r.ID, &saved); err != nil {
		t.Fatal(err)
	}
	v := saved.Versions[len(saved.Versions)-1]
	if len(saved.Versions) != 2 || v.Title != "春日散步" || v.SourceSnapshot != nil || len(v.SourceExtractions)+len(v.AssetIDs) != 0 {
		t.Fatalf("generated blank version acquired source references: %+v", v)
	}
}

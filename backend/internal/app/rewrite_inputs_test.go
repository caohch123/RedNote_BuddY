package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestRewriteGenerationUsesCompleteLatestSourceAndAllExtractions(t *testing.T) {
	f := newAPIFixture(t)
	note, first, _ := seedSource(t, f)
	note.Body = strings.Repeat("完整正文不能只发送预览。", 1000) + "正文末尾"
	first.Extraction = domain.Extraction{Text: "人工修正的完整 OCR", Edited: true, Status: "failed"}
	longOCR := first
	longOCR.ID = "long-ocr"
	longOCR.Extraction = domain.Extraction{Text: strings.Repeat("长图片文字。", 1000) + "OCR 末尾", Status: "succeeded"}
	transcript := first
	transcript.ID = "segment-transcript"
	transcript.Extraction = domain.Extraction{Status: "succeeded", Segments: []domain.Segment{{Text: "第一句口播", Start: 0, End: 2}, {Text: "第二句口播", Start: 2, End: 4}}}
	cover := first
	cover.ID = "source-cover"
	cover.Extraction = domain.Extraction{Text: "封面文字", Status: "succeeded"}
	late := first
	late.ID = "late-ocr"
	late.Extraction = domain.Extraction{Status: "running"}
	failed := first
	failed.ID = "failed-ocr"
	failed.Extraction = domain.Extraction{Text: "失败残留不应进入输入", Status: "failed"}
	foreign := first
	foreign.ID, foreign.OwnerID = "foreign-ocr", "other-note"
	foreign.Extraction = domain.Extraction{Text: "其他笔记不应进入输入", Status: "succeeded"}
	for _, asset := range []domain.Asset{first, longOCR, transcript, cover, late, failed, foreign} {
		if err := f.app.Store.Put("assets", asset.ID, asset); err != nil {
			t.Fatal(err)
		}
	}
	note.AssetIDs = []string{first.ID, longOCR.ID, transcript.ID, late.ID, failed.ID, first.ID}
	note.CoverAssetID = cover.ID
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	rewrite := decodeResponse[domain.Rewrite](t, f.call(t, "POST", "/rewrites?platform=xiaohongshu", map[string]string{"note_id": note.ID}, true), 201)
	url := "/rewrites/" + rewrite.ID + "/generate?platform=xiaohongshu"
	if response := f.call(t, "POST", url, domain.RewriteOptions{ExtractionAssetIDs: []string{foreign.ID}}, true); response.Code != 400 {
		t.Fatal("foreign extraction was accepted")
	}
	decodeResponse[map[string]string](t, f.call(t, "POST", url, domain.RewriteOptions{IncludeBody: false, ExtractionAssetIDs: []string{}}, true), 202)
	job, err := f.app.Store.Claim("rewrite")
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	var queued domain.RewriteOptions
	if err := json.Unmarshal(job.Payload, &queued); err != nil || !queued.IncludeBody || len(queued.ExtractionAssetIDs) != 4 {
		t.Fatalf("old selection not normalized: %+v; %v", queued, err)
	}
	// OCR completion and a corrected source after queuing must reach the model.
	note.Body += "\n排队后修正的正文"
	late.Extraction = domain.Extraction{Text: "排队后完成的 OCR", Status: "succeeded"}
	if err := f.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("assets", late.ID, late); err != nil {
		t.Fatal(err)
	}
	type sourceInput struct {
		Title       string   `json:"source_title"`
		Body        string   `json:"source_body"`
		Extractions []string `json:"extractions"`
	}
	requests := make(chan sourceInput, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 2 {
			t.Error("invalid rewrite request")
			w.WriteHeader(400)
			return
		}
		var input sourceInput
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		requests <- input
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"title":"新标题","body":"新正文","topics":[],"script":"新口播"}`}}}})
	}))
	defer server.Close()
	if err := f.app.saveSettings(domain.Settings{Providers: map[string]domain.ProviderConfig{"text": {BaseURL: server.URL, Model: "fixture-model", APIKey: "fixture-secret"}}}); err != nil {
		t.Fatal(err)
	}
	decodeResponse[domain.Rewrite](t, f.call(t, "PATCH", "/rewrites/"+rewrite.ID+"?platform=xiaohongshu", map[string]string{"title": "我的自定义项目"}, true), 200)
	f.app.runJob(job)
	if job.State != "succeeded" {
		t.Fatalf("rewrite failed: %+v", job)
	}
	input := <-requests
	wantTexts := []string{first.Extraction.Text, longOCR.Extraction.Text, "第一句口播\n第二句口播", late.Extraction.Text, cover.Extraction.Text}
	if input.Title != note.Title || input.Body != note.Body || !reflect.DeepEqual(input.Extractions, wantTexts) {
		t.Fatal("complete source or extraction text was omitted, truncated, or contaminated")
	}
	var saved domain.Rewrite
	if err := f.app.Store.Get("rewrites", rewrite.ID, &saved); err != nil {
		t.Fatal(err)
	}
	version := saved.Versions[len(saved.Versions)-1]
	if len(saved.Versions) != 2 || version.SourceSnapshot.Body != note.Body || len(version.SourceExtractions) != 5 || !saved.Options.IncludeBody {
		t.Fatal("source provenance was not retained with the new version")
	}
	if saved.Title != "我的自定义项目" || !saved.TitleCustomized {
		t.Fatal("AI generation overwrote the custom project name")
	}
}

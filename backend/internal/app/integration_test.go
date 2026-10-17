package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"xhs-workbench/internal/domain"
)

type apiFixture struct {
	app    *App
	router http.Handler
}

func newAPIFixture(t *testing.T) apiFixture {
	t.Helper()
	root := t.TempDir()
	a, err := New(root, "data", "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	return apiFixture{app: a, router: a.Router()}
}

func (fixture apiFixture) call(t *testing.T, method, target string, body any, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:8080/api/v1"+target, reader)
	req.Header.Set("Content-Type", "application/json")
	if authorized {
		req.Header.Set("X-CSRF-Token", fixture.app.CSRF)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, req)
	return response
}

func decodeResponse[T any](t *testing.T, response *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if response.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid response JSON: %v; %s", err, response.Body.String())
	}
	return result
}

func seedSource(t *testing.T, fixture apiFixture) (domain.Note, domain.Asset, []byte) {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 240, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	local := filepath.Join("assets", "fixture-image.png")
	if err := os.WriteFile(filepath.Join(fixture.app.Data, local), data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	asset := domain.Asset{ID: "asset-fixture", OwnerID: "note-fixture", Kind: "image", LocalPath: local, MIME: "image/png", Status: "ready", SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data)), Position: 0, Extraction: domain.Extraction{Status: "succeeded", Text: "OCR fixture"}}
	note := domain.Note{ID: "note-fixture", Platform: "xiaohongshu", NoteID: "platform-fixture", URL: "https://www.xiaohongshu.com/explore/platform-fixture", Title: "原始标题", Body: "原始内容，供集成测试使用。", Topics: []string{"测试"}, MetadataStatus: "succeeded", MediaStatus: "succeeded", Status: "ready", AssetIDs: []string{asset.ID}, Tags: []string{"fixture"}, Author: domain.Author{Name: "Test author"}}
	if err := fixture.app.Store.Put("assets", asset.ID, asset); err != nil {
		t.Fatal(err)
	}
	if err := fixture.app.Store.Put("notes", note.ID, note); err != nil {
		t.Fatal(err)
	}
	return note, asset, data
}

func TestSessionCSRFAndOriginBoundary(t *testing.T) {
	fixture := newAPIFixture(t)
	session := decodeResponse[map[string]string](t, fixture.call(t, "GET", "/session", nil, false), 200)
	if session["csrf_token"] == "" || session["csrf_token"] != fixture.app.CSRF {
		t.Fatal("session must return the active CSRF token")
	}
	if response := fixture.call(t, "PATCH", "/settings", map[string]any{}, false); response.Code != 403 {
		t.Fatalf("write without token returned %d", response.Code)
	}
	decodeResponse[domain.Settings](t, fixture.call(t, "PATCH", "/settings", map[string]any{}, true), 200)
	for _, scenario := range []struct{ host, origin string }{{"attacker.example", ""}, {"127.0.0.1:8080", "https://attacker.example"}} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/session", nil)
		req.Host = scenario.host
		req.Header.Set("Origin", scenario.origin)
		response := httptest.NewRecorder()
		fixture.router.ServeHTTP(response, req)
		if response.Code != 403 {
			t.Fatalf("untrusted host/origin was accepted: %+v status %d", scenario, response.Code)
		}
	}
}

func TestWindowsSecretStorageAndMasks(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI integration requires Windows")
	}
	fixture := newAPIFixture(t)
	const cookie = "a1=integration-secret-cookie"
	const key = "integration-secret-key"
	body := domain.Settings{Cookie: cookie, Providers: map[string]domain.ProviderConfig{"text": {APIKey: key}}}
	response := fixture.call(t, "PATCH", "/settings", body, true)
	settings := decodeResponse[domain.Settings](t, response, 200)
	if strings.Contains(response.Body.String(), cookie) || strings.Contains(response.Body.String(), key) || !settings.CookieConfigured || !settings.Providers["text"].Configured {
		t.Fatal("settings response leaked secrets or lost configured state")
	}
	var raw domain.Settings
	if err := fixture.app.Store.Get("settings", "main", &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw.Cookie, "dpapi:") || !strings.HasPrefix(raw.Providers["text"].APIKey, "dpapi:") || strings.Contains(raw.Cookie, cookie) || strings.Contains(raw.Providers["text"].APIKey, key) {
		t.Fatal("secrets were not encrypted at rest")
	}
	decrypted, err := fixture.app.settings()
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.Cookie != cookie || decrypted.Providers["text"].APIKey != key {
		t.Fatal("DPAPI round trip changed secret values")
	}
	decodeResponse[domain.Settings](t, fixture.call(t, "PATCH", "/settings", domain.Settings{Cookie: "", Providers: map[string]domain.ProviderConfig{"text": {APIKey: "", Model: "configured-model"}}}, true), 200)
	decrypted, err = fixture.app.settings()
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.Cookie != cookie || decrypted.Providers["text"].APIKey != key {
		t.Fatal("empty secret update erased saved credentials")
	}
	listed := fixture.call(t, "GET", "/settings", nil, false)
	if strings.Contains(listed.Body.String(), cookie) || strings.Contains(listed.Body.String(), key) {
		t.Fatal("GET settings exposed a secret")
	}
	decodeResponse[domain.Settings](t, fixture.call(t, "PATCH", "/settings", domain.Settings{Cookie: "__CLEAR__", Providers: map[string]domain.ProviderConfig{"text": {APIKey: "__CLEAR__"}}}, true), 200)
	decrypted, err = fixture.app.settings()
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.Cookie != "" || decrypted.Providers["text"].APIKey != "" {
		t.Fatal("explicit credential removal did not work")
	}
}

func TestManualRewritePublicationSnapshotAndExport(t *testing.T) {
	fixture := newAPIFixture(t)
	note, asset, media := seedSource(t, fixture)
	rewrite := decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites", map[string]string{"note_id": note.ID}, true), 201)
	if len(rewrite.Versions) != 1 || rewrite.Versions[0].Body != note.Body {
		t.Fatal("rewrite lost source context")
	}
	version := decodeResponse[domain.Version](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/versions", domain.Version{Title: "确认稿标题", Body: "确认稿正文", Topics: []string{"新话题"}, Script: "确认口播", AssetIDs: []string{asset.ID}}, true), 201)
	if version.Number != 2 || version.Confirmed {
		t.Fatal("new manual version must be distinct and unconfirmed")
	}
	if response := fixture.call(t, "POST", "/publications", map[string]string{"rewrite_id": rewrite.ID, "version_id": version.ID}, true); response.Code != 409 {
		t.Fatal("unconfirmed version was publishable")
	}
	decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/confirm", map[string]string{"version_id": version.ID}, true), 200)
	publication := decodeResponse[domain.Publication](t, fixture.call(t, "POST", "/publications", map[string]string{"rewrite_id": rewrite.ID, "version_id": version.ID}, true), 201)
	if publication.Status != "ready" || publication.VersionID != version.ID {
		t.Fatalf("bad publication: %+v", publication)
	}
	newer := decodeResponse[domain.Version](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/versions", domain.Version{Title: "后续修改", Body: "之后的正文", Topics: []string{"另一话题"}, AssetIDs: []string{asset.ID}}, true), 201)
	decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/confirm", map[string]string{"version_id": newer.ID}, true), 200)
	snapshot := decodeResponse[domain.Publication](t, fixture.call(t, "GET", "/publications/"+publication.ID, nil, false), 200)
	if snapshot.Title != version.Title || snapshot.Body != version.Body || snapshot.VersionID != version.ID {
		t.Fatal("publication snapshot was overwritten by later edits")
	}
	var original domain.Note
	if err := fixture.app.Store.Get("notes", note.ID, &original); err != nil {
		t.Fatal(err)
	}
	if original.Title != note.Title || original.Body != note.Body {
		t.Fatal("rewriting changed original note")
	}
	if response := fixture.call(t, "GET", "/publications/"+publication.ID+"/export", nil, false); response.Code != 403 {
		t.Fatal("stateful export must require CSRF")
	}
	exported := fixture.call(t, "GET", "/publications/"+publication.ID+"/export", nil, true)
	if exported.Code != 200 {
		t.Fatalf("export failed: %s", exported.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(exported.Body.Bytes()), int64(exported.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = data
	}
	if string(entries["标题.txt"]) != version.Title || !strings.Contains(string(entries["文案.txt"]), "#新话题") || !bytes.Equal(entries["01_image.png"], media) {
		t.Fatalf("ZIP did not preserve confirmed text or media: %v", entries)
	}
	var manifest struct {
		VersionID string `json:"rewrite_version_id"`
		Assets    []struct {
			AssetID string `json:"asset_id"`
			SHA256  string `json:"sha256"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.VersionID != version.ID || len(manifest.Assets) != 1 || manifest.Assets[0].AssetID != asset.ID || manifest.Assets[0].SHA256 != asset.SHA256 {
		t.Fatal("export manifest points to another version or asset")
	}
	snapshot = decodeResponse[domain.Publication](t, fixture.call(t, "GET", "/publications/"+publication.ID, nil, false), 200)
	if snapshot.Status != "exported" || snapshot.PublishedAt != "" || snapshot.PublishedURL != "" {
		t.Fatal("export falsely marked a publication as published")
	}
	published := decodeResponse[domain.Publication](t, fixture.call(t, "PATCH", "/publications/"+publication.ID, map[string]string{"status": "published", "published_url": "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f", "published_at": "2026-10-06T13:00:00Z"}, true), 200)
	if published.Status != "published" {
		t.Fatal("manual publish record was not saved")
	}
	if response := fixture.call(t, "GET", "/publications/"+publication.ID+"/export", nil, true); response.Code != 200 {
		t.Fatal("re-export failed")
	}
	snapshot = decodeResponse[domain.Publication](t, fixture.call(t, "GET", "/publications/"+publication.ID, nil, false), 200)
	if snapshot.Status != "published" || snapshot.PublishedAt != published.PublishedAt {
		t.Fatal("re-export reset real publication state")
	}
}

func TestMediaRangeAndPathEscape(t *testing.T) {
	fixture := newAPIFixture(t)
	_, asset, data := seedSource(t, fixture)
	req := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/assets/"+asset.ID+"/file", nil)
	req.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, req)
	if response.Code != 206 || !bytes.Equal(response.Body.Bytes(), data[2:6]) || response.Header().Get("Content-Range") == "" {
		t.Fatalf("media range failed: %d %v", response.Code, response.Header())
	}
	outside := filepath.Join(filepath.Dir(fixture.app.Data), "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, local := range []string{outside, filepath.Join("..", "outside-secret.txt")} {
		unsafe := asset
		unsafe.ID = ID()
		unsafe.LocalPath = local
		if err := fixture.app.Store.Put("assets", unsafe.ID, unsafe); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.app.assetPath(unsafe); err == nil {
			t.Fatal("assetPath accepted an escaped path")
		}
		if response := fixture.call(t, "GET", "/assets/"+unsafe.ID+"/file", nil, false); response.Code != 404 || strings.Contains(response.Body.String(), "outside") {
			t.Fatal("escaped asset was exposed")
		}
	}
	symlink := filepath.Join(fixture.app.Data, "assets", "outside-link.txt")
	if err := os.Symlink(outside, symlink); err == nil {
		unsafe := asset
		unsafe.LocalPath = symlink
		if _, err := fixture.app.assetPath(unsafe); err == nil {
			t.Fatal("assetPath followed a symlink outside data")
		}
	}
	assets := fixture.call(t, "GET", "/assets", nil, false)
	if strings.Contains(assets.Body.String(), fixture.app.Data) || strings.Contains(assets.Body.String(), "outside-secret.txt") {
		t.Fatal("asset listing exposed local file paths")
	}
}

func TestMissingKeyWaitsWithoutCreatingFakeVersion(t *testing.T) {
	fixture := newAPIFixture(t)
	note, _, _ := seedSource(t, fixture)
	rewrite := decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites", map[string]string{"note_id": note.ID}, true), 201)
	result := decodeResponse[map[string]string](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/generate", domain.RewriteOptions{IncludeBody: true}, true), 202)
	job, err := fixture.app.Store.Claim("rewrite")
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != result["job_id"] {
		t.Fatal("queued rewrite was not claimable")
	}
	fixture.app.runJob(job)
	var saved domain.Job
	if err := fixture.app.Store.Get("jobs", job.ID, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.State != "waiting_config" || saved.Error == "" || saved.Progress == 100 {
		t.Fatalf("missing API key was presented as success: %+v", saved)
	}
	var updated domain.Rewrite
	if err := fixture.app.Store.Get("rewrites", rewrite.ID, &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Versions) != 1 {
		t.Fatal("no-key job invented a rewrite version")
	}
	jobJSON := fixture.call(t, "GET", "/jobs/"+job.ID, nil, false)
	if strings.Contains(jobJSON.Body.String(), "payload") {
		t.Fatal("internal job input leaked through public API")
	}
}

func TestQueueDeduplicatesConcurrentActiveCollectionWork(t *testing.T) {
	fixture := newAPIFixture(t)
	note, _, _ := seedSource(t, fixture)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errors_ := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := fixture.app.enqueue("collect", note.ID, "", "", map[string]string{"url": note.URL})
			ids <- job.ID
			errors_ <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errors_)
	for err := range errors_ {
		if err != nil {
			t.Fatal(err)
		}
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent enqueue created %d duplicate jobs", len(unique))
	}
	received := decodeResponse[struct {
		Existing []string `json:"existing_ids"`
		Jobs     []string `json:"job_ids"`
	}](t, fixture.call(t, "POST", "/collections", map[string]string{"text": "分享文字 " + note.URL + "\n" + note.URL}, true), 202)
	if len(received.Existing) != 1 || received.Existing[0] != note.ID || len(received.Jobs) != 0 {
		t.Fatal("repeat URL did not locate the existing note")
	}

}

func TestTextOnlyPublicationExportsConfirmedSnapshotWithoutPublishing(t *testing.T) {
	fixture := newAPIFixture(t)
	note, _, _ := seedSource(t, fixture)
	rewrite := decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites", map[string]string{"note_id": note.ID}, true), 201)
	version := decodeResponse[domain.Version](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/versions", domain.Version{Title: "纯文字确认稿", Body: "可以先导出文案，再准备图片。", Topics: []string{"文字测试"}, AssetIDs: []string{}}, true), 201)
	decodeResponse[domain.Rewrite](t, fixture.call(t, "POST", "/rewrites/"+rewrite.ID+"/confirm", map[string]string{"version_id": version.ID}, true), 200)
	publication := decodeResponse[domain.Publication](t, fixture.call(t, "POST", "/publications", map[string]string{"rewrite_id": rewrite.ID, "version_id": version.ID}, true), 201)
	if len(publication.AssetIDs) != 0 || publication.Status != "preparing" {
		t.Fatal("text-only draft must accurately report missing platform media")
	}
	response := fixture.call(t, "GET", "/publications/"+publication.ID+"/export", nil, true)
	if response.Code != 200 {
		t.Fatalf("text-only export failed: %s", response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = string(data)
	}
	if entries["标题.txt"] != version.Title || !strings.Contains(entries["文案.txt"], version.Body) || !strings.Contains(entries["manifest.json"], version.ID) || len(entries) != 3 {
		t.Fatalf("invalid text-only ZIP entries: %+v", entries)
	}
	publication = decodeResponse[domain.Publication](t, fixture.call(t, "GET", "/publications/"+publication.ID, nil, false), 200)
	if publication.Status != "exported" || publication.PublishedAt != "" || publication.PublishedURL != "" {
		t.Fatal("text-only export falsely marked published")
	}
}

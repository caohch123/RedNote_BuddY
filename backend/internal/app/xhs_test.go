package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
	"xhs-workbench/internal/store"
)

type fakeXHSRPC struct {
	mu            sync.Mutex
	profile       xhsAccount
	status        string
	available     bool
	methods       []string
	publishResult any
	publishErr    error
	loginResult   any
	fn            func(string, any) (any, error)
}

func (f *fakeXHSRPC) Close() error { return nil }
func (f *fakeXHSRPC) Call(ctx context.Context, method string, params any, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods = append(f.methods, method)
	var value any
	var err error
	if f.fn != nil {
		value, err = f.fn(method, params)
	}
	if value == nil && err == nil {
		switch method {
		case "health":
			value = map[string]any{"available": f.available}
		case "account_status", "account_restore":
			value = xhsAccountStatus{Status: f.status, Account: &f.profile}
		case "account_export":
			value = map[string]any{"session": "opaque-cookie-secret", "account": f.profile}
		case "login_start", "login_status":
			value = f.loginResult
		case "logout":
			f.status = "logged_out"
			value = map[string]any{"ok": true}
		case "publish":
			value = f.publishResult
			err = f.publishErr
		default:
			err = errors.New("unsupported fake method")
		}
	}
	if err != nil {
		return err
	}
	if result != nil {
		b, e := json.Marshal(value)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, result)
	}
	return nil
}
func (f *fakeXHSRPC) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.methods {
		if m == method {
			n++
		}
	}
	return n
}

func seedXHSFixture(t *testing.T) (apiFixture, *fakeXHSRPC, domain.Publication) {
	t.Helper()
	f := newAPIFixture(t)
	rpc := &fakeXHSRPC{profile: xhsAccount{ID: "account-fixture", Nickname: "本人测试账号", Avatar: "https://avatar.xhscdn.com/example.png"}, status: "logged_in", available: true}
	f.app.xhs.rpc.Close()
	f.app.xhs.rpc = rpc
	f.app.xhs.loaded = true
	f.app.xhs.session = "opaque-cookie-secret"
	note, asset, _ := seedSource(t, f)
	version := domain.Version{ID: "confirmed-fixture", Number: 1, Title: "扫码发布测试笔记", Body: "这是经过确认的正文。", Topics: []string{"测试"}, AssetIDs: []string{asset.ID}, Confirmed: true}
	rewrite := domain.Rewrite{ID: "rewrite-fixture", NoteID: note.ID, Versions: []domain.Version{version}, ConfirmedVersionID: version.ID}
	p := domain.Publication{ID: "publication-fixture", RewriteID: rewrite.ID, VersionID: version.ID, Title: version.Title, Body: version.Body, Topics: version.Topics, AssetIDs: version.AssetIDs, Status: "ready"}
	if e := f.app.Store.Put("rewrites", rewrite.ID, rewrite); e != nil {
		t.Fatal(e)
	}
	if e := f.app.Store.Put("publications", p.ID, p); e != nil {
		t.Fatal(e)
	}
	return f, rpc, p
}

func previewXHSTest(t *testing.T, f apiFixture, p domain.Publication) xhsPublishPreview {
	t.Helper()
	return decodeResponse[xhsPublishPreview](t, f.call(t, "POST", "/publications/"+p.ID+"/publish-preview", map[string]string{"visibility": "private"}, true), 200)
}

func submitXHSTest(t *testing.T, f apiFixture, p domain.Publication, token string) string {
	t.Helper()
	reply := decodeResponse[map[string]string](t, f.call(t, "POST", "/publications/"+p.ID+"/publish", map[string]string{"confirmation_token": token}, true), 202)
	return reply["job_id"]
}

func TestXHSAccountAndPublicationRoutesRequireCSRFAndOrigin(t *testing.T) {
	f, _, p := seedXHSFixture(t)
	for _, entry := range []struct{ method, path string }{{"POST", "/xhs/login"}, {"DELETE", "/xhs/account"}, {"POST", "/publications/" + p.ID + "/publish-preview"}, {"POST", "/publications/" + p.ID + "/publish"}} {
		if response := f.call(t, entry.method, entry.path, map[string]string{"visibility": "private"}, false); response.Code != 403 {
			t.Fatalf("CSRF boundary failed %s %d", entry.path, response.Code)
		}
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/xhs/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("X-CSRF-Token", f.app.CSRF)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, req)
	if response.Code != 403 {
		t.Fatal("cross-origin account write accepted")
	}
}

func TestXHSPreviewBindsSafeMediaVisibilityAndSingleUseToken(t *testing.T) {
	f, rpc, p := seedXHSFixture(t)
	preview := previewXHSTest(t, f, p)
	if preview.Visibility != "private" || preview.MediaType != "image" || len(preview.Assets) != 1 || preview.Account.ID != rpc.profile.ID || preview.ConfirmationToken == "" {
		t.Fatalf("invalid preview: %+v", preview)
	}
	response := f.call(t, "POST", "/publications/"+p.ID+"/publish-preview", map[string]string{"visibility": "private"}, true)
	if strings.Contains(response.Body.String(), "opaque-cookie-secret") || strings.Contains(response.Body.String(), "local_path") || strings.Contains(response.Body.String(), f.app.Data) || strings.Contains(response.Body.String(), "source_url") {
		t.Fatal("preview leaked credentials or local path")
	}
	jobID := submitXHSTest(t, f, p, preview.ConfirmationToken)
	if response := f.call(t, "POST", "/publications/"+p.ID+"/publish", map[string]string{"confirmation_token": preview.ConfirmationToken}, true); response.Code != 409 {
		t.Fatal("confirmation token reused")
	}
	var job domain.Job
	f.app.Store.Get("jobs", jobID, &job)
	if job.Kind != "publish" || job.State != "queued" || strings.Contains(string(job.Payload), "opaque-cookie-secret") {
		t.Fatal("queue snapshot did not exclude account session")
	}
	if rpc.count("publish") != 0 {
		t.Fatal("publish happened during confirmation enqueue")
	}
	var saved domain.Publication
	f.app.Store.Get("publications", p.ID, &saved)
	if saved.Status != "publishing" || saved.Visibility != "private" || saved.PublishJobID != jobID || saved.PublishedAt != "" {
		t.Fatal("enqueue falsely marked published")
	}
	listed := f.call(t, "GET", "/jobs/"+jobID, nil, false)
	if strings.Contains(listed.Body.String(), "payload") || strings.Contains(listed.Body.String(), "opaque-cookie-secret") {
		t.Fatal("job endpoint leaked snapshot")
	}
	if exported := f.call(t, "GET", "/publications/"+p.ID+"/export", nil, true); exported.Code != 200 {
		t.Fatal("export unavailable during publish")
	}
	f.app.Store.Get("publications", p.ID, &saved)
	if saved.Status != "publishing" {
		t.Fatal("export overwrote active publish state")
	}
	if changed := f.call(t, "PATCH", "/publications/"+p.ID, map[string]string{"status": "published", "published_url": "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f"}, true); changed.Code != 409 {
		t.Fatal("manual record changed an active publishing request")
	}
	if logout := f.call(t, "DELETE", "/xhs/account", nil, true); logout.Code != 409 {
		t.Fatal("logout cleared the account of a queued publication")
	}
}

func TestXHSConfirmationExpiresRejectsAccountAndMediaChanges(t *testing.T) {
	for _, scenario := range []string{"expired", "account", "media", "content", "logout"} {
		t.Run(scenario, func(t *testing.T) {
			f, rpc, p := seedXHSFixture(t)
			preview := previewXHSTest(t, f, p)
			switch scenario {
			case "expired":
				f.app.xhs.mu.Lock()
				token := f.app.xhs.tokens[preview.ConfirmationToken]
				token.ExpiresAt = time.Now().Add(-time.Minute)
				f.app.xhs.tokens[preview.ConfirmationToken] = token
				f.app.xhs.mu.Unlock()
			case "account":
				rpc.mu.Lock()
				rpc.profile.ID = "different-account"
				rpc.mu.Unlock()
			case "media":
				var asset domain.Asset
				f.app.Store.Get("assets", p.AssetIDs[0], &asset)
				path, _ := f.app.assetPath(asset)
				os.WriteFile(path, []byte("changed"), 0600)
			case "content":
				p.Body = "已经改变的文案"
				f.app.Store.Put("publications", p.ID, p)
			case "logout":
				decodeResponse[xhsAccountStatus](t, f.call(t, "DELETE", "/xhs/account", nil, true), 200)
			}
			response := f.call(t, "POST", "/publications/"+p.ID+"/publish", map[string]string{"confirmation_token": preview.ConfirmationToken}, true)
			if response.Code != 409 || rpc.count("publish") != 0 || len(list[domain.Job](f.app, "jobs")) != 0 {
				t.Fatalf("changed confirmation accepted %s %d", scenario, response.Code)
			}
		})
	}
}

func TestXHSConcurrentConfirmationsCreateOnePublishJob(t *testing.T) {
	f, _, p := seedXHSFixture(t)
	one := previewXHSTest(t, f, p)
	two := previewXHSTest(t, f, p)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, token := range []string{one.ConfirmationToken, two.ConfirmationToken} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			results <- f.call(t, "POST", "/publications/"+p.ID+"/publish", map[string]string{"confirmation_token": token}, true).Code
		}(token)
	}
	wg.Wait()
	close(results)
	codes := map[int]int{}
	for code := range results {
		codes[code]++
	}
	if codes[202] != 1 || codes[409] != 1 || len(list[domain.Job](f.app, "jobs")) != 1 {
		t.Fatalf("duplicate publication queued %+v", codes)
	}
}

func TestXHSPublicationValidationProtectsPathsTypesAndTopicLength(t *testing.T) {
	for _, scenario := range []string{"outside", "unconfirmed", "title", "topic_length", "topic_name", "topic_count", "topic_hash", "mime", "empty", "count", "visibility"} {
		t.Run(scenario, func(t *testing.T) {
			f, _, p := seedXHSFixture(t)
			var rewrite domain.Rewrite
			f.app.Store.Get("rewrites", p.RewriteID, &rewrite)
			var asset domain.Asset
			f.app.Store.Get("assets", p.AssetIDs[0], &asset)
			visibility := "private"
			switch scenario {
			case "outside":
				outside := filepath.Join(filepath.Dir(f.app.Data), "outside.png")
				os.WriteFile(outside, []byte("private"), 0600)
				asset.LocalPath = outside
				f.app.Store.Put("assets", asset.ID, asset)
			case "unconfirmed":
				rewrite.Versions[0].Confirmed = false
			case "title":
				p.Title = strings.Repeat("字", 21)
				rewrite.Versions[0].Title = p.Title
			case "topic_length":
				p.Body = strings.Repeat("字", 990)
				rewrite.Versions[0].Body = p.Body
				p.Topics = []string{"测试话题"}
				rewrite.Versions[0].Topics = p.Topics
			case "topic_name":
				p.Topics = []string{strings.Repeat("字", 31)}
				rewrite.Versions[0].Topics = p.Topics
			case "topic_count":
				p.Topics = []string{}
				for i := 0; i < 11; i++ {
					p.Topics = append(p.Topics, "话题")
				}
				rewrite.Versions[0].Topics = p.Topics
			case "topic_hash":
				p.Topics = []string{"#话题"}
				rewrite.Versions[0].Topics = p.Topics
			case "mime":
				asset.Kind = "audio"
				f.app.Store.Put("assets", asset.ID, asset)
			case "empty":
				p.AssetIDs = []string{}
			case "count":
				for i := 0; i < 19; i++ {
					duplicate := asset
					duplicate.ID = ID()
					f.app.Store.Put("assets", duplicate.ID, duplicate)
					p.AssetIDs = append(p.AssetIDs, duplicate.ID)
				}
			case "visibility":
				visibility = "friends"
			}
			f.app.Store.Put("rewrites", rewrite.ID, rewrite)
			f.app.Store.Put("publications", p.ID, p)
			response := f.call(t, "POST", "/publications/"+p.ID+"/publish-preview", map[string]string{"visibility": visibility}, true)
			if response.Code != 409 {
				t.Fatalf("invalid %s preview accepted: %d %s", scenario, response.Code, response.Body.String())
			}
		})
	}
}

func TestXHSPublishWorkerRequiresRealNoteIDAndNeverRetriesUnknown(t *testing.T) {
	for _, scenario := range []string{"success", "rejected", "unknown", "lost_response", "missing_note_id", "changed_media", "expired_account"} {
		t.Run(scenario, func(t *testing.T) {
			f, rpc, p := seedXHSFixture(t)
			preview := previewXHSTest(t, f, p)
			jobID := submitXHSTest(t, f, p, preview.ConfirmationToken)
			rpc.publishResult = map[string]any{"note_id": "6ac5ed550000000014039b6f", "account": rpc.profile, "visibility": "private", "published_at": "2026-10-07T13:00:00Z"}
			expected := "succeeded"
			switch scenario {
			case "rejected":
				rpc.publishErr = &providers.XHSPublisherError{Code: "publish_rejected", Message: "cookie=opaque-cookie-secret"}
				expected = "failed"
			case "unknown":
				rpc.publishErr = &providers.XHSPublisherError{Code: "submission_unknown", SubmissionUnknown: true, Message: "cookie=opaque-cookie-secret"}
				expected = "submission_unknown"
			case "lost_response":
				rpc.publishErr = errors.New("opaque-cookie-secret transport lost")
				expected = "submission_unknown"
			case "missing_note_id":
				rpc.publishResult = map[string]any{"url": "https://www.xiaohongshu.com"}
				expected = "submission_unknown"
			case "changed_media":
				var asset domain.Asset
				f.app.Store.Get("assets", p.AssetIDs[0], &asset)
				path, _ := f.app.assetPath(asset)
				os.WriteFile(path, []byte("changed after queued"), 0600)
				expected = "failed"
			case "expired_account":
				rpc.status = "expired"
				expected = "failed"
			}
			job, e := f.app.Store.Claim("publish")
			if e != nil || job == nil || job.ID != jobID {
				t.Fatal("publish queue claim failed")
			}
			f.app.runJob(job)
			f.app.Store.Get("jobs", jobID, job)
			f.app.Store.Get("publications", p.ID, &p)
			if job.State != expected || strings.Contains(job.Error, "opaque-cookie-secret") || strings.Contains(p.PublishError, "opaque-cookie-secret") {
				t.Fatalf("unsafe or wrong state %+v %+v", job, p)
			}
			if expected == "succeeded" {
				if p.Status != "published" || p.XHSNoteID == "" || p.PublishedURL != "https://www.xiaohongshu.com/explore/6ac5ed550000000014039b6f" || p.PublishedAt == "" {
					t.Fatal("real note result not saved")
				}
			}
			if expected == "submission_unknown" {
				if p.Status != "submission_unknown" || p.PublishedAt != "" || p.XHSNoteID != "" {
					t.Fatal("ambiguous request falsely published")
				}
				if retry := f.call(t, "POST", "/jobs/"+jobID+"/retry", map[string]string{"remote_task_id": "attempt-to-bypass"}, true); retry.Code != 409 {
					t.Fatal("unknown publish task resumed")
				}
				if retry := f.call(t, "POST", "/publications/"+p.ID+"/publish-preview", map[string]string{"visibility": "private"}, true); retry.Code != 409 {
					t.Fatal("unknown publish preview allowed duplicate")
				}
				f.call(t, "GET", "/publications/"+p.ID+"/export", nil, true)
				f.app.Store.Get("publications", p.ID, &p)
				if p.Status != "submission_unknown" {
					t.Fatal("export lost unknown state")
				}
			}
			if expected == "failed" {
				if p.Status != "publish_failed" || p.PublishedAt != "" {
					t.Fatal("rejected request falsely published")
				}
			}
			if scenario == "changed_media" || scenario == "expired_account" {
				if rpc.count("publish") != 0 {
					t.Fatal("invalid queue snapshot submitted")
				}
			} else if rpc.count("publish") != 1 {
				t.Fatal("publish call count incorrect")
			}
		})
	}
}

func TestXHSLoginAsyncPersistsOnlyDPAPIAndCanClear(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI requires Windows")
	}
	f, rpc, _ := seedXHSFixture(t)
	rpc.loginResult = map[string]any{"login_id": "remote-login", "status": "logged_in", "account": rpc.profile}
	result := decodeResponse[map[string]string](t, f.call(t, "POST", "/xhs/login", map[string]any{}, true), 202)
	deadline := time.Now().Add(3 * time.Second)
	var login xhsLogin
	for time.Now().Before(deadline) {
		login = decodeResponse[xhsLogin](t, f.call(t, "GET", "/xhs/login/"+result["login_id"], nil, false), 200)
		if login.Status == "logged_in" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if login.Status != "logged_in" || login.Account == nil || login.QRImage != "" {
		t.Fatalf("login not verified %+v", login)
	}
	var saved savedXHSAccount
	if e := f.app.Store.Get("xhs_accounts", "main", &saved); e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(saved.EncryptedSession, "dpapi:") || strings.Contains(saved.EncryptedSession, "opaque-cookie-secret") {
		t.Fatal("session not encrypted")
	}
	plain, e := cryptSecret(strings.TrimPrefix(saved.EncryptedSession, "dpapi:"), false)
	if e != nil || plain != "opaque-cookie-secret" {
		t.Fatal("saved account cannot restore")
	}
	response := f.call(t, "GET", "/xhs/login/"+result["login_id"], nil, false)
	if strings.Contains(response.Body.String(), "opaque-cookie-secret") || strings.Contains(response.Body.String(), "session") {
		t.Fatal("login exposed opaque credential")
	}
	decodeResponse[xhsAccountStatus](t, f.call(t, "DELETE", "/xhs/account", nil, true), 200)
	if f.app.Store.Get("xhs_accounts", "main", &saved) == nil {
		t.Fatal("logout retained DPAPI account")
	}
}

func TestXHSAccountRestoreRequiresLiveVerificationAndHealth(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI requires Windows")
	}
	f, rpc, _ := seedXHSFixture(t)
	encrypted, e := cryptSecret("saved-opaque-account", true)
	if e != nil {
		t.Fatal(e)
	}
	f.app.Store.Put("xhs_accounts", "main", savedXHSAccount{EncryptedSession: "dpapi:" + encrypted, Account: xhsAccount{ID: "stale-profile", Nickname: "旧用户名"}})
	f.app.xhs.loaded = false
	f.app.xhs.session = ""
	rpc.status = "expired"
	f.app.restoreXHSAccount(context.Background())
	profile, e := f.app.checkXHSAccount(context.Background())
	if e == nil || profile != nil || f.app.xhs.status.Status == "logged_in" {
		t.Fatal("saved profile falsely treated as verified login")
	}
	rpc.status = "logged_in"
	rpc.available = false
	if _, e = f.app.checkXHSAccount(context.Background()); e == nil || f.app.xhs.status.DependencyReady || f.app.xhs.status.Status != "unavailable" {
		t.Fatal("missing dependencies marked ready")
	}
	if rpc.count("health") == 0 {
		t.Fatal("dependency status skipped health RPC")
	}
}

func TestXHSLoginQRCodeExpiryAndSafeErrorMapping(t *testing.T) {
	f, rpc, _ := seedXHSFixture(t)
	id := ID()
	f.app.xhs.logins[id] = &xhsLogin{ID: id, Status: "waiting_scan", QRImage: "data:image/png;base64,aW1hZ2U=", ExpiresAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}
	login := decodeResponse[xhsLogin](t, f.call(t, "GET", "/xhs/login/"+id, nil, false), 200)
	if login.Status != "expired" || login.QRImage != "" {
		t.Fatal("expired QR remained usable")
	}
	for _, code := range []string{"dependency_missing", "session_invalid", "publish_rejected", "topic_not_found", "upload_failed"} {
		message := xhsErrorMessage(&providers.XHSPublisherError{Code: code, Message: "opaque-cookie-secret"})
		if strings.Contains(message, "opaque-cookie-secret") || message == "小红书服务暂时不可用，请检查本机发布依赖和网络" {
			t.Fatalf("known safe code not explained: %s", code)
		}
	}
	_ = rpc
}

func TestXHSCancelledLoginCannotRestoreAccount(t *testing.T) {
	f, rpc, _ := seedXHSFixture(t)
	// The QR was deleted by logout before its background goroutine started.
	f.app.xhs.logins = map[string]*xhsLogin{}
	f.app.runXHSLogin("cancelled")
	if rpc.count("login_start") != 0 {
		t.Fatal("cancelled login started a new remote QR")
	}
	if e := f.app.persistXHSLogin(context.Background(), "cancelled"); e == nil {
		t.Fatal("cancelled QR exported an account")
	}
	if rpc.count("account_export") != 0 {
		t.Fatal("cancelled QR resurrected login")
	}
}

func TestXHSExplicitRejectionRequiresNewPreviewAndShutdownPausesSubmission(t *testing.T) {
	f, rpc, p := seedXHSFixture(t)
	first := previewXHSTest(t, f, p)
	jobID := submitXHSTest(t, f, p, first.ConfirmationToken)
	rpc.publishErr = &providers.XHSPublisherError{Code: "publish_rejected"}
	job, _ := f.app.Store.Claim("publish")
	f.app.runJob(job)
	if response := f.call(t, "POST", "/jobs/"+jobID+"/retry", map[string]any{}, true); response.Code != 409 {
		t.Fatal("explicit failure bypassed review")
	}
	second := previewXHSTest(t, f, p)
	secondID := submitXHSTest(t, f, p, second.ConfirmationToken)
	if secondID == jobID {
		t.Fatal("new confirmation reused failed job")
	}
	rpc.publishErr = nil
	rpc.fn = func(method string, params any) (any, error) {
		if method == "publish" {
			f.app.cancel()
			return nil, context.Canceled
		}
		return nil, nil
	}
	job, _ = f.app.Store.Claim("publish")
	f.app.runJob(job)
	f.app.Store.Get("jobs", secondID, job)
	f.app.Store.Get("publications", p.ID, &p)
	if job.State != "submission_unknown" || p.Status != "submission_unknown" {
		t.Fatal("shutdown automatically queued a submitted publication")
	}
}

// 本机保存的会话不可再用时必须自愈：删除存档并让登录入口保持可用，
// 而不是停在 expired/unavailable 状态把扫码按钮永久禁用。
func TestXHSUnusableSavedSessionSelfHeals(t *testing.T) {
	f, rpc, _ := seedXHSFixture(t)
	x := f.app.xhs
	x.loaded, x.session = false, ""
	if e := f.app.Store.Put("xhs_accounts", "main", savedXHSAccount{EncryptedSession: "invalid-fixture-format"}); e != nil {
		t.Fatal(e)
	}
	f.app.restoreXHSAccount(context.Background())
	if x.status.Status != "logged_out" || !x.status.DependencyReady {
		t.Fatalf("unusable saved session did not reopen the login entry: %+v", x.status)
	}
	var saved savedXHSAccount
	if e := f.app.Store.Get("xhs_accounts", "main", &saved); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("unusable saved session was not removed")
	}
	if rpc.count("account_restore") != 0 {
		t.Fatal("invalid local session reached the bridge")
	}
}

// 桥接明确拒绝保存的会话（会话类错误码）时同样自愈，用户可直接重新扫码。
func TestXHSRejectedSavedSessionSelfHeals(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI requires Windows")
	}
	f, rpc, _ := seedXHSFixture(t)
	x := f.app.xhs
	x.loaded, x.session = false, ""
	encrypted, e := cryptSecret("expired-xhs-session", true)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.app.Store.Put("xhs_accounts", "main", savedXHSAccount{EncryptedSession: "dpapi:" + encrypted}); e != nil {
		t.Fatal(e)
	}
	rpc.fn = func(method string, params any) (any, error) {
		if method == "account_restore" {
			return nil, &providers.XHSPublisherError{Code: "SESSION_EXPIRED", Message: "fixture-private-error-payload"}
		}
		return nil, nil
	}
	f.app.restoreXHSAccount(context.Background())
	if x.status.Status != "logged_out" || !x.status.DependencyReady {
		t.Fatalf("rejected saved session did not reopen the login entry: %+v", x.status)
	}
	var saved savedXHSAccount
	if e := f.app.Store.Get("xhs_accounts", "main", &saved); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("rejected saved session was not removed")
	}
	if rpc.count("account_restore") != 1 {
		t.Fatal("saved session was not offered to the bridge once")
	}
}

// 实时核验拒绝当前会话时，登录入口必须保持可用，且不得透传桥接私有错误。
func TestXHSLiveSessionErrorKeepsLoginEntry(t *testing.T) {
	f, rpc, _ := seedXHSFixture(t)
	x := f.app.xhs
	rpc.fn = func(method string, params any) (any, error) {
		if method == "account_status" {
			return nil, &providers.XHSPublisherError{Code: "NOT_LOGGED_IN", Message: "fixture-private-error-payload"}
		}
		return nil, nil
	}
	if _, e := f.app.checkXHSAccount(context.Background()); e == nil {
		t.Fatal("rejected session was accepted")
	}
	if x.status.Status != "logged_out" || !x.status.DependencyReady {
		t.Fatalf("session rejection disabled the login entry: %+v", x.status)
	}
	if strings.Contains(x.status.Message, "fixture-private-error-payload") {
		t.Fatal("unsafe bridge message reached the UI")
	}
}

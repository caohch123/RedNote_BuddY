package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestSeparatedAPIAllowsOnlyConfiguredFrontendAndKeepsCSRF(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.app.ConfigureFrontendOrigins([]string{"http://127.0.0.1:5173", "http://localhost:8080"}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		origin, method, token string
		preflight             bool
		status                int
	}{
		{"http://127.0.0.1:5173", "OPTIONS", "", true, 204},
		{"http://127.0.0.1:5173", "GET", "", false, 200},
		{"http://localhost:8080", "GET", "", false, 200},
		{"https://attacker.example", "OPTIONS", "", true, 403},
		{"http://127.0.0.1:5174", "GET", "", false, 403},
		{"http://127.0.0.1:5173.evil.example", "GET", "", false, 403},
		{"null", "GET", "", false, 403},
		{"http://127.0.0.1:5173", "POST", "", false, 403},
		{"http://127.0.0.1:5173", "POST", f.app.CSRF, false, 400},
	} {
		req := httptest.NewRequest(scenario.method, "http://127.0.0.1:8080/api/v1/session", nil)
		if scenario.method == "POST" {
			req.URL.Path = "/api/v1/rewrites"
		}
		req.Header.Set("Origin", scenario.origin)
		req.Header.Set("X-CSRF-Token", scenario.token)
		if scenario.preflight {
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", "content-type,x-csrf-token")
		}
		reply := httptest.NewRecorder()
		f.router.ServeHTTP(reply, req)
		if reply.Code != scenario.status {
			t.Fatalf("%s %s: %d %s", scenario.method, scenario.origin, reply.Code, reply.Body.String())
		}
		if scenario.status != 403 && reply.Header().Get("Access-Control-Allow-Origin") != scenario.origin {
			t.Fatal("allowed frontend cannot read the response")
		}
		if reply.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Fatal("wildcard origins must never be enabled")
		}
	}
	if len(list[domain.Rewrite](f.app, "rewrites")) != 0 {
		t.Fatal("rejected writes changed data")
	}
	req := httptest.NewRequest("GET", "http://attacker.example/api/v1/session", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	reply := httptest.NewRecorder()
	f.router.ServeHTTP(reply, req)
	if reply.Code != 403 {
		t.Fatal("frontend origin configuration bypassed the Host boundary")
	}
}

func TestSeparatedAPIHasNoFrontendBuildDependency(t *testing.T) {
	f := newAPIFixture(t)
	dist := filepath.Join(f.app.Root, "frontend", "dist")
	if err := os.MkdirAll(dist, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("frontend must not be served by API"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/index.html", "/some-page", "/api/v1/unknown"} {
		reply := httptest.NewRecorder()
		f.router.ServeHTTP(reply, httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil))
		if reply.Code != 404 {
			t.Fatalf("API served a frontend route %s: %d", path, reply.Code)
		}
	}
	decodeResponse[map[string]string](t, f.call(t, "GET", "/session", nil, false), 200)
}

func TestFrontendOriginConfigurationRejectsAmbiguousOrigins(t *testing.T) {
	f := newAPIFixture(t)
	for _, origin := range []string{"*", "null", "http://*.example.com", "http://localhost:5173/", "http://localhost:5173/path", "http://user:password@localhost:5173", "http://localhost:5173?x=1", "http://localhost:5173#fragment"} {
		if err := f.app.ConfigureFrontendOrigins([]string{origin}); err == nil {
			t.Fatalf("invalid origin accepted: %s", origin)
		}
	}
}

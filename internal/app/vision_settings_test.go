package app

import (
	"strings"
	"testing"

	"xhs-workbench/internal/domain"
)

func TestVisionConfigurationMigrationAndSecretStorage(t *testing.T) {
	f := newAPIFixture(t)
	var legacy domain.Settings
	if err := f.app.Store.Get("settings", "main", &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy.Providers, "vision")
	if err := f.app.Store.Put("settings", "main", legacy); err != nil {
		t.Fatal(err)
	}
	settings := decodeResponse[domain.Settings](t, f.call(t, "GET", "/settings", nil, false), 200)
	if p := settings.Providers["vision"]; p.Model != "qwen3-vl-plus" || p.Configured {
		t.Fatal("legacy settings must expose an unconfigured vision provider")
	}
	tested := decodeResponse[struct {
		OK bool `json:"ok"`
	}](t, f.call(t, "POST", "/settings/test", map[string]string{"capability": "vision"}, true), 200)
	if tested.OK {
		t.Fatal("unconfigured vision must not report success")
	}
	response := f.call(t, "PATCH", "/settings", domain.Settings{Providers: map[string]domain.ProviderConfig{
		"vision": {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3-vl-plus", APIKey: "fixture-vision-secret"},
	}}, true)
	updated := decodeResponse[domain.Settings](t, response, 200)
	if !updated.Providers["vision"].Configured || updated.Providers["text"].Model != legacy.Providers["text"].Model || strings.Contains(response.Body.String(), "fixture-vision-secret") {
		t.Fatal("vision update must mask its secret and preserve existing settings")
	}
	var saved domain.Settings
	if err := f.app.Store.Get("settings", "main", &saved); err != nil {
		t.Fatal(err)
	}
	if key := saved.Providers["vision"].APIKey; !strings.HasPrefix(key, "dpapi:") || strings.Contains(key, "fixture-vision-secret") {
		t.Fatal("vision secret must be encrypted at rest")
	}
}

package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func proposalInput(count int) ImageProposalInput {
	input := ImageProposalInput{Ratio: "3:4"}
	for i := 0; i < count; i++ {
		features := map[string]string{}
		for _, key := range flexibleImageFeatureOrder {
			features[key] = "原图的" + flexibleImageFeatureName(key)
		}
		role := "reference"
		if i == 0 {
			role = "primary"
		}
		input.Images = append(input.Images, ImageProposalImage{Number: i + 1, AssetID: []string{"main-id", "aux-id", "other-id"}[i], Role: role, Features: features})
	}
	return input
}

func proposalResponse(count int) map[string]any {
	rows := []map[string]any{}
	for i := 0; i < 3; i++ {
		controls := map[string]flexibleImageControl{}
		for _, key := range flexibleImageFeatureOrder {
			controls[key] = flexibleImageControl{Mode: "image", ImageNumber: 1}
		}
		if i == 1 {
			controls["palette"] = flexibleImageControl{Mode: "custom", Value: "克制的黑金色板"}
			if count > 1 {
				controls["scene"] = flexibleImageControl{Mode: "image", ImageNumber: 2}
			}
		}
		if i == 2 {
			controls["texture"] = flexibleImageControl{Mode: "custom", Value: "粗颗粒版画"}
			if count > 2 {
				controls["lighting"] = flexibleImageControl{Mode: "image", ImageNumber: 3}
			}
		}
		rows = append(rows, map[string]any{"title": []string{"原图精修", "色彩与背景重组", "画法与光影重组"}[i], "summary": "测试建议保留主图主体，明确调整当前选定画面属性。", "feature_controls": controls})
	}
	return map[string]any{"proposals": rows}
}

func TestImageProposalsUseActualImagesAndMapOnlyKnownSources(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		input := proposalInput(count)
		input.Images[0].Features["text"] = "MODEL_BYPASS_abc 是图中文字"
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/chat/completions" {
				t.Errorf("unexpected generation request: %s", r.URL.Path)
			}
			var req struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Messages) != 2 || req.Messages[0].Role != "system" || strings.Contains(req.Messages[0].Content, "MODEL_BYPASS_abc") {
				t.Error("image evidence was promoted to system instructions")
			}
			var actual ImageProposalInput
			_ = json.Unmarshal([]byte(req.Messages[1].Content), &actual)
			if len(actual.Images) != count || actual.Images[0].Role != "primary" || strings.Contains(req.Messages[1].Content, "main-id") {
				t.Error("image role/order or private ID mapping changed")
			}
			content, _ := json.Marshal(proposalResponse(count))
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
		}))
		proposals, err := configured("text", server.URL).RecommendImageProposals(context.Background(), input)
		server.Close()
		if err != nil || len(proposals) != 3 || calls != 1 {
			t.Fatalf("%d images: proposals=%v calls=%d error=%v", count, proposals, calls, err)
		}
		if proposals[0].ID != "proposal-1" || proposals[0].FeatureControls["subject"].AssetID != "main-id" {
			t.Fatal("primary binding was lost")
		}
		if count > 1 && proposals[1].FeatureControls["scene"].AssetID != "aux-id" {
			t.Fatal("reference binding was lost")
		}
		if count > 2 && proposals[2].FeatureControls["lighting"].AssetID != "other-id" {
			t.Fatal("second reference binding was lost")
		}
	}
}

func TestImageProposalsRejectIncompleteOrDuplicatedModelSuggestions(t *testing.T) {
	cases := map[string]func(map[string]any){
		"two_options": func(out map[string]any) { out["proposals"] = out["proposals"].([]map[string]any)[:2] },
		"duplicate_controls": func(out map[string]any) {
			rows := out["proposals"].([]map[string]any)
			rows[1]["feature_controls"] = rows[0]["feature_controls"]
		},
		"unknown_source": func(out map[string]any) {
			out["proposals"].([]map[string]any)[1]["feature_controls"].(map[string]flexibleImageControl)["scene"] = flexibleImageControl{Mode: "image", ImageNumber: 4}
		},
		"missing_dimension": func(out map[string]any) {
			delete(out["proposals"].([]map[string]any)[0]["feature_controls"].(map[string]flexibleImageControl), "action")
		},
		"extra_field": func(out map[string]any) {
			out["proposals"].([]map[string]any)[0]["prompt"] = "不应被接受的第二份依据"
		},
		"long_custom": func(out map[string]any) {
			out["proposals"].([]map[string]any)[1]["feature_controls"].(map[string]flexibleImageControl)["palette"] = flexibleImageControl{Mode: "custom", Value: strings.Repeat("长", 501)}
		},
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			out := proposalResponse(2)
			modify(out)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				content, _ := json.Marshal(out)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
			}))
			defer server.Close()
			if proposals, err := configured("text", server.URL).RecommendImageProposals(context.Background(), proposalInput(2)); err == nil || len(proposals) > 0 || calls != 1 {
				t.Fatalf("invalid suggestions accepted or retried: %v %v calls=%d", proposals, err, calls)
			}
		})
	}
}

func TestImageProposalsIncompleteAnalysisDoesNotCallTextModel(t *testing.T) {
	input := proposalInput(1)
	delete(input.Images[0].Features, "action")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	if _, err := configured("text", server.URL).RecommendImageProposals(context.Background(), input); err == nil || calls != 0 {
		t.Fatal("incomplete analysis reached text model")
	}
}

func TestFlexibleImagePromptMergesOptionalRequirementsWithoutChangingBaseSources(t *testing.T) {
	input := flexibleTestInput(t, map[string]flexibleImageControl{"subject": {Mode: "image", ImageNumber: 1}, "palette": {Mode: "image", ImageNumber: 1}}, []map[string]string{{"subject": "一只茶壶", "palette": "绿色"}})
	input["extra_requirements"] = "保持茶壶造型，将配色改为紫色"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req.Messages[0].Content, "优先于建议方案") || strings.Contains(req.Messages[0].Content, "紫色") {
			t.Error("additional requirements did not remain user data with targeted precedence")
		}
		if !strings.Contains(req.Messages[1].Content, "保持茶壶造型，将配色改为紫色") {
			t.Error("additional requirements missing from model request")
		}
		out := flexibleTestResponse(input)
		out["resolved_features"].(map[string]string)["palette"] = "紫色"
		content, _ := json.Marshal(out)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
	}))
	defer server.Close()
	prompt, err := configured("text", server.URL).ComposeImagePrompt(context.Background(), input)
	if err != nil || !strings.Contains(prompt, "用户附加要求") || !strings.Contains(prompt, "已合并附加要求）：紫色") || !strings.Contains(prompt, "一只茶壶") {
		t.Fatalf("requirements were lost: %s %v", prompt, err)
	}
	input["extra_requirements"] = strings.Repeat("长", 1001)
	if _, err := configured("text", server.URL).ComposeImagePrompt(context.Background(), input); err == nil {
		t.Fatal("overlong additional requirements accepted")
	}
}

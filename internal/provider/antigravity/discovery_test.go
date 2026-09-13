package antigravity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTryFetchAvailableModels(t *testing.T) {
	mockResp := fetchAvailableModelsResponse{
		Models: map[string]struct {
			DisplayName      string `json:"displayName"`
			SupportsThinking bool   `json:"supportsThinking"`
			MaxTokens        int    `json:"maxTokens"`
			MaxOutputTokens  int    `json:"maxOutputTokens"`
			IsInternal       bool   `json:"isInternal"`
		}{
			"gemini-3.8-flash": {
				DisplayName:      "Gemini 3.8 Flash",
				SupportsThinking: true,
				MaxTokens:        1048576,
				MaxOutputTokens:  65536,
			},
			"claude-sonnet-4-6": {
				DisplayName:      "Claude Sonnet 4.6",
				SupportsThinking: true,
				MaxTokens:        200000,
				MaxOutputTokens:  64000,
			},
			"chat_20706": {
				DisplayName: "Internal Chat 20706",
			},
			"internal-eval-model": {
				DisplayName: "Internal Eval",
				IsInternal:  true,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != FetchAvailableModelsPath {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") != DefaultUserAgent {
			t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), DefaultUserAgent)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResp)
	}))
	defer server.Close()

	models, err := tryFetchAvailableModels(context.Background(), server.URL, "mock-token")
	if err != nil {
		t.Fatalf("tryFetchAvailableModels 失敗: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("預期過濾後有 2 個模型，實際得到 %d 個", len(models))
	}

	if models[0].ID != "claude-sonnet-4-6" || models[1].ID != "gemini-3.8-flash" {
		t.Errorf("排序或模型清單不符: %+v", models)
	}

	if models[1].ContextWindow != 1048576 || !models[1].SupportsThinking {
		t.Errorf("gemini-3.8-flash 屬性不符: %+v", models[1])
	}
}

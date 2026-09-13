package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const (
	FetchAvailableModelsPath = "/v1internal:fetchAvailableModels"
	RetrieveUserQuotaPath    = "/v1internal:retrieveUserQuota"
)

// DiscoveredModel 代表從 Antigravity API 自動探測到的可用模型。
type DiscoveredModel struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	ContextWindow    int    `json:"context_window"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	SupportsThinking bool   `json:"supports_thinking"`
}

type fetchAvailableModelsResponse struct {
	Models map[string]struct {
		DisplayName      string `json:"displayName"`
		SupportsThinking bool   `json:"supportsThinking"`
		MaxTokens        int    `json:"maxTokens"`
		MaxOutputTokens  int    `json:"maxOutputTokens"`
		IsInternal       bool   `json:"isInternal"`
	} `json:"models"`
}

type retrieveUserQuotaResponse struct {
	Buckets []struct {
		ModelID string `json:"modelId"`
	} `json:"buckets"`
}

// FetchAvailableModels 自動從 Google Antigravity 端點獲取該帳號所有可用的模型清單。
func FetchAvailableModels(ctx context.Context, creds *Credentials) ([]DiscoveredModel, error) {
	token, err := EnsureValidToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("ensure valid token: %w", err)
	}

	endpoints := []string{CloudCodeAssistEndpoint, CloudCodeSandboxEndpoint}

	// 1. 優先嘗試 fetchAvailableModels 端點
	for _, ep := range endpoints {
		models, err := tryFetchAvailableModels(ctx, ep, token)
		if err == nil && len(models) > 0 {
			return models, nil
		}
	}

	// 2. 備援方案：嘗試從 retrieveUserQuota 端點解析可用模型
	if creds.ProjectID != "" {
		for _, ep := range endpoints {
			models, err := tryRetrieveUserQuotaModels(ctx, ep, token, creds.ProjectID)
			if err == nil && len(models) > 0 {
				return models, nil
			}
		}
	}

	return nil, fmt.Errorf("無法從 Antigravity 端點獲取可用模型清單 (可能需要先完成帳號登入與配額開通)")
}

func tryFetchAvailableModels(ctx context.Context, endpoint, token string) ([]DiscoveredModel, error) {
	reqURL := endpoint + FetchAvailableModelsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed fetchAvailableModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	denylist := map[string]bool{
		"chat_20706": true,
		"chat_23310": true,
	}

	var results []DiscoveredModel
	for id, m := range parsed.Models {
		if denylist[id] || m.IsInternal {
			continue
		}

		ctxWindow := m.MaxTokens
		if ctxWindow <= 0 {
			if strings.Contains(id, "gemini") {
				ctxWindow = 1048576
			} else {
				ctxWindow = 200000
			}
		}

		maxOutput := m.MaxOutputTokens
		if maxOutput <= 0 {
			maxOutput = 65536
		}

		displayName := m.DisplayName
		if displayName == "" {
			displayName = id
		}

		results = append(results, DiscoveredModel{
			ID:               id,
			DisplayName:      displayName,
			ContextWindow:    ctxWindow,
			MaxOutputTokens:  maxOutput,
			SupportsThinking: m.SupportsThinking,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ID < results[j].ID
	})

	return results, nil
}

func tryRetrieveUserQuotaModels(ctx context.Context, endpoint, token, projectID string) ([]DiscoveredModel, error) {
	reqURL := endpoint + RetrieveUserQuotaPath
	payload, _ := json.Marshal(map[string]string{"project": projectID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var quotaResp retrieveUserQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quotaResp); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var results []DiscoveredModel
	for _, b := range quotaResp.Buckets {
		id := strings.TrimSpace(b.ModelID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true

		ctxWindow := 200000
		if strings.Contains(id, "gemini") {
			ctxWindow = 1048576
		}

		results = append(results, DiscoveredModel{
			ID:               id,
			DisplayName:      id,
			ContextWindow:    ctxWindow,
			MaxOutputTokens:  65536,
			SupportsThinking: strings.Contains(id, "thinking") || strings.Contains(id, "3.") || strings.Contains(id, "2.5"),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ID < results[j].ID
	})

	return results, nil
}

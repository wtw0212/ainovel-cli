package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	CloudCodeAssistEndpoint = "https://daily-cloudcode-pa.googleapis.com"
	CloudCodeSandboxEndpoint = "https://daily-cloudcode-pa.sandbox.googleapis.com"

	LoadCodeAssistURL = CloudCodeAssistEndpoint + "/v1internal:loadCodeAssist"
	OnboardUserURL    = CloudCodeAssistEndpoint + "/v1internal:onboardUser"
	OperationsBaseURL = CloudCodeAssistEndpoint + "/v1internal"

	DefaultUserAgent = "antigravity/hub/2.8.0 (aidev_client; os_type=darwin; arch=arm64; cl=963137146)"
	FreeTierID       = "free-tier"
)

// Credentials 存放 Antigravity 本地授權資訊。
type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // Unix 秒
	ProjectID    string `json:"project_id"`
	Email        string `json:"email"`
}

var (
	tokenMu sync.Mutex
	authFilePathOverride string
)

// SetAuthFilePathForTest 供單元測試隔離憑證檔案路徑，返回還原函數。
func SetAuthFilePathForTest(path string) func() {
	orig := authFilePathOverride
	authFilePathOverride = path
	return func() {
		authFilePathOverride = orig
	}
}

// DefaultAuthFilePath 返回 ~/.ainovel/antigravity_auth.json 路徑。
func DefaultAuthFilePath() string {
	if authFilePathOverride != "" {
		return authFilePathOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".ainovel", "antigravity_auth.json")
}

// HasCredentials 檢查本地是否存在有效憑證檔案。
func HasCredentials() bool {
	path := DefaultAuthFilePath()
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return false
	}
	creds, err := LoadCredentials()
	if err != nil {
		return false
	}
	return creds.AccessToken != "" || creds.RefreshToken != ""
}

// LoadCredentials 從本地檔案讀取 Antigravity 憑證。
func LoadCredentials() (*Credentials, error) {
	path := DefaultAuthFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}
	if creds.AccessToken == "" && creds.RefreshToken == "" {
		return nil, fmt.Errorf("credentials file contains no tokens")
	}
	return &creds, nil
}

// SaveCredentials 將憑證保存至本地檔案（權限 0600）。
func SaveCredentials(creds *Credentials) error {
	if creds == nil {
		return fmt.Errorf("nil credentials")
	}
	path := DefaultAuthFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// EnsureValidToken 檢查 Token 是否即將過期（5 分鐘緩衝），若過期則自動刷新並持久化。
func EnsureValidToken(ctx context.Context, creds *Credentials) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	now := time.Now().Unix()
	// 若離過期還有超過 300 秒，直接使用
	if creds.AccessToken != "" && creds.ExpiresAt-now > 300 {
		return creds.AccessToken, nil
	}

	if creds.RefreshToken == "" {
		return "", fmt.Errorf("antigravity 缺少 refresh_token，請重新登入")
	}

	form := url.Values{
		"client_id":     {GetClientID()},
		"client_secret": {GetClientSecret()},
		"refresh_token": {creds.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("refresh token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read refresh response: %w", err)
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("unmarshal refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("refresh token failed (%d): %s - %s", resp.StatusCode, tr.Error, tr.ErrorDesc)
	}

	creds.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		creds.RefreshToken = tr.RefreshToken
	}
	creds.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).Unix()

	if err := SaveCredentials(creds); err != nil {
		// 儲存失敗僅記錄，不中斷本次使用
		_ = err
	}

	return creds.AccessToken, nil
}

type loadCodeAssistResponse struct {
	CurrentTier             *struct{ ID string `json:"id"` } `json:"currentTier"`
	PaidTier                *struct{ ID string `json:"id"` } `json:"paidTier"`
	CloudaicompanionProject string                           `json:"cloudaicompanionProject"`
}

type onboardOperation struct {
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Response *struct {
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	} `json:"response"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// DiscoverAndOnboardProject 探測 Antigravity 關聯的 GCP 專案，若未開通則自動開通 free-tier。
func DiscoverAndOnboardProject(ctx context.Context, creds *Credentials, onProgress func(string)) (string, error) {
	if creds.ProjectID != "" {
		return creds.ProjectID, nil
	}

	token, err := EnsureValidToken(ctx, creds)
	if err != nil {
		return "", err
	}

	if onProgress != nil {
		onProgress("正在檢查 Cloud Code Assist 帳號狀態...")
	}

	// 1. 初次調用 loadCodeAssist
	loadResp, err := callLoadCodeAssist(ctx, token, "")
	if err != nil {
		return "", fmt.Errorf("call loadCodeAssist: %w", err)
	}

	// 2. 若尚未開通 currentTier，觸發 onboardUser 開通 free-tier
	if loadResp.CurrentTier == nil && loadResp.PaidTier == nil {
		if onProgress != nil {
			onProgress("帳號尚未開通 Antigravity，正在為您自動開通 Free Tier...")
		}
		if err := runOnboardUser(ctx, token); err != nil {
			return "", fmt.Errorf("onboard user: %w", err)
		}
	}

	// 3. 再次調用 loadCodeAssist 取得專案 ID
	refreshed, err := callLoadCodeAssist(ctx, token, loadResp.CloudaicompanionProject)
	if err != nil {
		return "", fmt.Errorf("refresh loadCodeAssist: %w", err)
	}

	projectID := refreshed.CloudaicompanionProject
	if projectID == "" {
		projectID = loadResp.CloudaicompanionProject
	}
	if projectID == "" {
		return "", fmt.Errorf("loadCodeAssist 未能返回 cloudaicompanionProject")
	}

	creds.ProjectID = projectID
	_ = SaveCredentials(creds)
	return projectID, nil
}

func callLoadCodeAssist(ctx context.Context, token, existingProject string) (*loadCodeAssistResponse, error) {
	payload := map[string]any{
		"metadata": map[string]string{
			"ideType": "ANTIGRAVITY",
		},
	}
	if existingProject != "" {
		payload["cloudaicompanionProject"] = existingProject
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, LoadCodeAssistURL, bytes.NewReader(bodyBytes))
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
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loadCodeAssist status %d: %s", resp.StatusCode, string(raw))
	}

	var res loadCodeAssistResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

func runOnboardUser(ctx context.Context, token string) error {
	payload := map[string]any{
		"tierId": FreeTierID,
		"metadata": map[string]string{
			"ideType": "ANTIGRAVITY",
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OnboardUserURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("onboardUser status %d: %s", resp.StatusCode, string(raw))
	}

	var op onboardOperation
	if err := json.NewDecoder(resp.Body).Decode(&op); err != nil {
		return err
	}

	// 輪詢 Operation 直到完成（最多 30 秒）
	deadline := time.Now().Add(30 * time.Second)
	for !op.Done {
		if time.Now().After(deadline) {
			return fmt.Errorf("onboardUser operation timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}

		opURL := fmt.Sprintf("%s/%s", OperationsBaseURL, op.Name)
		opReq, err := http.NewRequestWithContext(ctx, http.MethodGet, opURL, nil)
		if err != nil {
			return err
		}
		opReq.Header.Set("Authorization", "Bearer "+token)
		opReq.Header.Set("User-Agent", DefaultUserAgent)

		opResp, err := http.DefaultClient.Do(opReq)
		if err != nil {
			return err
		}
		_ = json.NewDecoder(opResp.Body).Decode(&op)
		opResp.Body.Close()
	}

	if op.Error != nil && op.Error.Code != 0 {
		return fmt.Errorf("onboard operation failed: %d %s", op.Error.Code, op.Error.Message)
	}

	return nil
}

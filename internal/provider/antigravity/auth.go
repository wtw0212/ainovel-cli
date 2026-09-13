package antigravity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	UserInfoURL  = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"

	CallbackHost = "127.0.0.1"
	CallbackPort = 51121
	CallbackPath = "/oauth-callback"
)

func decodeAntigravityParam(data []byte) string {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ 0x5A
	}
	return string(out)
}

// GetClientID 返回 Antigravity Google OAuth Client ID。
func GetClientID() string {
	return decodeAntigravityParam([]byte{
		107, 106, 109, 107, 106, 106, 108, 106, 108, 106, 111, 99, 107, 119, 46, 55,
		50, 41, 41, 51, 52, 104, 50, 104, 107, 54, 57, 40, 63, 104, 105, 111,
		44, 46, 53, 54, 53, 48, 50, 110, 61, 110, 106, 105, 63, 42, 116, 59,
		42, 42, 41, 116, 61, 53, 53, 61, 54, 63, 47, 41, 63, 40, 57, 53,
		52, 46, 63, 52, 46, 116, 57, 53, 55,
	})
}

// GetClientSecret 返回 Antigravity Google OAuth Client Secret。
func GetClientSecret() string {
	return decodeAntigravityParam([]byte{
		29, 21, 25, 9, 10, 2, 119, 17, 111, 98, 28, 13, 8, 110, 98, 108,
		22, 62, 22, 16, 107, 55, 22, 24, 98, 41, 2, 25, 110, 32, 108, 43,
		30, 27, 60,
	})
}

var AntigravityScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// GeneratePKCE 生成 PKCE 的 verifier 與 challenge (S256)。
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// TokenResponse Google OAuth Token 端點返回的結構體。
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // 秒
	TokenType    string `json:"token_type"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// UserInfoResponse Google UserInfo 返回的結構體。
type UserInfoResponse struct {
	Email string `json:"email"`
}

// StartOAuthFlow 啟動本地 HTTP 回調監聽並打開瀏覽器進行 Antigravity OAuth 授權。
func StartOAuthFlow(ctx context.Context, onProgress func(string)) (*Credentials, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	redirectURI := fmt.Sprintf("http://%s:%d%s", CallbackHost, CallbackPort, CallbackPath)

	authParams := url.Values{
		"client_id":             {GetClientID()},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(AntigravityScopes, " ")},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	authURL := fmt.Sprintf("%s?%s", AuthorizeURL, authParams.Encode())

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", CallbackHost, CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("監聽回調埠 %d 失敗: %w (請確認該端口未被佔用)", CallbackPort, err)
	}
	defer listener.Close()

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			errChan <- fmt.Errorf("OAuth 授權被拒絕: %s", errParam)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body><h3>授權失敗：%s</h3><p>請返回終端查看。</p></body></html>", errParam)
			return
		}

		if q.Get("state") != state {
			errChan <- fmt.Errorf("OAuth 狀態驗證失敗 (state mismatch)")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body><h3>狀態驗證失敗</h3></body></html>")
			return
		}

		code := q.Get("code")
		if code == "" {
			errChan <- fmt.Errorf("未收到授權碼 code")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body><h3>未收到授權碼</h3></body></html>")
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "<html><body style='font-family:sans-serif;text-align:center;padding-top:50px;'><h2>🎉 Antigravity 授權成功！</h2><p>您可以關閉此瀏覽器視窗，並返回終端繼續小說創作。</p></body></html>")
		codeChan <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()
	defer func() {
		ctxShutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctxShutdown)
	}()

	if onProgress != nil {
		onProgress(fmt.Sprintf("正在打開瀏覽器進行 Google 授權...\n若未自動打開，請手動複製訪問以下網址：\n%s", authURL))
	}
	_ = openBrowser(authURL)

	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errChan:
		return nil, err
	case code = <-codeChan:
	}

	if onProgress != nil {
		onProgress("正在換取 Token 並驗證帳號身份...")
	}

	// 換取 Token
	tokenResp, err := exchangeCode(ctx, code, verifier, redirectURI)
	if err != nil {
		return nil, err
	}

	// 查詢 Email
	email, err := fetchEmail(ctx, tokenResp.AccessToken)
	if err != nil {
		email = "unknown"
	}

	creds := &Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Unix(),
		Email:        email,
	}

	return creds, nil
}

func exchangeCode(ctx context.Context, code, verifier, redirectURI string) (*TokenResponse, error) {
	form := url.Values{
		"client_id":     {GetClientID()},
		"client_secret": {GetClientSecret()},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange token failed (%d): %s - %s", resp.StatusCode, tr.Error, tr.ErrorDesc)
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &tr, nil
}

func fetchEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var info UserInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.Email, nil
}

// openBrowser 在本機預設瀏覽器打開 URL。
func openBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	case "darwin":
		cmd = exec.Command("open", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}

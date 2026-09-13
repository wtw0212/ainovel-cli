package antigravity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE 失敗: %v", err)
	}

	if len(verifier) == 0 {
		t.Fatalf("verifier 不得為空")
	}

	// 驗證 challenge 是否為 sha256(verifier) 的 base64url 編碼
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	if challenge != expectedChallenge {
		t.Errorf("challenge = %s, 預期 %s", challenge, expectedChallenge)
	}
}

func TestCredentialsSerialization(t *testing.T) {
	creds := Credentials{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
		ExpiresAt:    1741860000,
		ProjectID:    "cloudaicompanion-test-project",
		Email:        "author@example.com",
	}

	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("Marshal 失敗: %v", err)
	}

	var decoded Credentials
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失敗: %v", err)
	}

	if decoded.AccessToken != creds.AccessToken ||
		decoded.RefreshToken != creds.RefreshToken ||
		decoded.ExpiresAt != creds.ExpiresAt ||
		decoded.ProjectID != creds.ProjectID ||
		decoded.Email != creds.Email {
		t.Errorf("解碼結果不符: %+v vs %+v", decoded, creds)
	}
}

func TestGetClientCredentials(t *testing.T) {
	cid := GetClientID()
	cs := GetClientSecret()
	if len(cid) < 40 {
		t.Errorf("GetClientID length %d too short", len(cid))
	}
	if len(cs) < 20 {
		t.Errorf("GetClientSecret length %d too short", len(cs))
	}
}

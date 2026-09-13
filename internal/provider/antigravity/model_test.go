package antigravity

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/voocel/agentcore"
)

func TestNewModelLazyCredentials(t *testing.T) {
	restore := SetAuthFilePathForTest(filepath.Join(t.TempDir(), "nonexistent.json"))
	defer restore()

	// 驗證未提供憑證且檔案不存在時，NewModel 不應出錯崩潰
	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("NewModel 不應在憑證未就緒時出錯: %v", err)
	}

	if m.ProviderName() != "google-antigravity" {
		t.Errorf("ProviderName = %s, 預期 google-antigravity", m.ProviderName())
	}
	if !m.SupportsTools() {
		t.Errorf("SupportsTools 預期為 true")
	}

	// 驗證未登入時發起請求，會優雅返回提示執行登入的錯誤
	_, err = m.Generate(context.Background(), []agentcore.Message{
		{
			Role:    agentcore.RoleUser,
			Content: []agentcore.ContentBlock{agentcore.TextBlock("hello")},
		},
	}, nil)
	if err == nil {
		t.Fatalf("未登入時 Generate 應返回錯誤")
	}
}

func TestHasCredentials(t *testing.T) {
	restore := SetAuthFilePathForTest(filepath.Join(t.TempDir(), "nonexistent.json"))
	defer restore()

	if HasCredentials() {
		t.Errorf("憑證檔案不存在時 HasCredentials 應為 false")
	}
}

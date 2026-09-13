package antigravity

import (
	"context"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
)

func TestLiveCallWithArbiter(t *testing.T) {
	if testing.Short() || !HasCredentials() {
		t.Skip("skipping live network test")
	}

	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := m.Generate(ctx, []agentcore.Message{
		agentcore.SystemMsg("You are a helpful assistant. Output a JSON object with answer field."),
		agentcore.UserMsg("hi"),
	}, nil)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	t.Logf("Response StopReason: %s", resp.Message.StopReason)
	t.Logf("TextContent: %q", resp.Message.TextContent())
	t.Logf("Content blocks count: %d", len(resp.Message.Content))
	for i, b := range resp.Message.Content {
		t.Logf("Block %d: type=%v, text=%q, thinking len=%d", i, b.Type, b.Text, len(b.Thinking))
	}
}

type testDecision struct {
	Answer string `json:"answer"`
	Reason string `json:"reason"`
}

func TestLiveLLMContractExecute(t *testing.T) {
	if testing.Short() || !HasCredentials() {
		t.Skip("skipping live network test")
	}

	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	contract := llmcontract.Contract{
		Name:        "arbiter_intervention",
		Description: "用户干预裁定",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"answer": map[string]any{"type": "string"},
				"reason": map[string]any{"type": "string"},
			},
			"required": []string{"answer", "reason"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := llmcontract.Execute(ctx, m, llmcontract.Request[testDecision]{
		Contract:     contract,
		SystemPrompt: "你是小说创作裁定器。根据用户指令做出裁定并给出 answer 与 reason。",
		Payload:      "hi",
	})
	if err != nil {
		t.Fatalf("llmcontract.Execute 失敗: %v", err)
	}

	t.Logf("✓ llmcontract.Execute 成功！Answer=%q, Reason=%q", result.Answer, result.Reason)
	if result.Answer == "" || result.Reason == "" {
		t.Errorf("結果欄位不可為空: %+v", result)
	}
}

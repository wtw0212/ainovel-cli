package antigravity

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/arbiter"
	"github.com/voocel/ainovel-cli/internal/llmcontract"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
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

func TestLiveArchitectTools(t *testing.T) {
	st := store.NewStore(t.TempDir())

	architectTools := []agentcore.Tool{
		tools.NewContextTool(st, tools.References{}, "default", tools.NewStyleStatsIndex(st)),
		tools.NewSaveBookTool(st),
		tools.NewSaveFoundationTool(st),
		tools.NewReviseOutlineTool(st),
		tools.NewResolveOutlineFeedbackTool(st),
		tools.NewAuditFoundationTool(st),
		tools.NewExpandNextArcTool(st),
	}

	var specs []agentcore.ToolSpec
	for _, tool := range architectTools {
		specs = append(specs, agentcore.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Schema(),
		})
	}

	converted := ConvertTools(specs)
	b, _ := json.MarshalIndent(converted, "", "  ")
	t.Logf("Converted tools:\n%s", string(b))

	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	streamCh, err := m.GenerateStream(ctx, []agentcore.Message{
		agentcore.SystemMsg("你是长篇小说规划师"),
		agentcore.UserMsg("请构建一部作品的基础设定"),
	}, specs)
	if err != nil {
		t.Fatalf("GenerateStream error: %v", err)
	}

	for ev := range streamCh {
		if ev.Type == agentcore.StreamEventError {
			t.Fatalf("Stream error: %v", ev.Err)
		}
	}
}

func TestLiveHelloIntervention(t *testing.T) {
	if testing.Short() || !HasCredentials() {
		t.Skip("skipping live network test")
	}

	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	bundle := assets.Load("default", assets.DefaultLoadOptions(""))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	facts := arbiter.InterventionFacts{
		Phase: "init",
	}

	decision, err := arbiter.DecideIntervention(ctx, m, bundle.Prompts.ArbiterIntervention, facts, "hello")
	if err != nil {
		t.Fatalf("DecideIntervention('hello') failed: %v", err)
	}

	t.Logf("✓ DecideIntervention('hello') PASS! Answer=%q, Reason=%q", decision.Answer, decision.Reason)
	if decision.Answer == "" && decision.Reason == "" {
		t.Errorf("Decision is empty: %+v", decision)
	}
}

func TestLiveHelloPlanStart(t *testing.T) {
	if testing.Short() || !HasCredentials() {
		t.Skip("skipping live network test")
	}

	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	bundle := assets.Load("default", assets.DefaultLoadOptions(""))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	decision, err := arbiter.DecidePlanStart(ctx, m, bundle.Prompts.ArbiterPlanStart, "hello", "default")
	if err != nil {
		t.Fatalf("DecidePlanStart('hello') failed: %v", err)
	}

	t.Logf("✓ DecidePlanStart('hello') PASS! Planner=%q, Task=%q, Reason=%q", decision.Planner, decision.Task, decision.Reason)
	if decision.Planner == "" || decision.Task == "" {
		t.Errorf("Decision has missing fields: %+v", decision)
	}
}

func TestLiveHelloEndToEndArchitect(t *testing.T) {
	if testing.Short() || !HasCredentials() {
		t.Skip("skipping live network test")
	}

	m, err := NewModel("gemini-3.8-flash", ModelOptions{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	bundle := assets.Load("default", assets.DefaultLoadOptions(""))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Arbiter decides on "hello"
	decision, err := arbiter.DecidePlanStart(ctx, m, bundle.Prompts.ArbiterPlanStart, "hello", "default")
	if err != nil {
		t.Fatalf("DecidePlanStart('hello') failed: %v", err)
	}
	t.Logf("Step 1 Arbiter: Planner=%s", decision.Planner)

	// Step 2: Architect runs with the 7 tools
	st := store.NewStore(t.TempDir())
	architectTools := []agentcore.Tool{
		tools.NewContextTool(st, tools.References{}, "default", tools.NewStyleStatsIndex(st)),
		tools.NewSaveBookTool(st),
		tools.NewSaveFoundationTool(st),
		tools.NewReviseOutlineTool(st),
		tools.NewResolveOutlineFeedbackTool(st),
		tools.NewAuditFoundationTool(st),
		tools.NewExpandNextArcTool(st),
	}

	var specs []agentcore.ToolSpec
	for _, tool := range architectTools {
		specs = append(specs, agentcore.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Schema(),
		})
	}

	streamCh, err := m.GenerateStream(ctx, []agentcore.Message{
		agentcore.SystemMsg(bundle.Prompts.ArchitectLong),
		agentcore.UserMsg(decision.Task),
	}, specs)
	if err != nil {
		t.Fatalf("Architect GenerateStream failed: %v", err)
	}

	var textLen int
	var doneMsg *agentcore.Message
	for ev := range streamCh {
		if ev.Type == agentcore.StreamEventError {
			t.Fatalf("Architect Stream Error: %v", ev.Err)
		}
		if ev.Type == agentcore.StreamEventTextDelta {
			textLen += len(ev.Delta)
		}
		if ev.Type == agentcore.StreamEventDone {
			doneMsg = &ev.Message
		}
	}

	if doneMsg == nil {
		t.Fatalf("Architect did not receive StreamEventDone")
	}

	t.Logf("✓ Step 2 Architect successfully responded! TextLen=%d, ToolCalls=%d, StopReason=%s", textLen, len(doneMsg.ToolCalls()), doneMsg.StopReason)
	for i, tc := range doneMsg.ToolCalls() {
		t.Logf("ToolCall[%d]: %s, args=%s", i, tc.Name, string(tc.Args))
	}
}




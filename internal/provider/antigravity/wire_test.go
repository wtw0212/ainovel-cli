package antigravity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
)

func TestResolveWireProfile(t *testing.T) {
	cases := []struct {
		input       string
		wantWire    string
		wantMaxTok  int
		wantClaude  bool
		wantModelEn string
	}{
		{
			input:      "claude-sonnet-4-6",
			wantWire:   "claude-sonnet-4-6",
			wantMaxTok: 64000,
			wantClaude: true,
		},
		{
			input:      "claude-opus-4-6-thinking",
			wantWire:   "claude-opus-4-6-thinking",
			wantMaxTok: 64000,
			wantClaude: true,
		},
		{
			input:       "gemini-3.5-flash-low",
			wantWire:    "gemini-3.5-flash-low",
			wantMaxTok:  65536,
			wantModelEn: "MODEL_PLACEHOLDER_M20",
		},
		{
			input:      "gemini-3.8-flash",
			wantWire:   "gemini-3.8-flash-low",
			wantMaxTok: 65536,
		},
		{
			input:       "gemini-3.5-flash",
			wantWire:    "gemini-3.5-flash-extra-low",
			wantMaxTok:  65536,
			wantModelEn: "MODEL_PLACEHOLDER_M20",
		},
		{
			input:       "gemini-3.1-pro",
			wantWire:    "gemini-3.1-pro-low",
			wantMaxTok:  65535,
			wantModelEn: "MODEL_PLACEHOLDER_M36",
		},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			p := ResolveWireProfile(tc.input)
			if p.WireModelID != tc.wantWire {
				t.Errorf("WireModelID = %q, want %q", p.WireModelID, tc.wantWire)
			}
			if p.MaxOutputTokens != tc.wantMaxTok {
				t.Errorf("MaxOutputTokens = %d, want %d", p.MaxOutputTokens, tc.wantMaxTok)
			}
			if p.IsClaude != tc.wantClaude {
				t.Errorf("IsClaude = %v, want %v", p.IsClaude, tc.wantClaude)
			}
			if tc.wantModelEn != "" && p.ModelEnum != tc.wantModelEn {
				t.Errorf("ModelEnum = %q, want %q", p.ModelEnum, tc.wantModelEn)
			}
		})
	}
}

func TestConvertMessages(t *testing.T) {
	msgs := []agentcore.Message{
		agentcore.SystemMsg("你是小說大綱規劃師"),
		agentcore.UserMsg("請規劃第一章"),
		{
			Role: agentcore.RoleAssistant,
			Content: []agentcore.ContentBlock{
				{Type: agentcore.ContentThinking, Thinking: "先構思主角登場"},
				agentcore.TextBlock("好的，這是第一章草案"),
				agentcore.ToolCallBlock(agentcore.ToolCall{
					ID:   "call_1",
					Name: "save_outline",
					Args: json.RawMessage(`{"title":"第一章"}`),
				}),
			},
		},
		{
			Role: agentcore.RoleTool,
			Content: []agentcore.ContentBlock{
				agentcore.TextBlock(`{"status":"saved"}`),
			},
			Metadata: map[string]any{"tool_name": "save_outline"},
		},
	}

	contents, sysInst := ConvertMessages(msgs)

	if sysInst == nil || len(sysInst.Parts) != 1 || sysInst.Parts[0].Text != "你是小說大綱規劃師" {
		t.Fatalf("SystemInstruction 轉換錯誤: %+v", sysInst)
	}
	if sysInst.Role != "user" {
		t.Errorf("Antigravity SystemInstruction Role 必須為 user，得到 %s", sysInst.Role)
	}

	if len(contents) != 3 {
		t.Fatalf("contents 長度 = %d, 預期 3", len(contents))
	}

	// 1. User
	if contents[0].Role != "user" || contents[0].Parts[0].Text != "請規劃第一章" {
		t.Errorf("contents[0] = %+v", contents[0])
	}

	// 2. Model (Assistant)
	if contents[1].Role != "model" || len(contents[1].Parts) != 3 {
		t.Fatalf("contents[1] = %+v", contents[1])
	}
	if !contents[1].Parts[0].Thought || contents[1].Parts[0].Text != "先構思主角登場" {
		t.Errorf("part[0] thinking 錯誤: %+v", contents[1].Parts[0])
	}
	if contents[1].Parts[1].Thought || contents[1].Parts[1].Text != "好的，這是第一章草案" {
		t.Errorf("part[1] text 錯誤: %+v", contents[1].Parts[1])
	}
	if contents[1].Parts[2].FunctionCall == nil || contents[1].Parts[2].FunctionCall.Name != "save_outline" {
		t.Errorf("part[2] toolCall 錯誤: %+v", contents[1].Parts[2])
	}

	// 3. Tool Response
	if contents[2].Role != "user" || len(contents[2].Parts) != 1 {
		t.Fatalf("contents[2] = %+v", contents[2])
	}
	fr := contents[2].Parts[0].FunctionResponse
	if fr == nil || fr.Name != "save_outline" {
		t.Errorf("FunctionResponse 錯誤: %+v", fr)
	}
}

func TestConvertTools(t *testing.T) {
	specs := []agentcore.ToolSpec{
		{
			Name:        "read_chapter",
			Description: "讀取某一章節內容",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"chapter": map[string]any{"type": "integer"},
				},
				"required": []string{"chapter"},
			},
		},
	}

	tools := ConvertTools(specs)
	if len(tools) != 1 || len(tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools 轉換失敗: %+v", tools)
	}

	decl := tools[0].FunctionDeclarations[0]
	if decl.Name != "read_chapter" || decl.Description != "讀取某一章節內容" {
		t.Errorf("decl 欄位不符: %+v", decl)
	}
}

func TestParseSSELine(t *testing.T) {
	// 忽略非 data 行
	chunk, err := ParseSSELine(": keep-alive")
	if err != nil || chunk != nil {
		t.Errorf("應忽略 comment 行，得到 chunk=%+v err=%v", chunk, err)
	}

	// 忽略 [DONE]
	chunk, err = ParseSSELine("data: [DONE]")
	if err != nil || chunk != nil {
		t.Errorf("應忽略 [DONE]，得到 chunk=%+v err=%v", chunk, err)
	}

	// 解析有效 payload
	sseData := `data: {"candidates":[{"content":{"parts":[{"text":"思考中","thought":true},{"text":"回答正文"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
	chunk, err = ParseSSELine(sseData)
	if err != nil {
		t.Fatalf("ParseSSELine 失敗: %v", err)
	}
	if chunk == nil || len(chunk.Candidates) != 1 {
		t.Fatalf("未解析到候選響應: %+v", chunk)
	}
	cand := chunk.Candidates[0]
	if cand.FinishReason != "STOP" {
		t.Errorf("finishReason = %s, want STOP", cand.FinishReason)
	}
	if len(cand.Content.Parts) != 2 {
		t.Fatalf("parts 長度 = %d, want 2", len(cand.Content.Parts))
	}
	if !cand.Content.Parts[0].Thought || cand.Content.Parts[0].Text != "思考中" {
		t.Errorf("thought part 錯誤: %+v", cand.Content.Parts[0])
	}
	if cand.Content.Parts[1].Thought || cand.Content.Parts[1].Text != "回答正文" {
		t.Errorf("text part 錯誤: %+v", cand.Content.Parts[1])
	}
	if chunk.UsageMetadata.TotalTokenCount != 15 {
		t.Errorf("usageMetadata 錯誤: %+v", chunk.UsageMetadata)
	}

	// 解析帶有 response 外層包裝的官方 CCA payload
	wrappedData := `data: {"response": {"candidates": [{"content": {"role": "model","parts": [{"text": "包裝回答"}]}}],"usageMetadata": {"promptTokenCount": 17,"candidatesTokenCount": 23,"totalTokenCount": 64}}}`
	chunk, err = ParseSSELine(wrappedData)
	if err != nil {
		t.Fatalf("ParseSSELine wrapped 失敗: %v", err)
	}
	if chunk == nil || len(chunk.Candidates) != 1 || chunk.Candidates[0].Content.Parts[0].Text != "包裝回答" {
		t.Fatalf("未正確解析 wrapped response: %+v", chunk)
	}
}

func TestAntigravityEnvelopeSerialization(t *testing.T) {
	env := AntigravityEnvelope{
		Project:     "project-12345",
		RequestID:   "agent/test-agent/12345/test-traj/2",
		UserAgent:   "antigravity",
		RequestType: "agent",
		Model:       "gemini-pro-agent",
		Request: CloudCodeAssistRequest{
			Contents: []Content{
				{
					Role:  "user",
					Parts: []Part{{Text: "你好"}},
				},
			},
			SessionID: "-123456789012345678",
			Labels: map[string]string{
				"model_enum":      "MODEL_PLACEHOLDER_M16",
				"last_step_index": "1",
			},
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal 失敗: %v", err)
	}

	str := string(data)
	if !strings.Contains(str, `"project":"project-12345"`) {
		t.Errorf("缺少 project 欄位: %s", str)
	}
	if !strings.Contains(str, `"userAgent":"antigravity"`) {
		t.Errorf("缺少 userAgent 欄位: %s", str)
	}
	if !strings.Contains(str, `"model":"gemini-pro-agent"`) {
		t.Errorf("缺少 model 欄位: %s", str)
	}
}

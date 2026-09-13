package antigravity

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
)

// AntigravityEnvelope 是發往 Google Cloud Code Assist 的外層封包。
type AntigravityEnvelope struct {
	Project     string                 `json:"project"`
	RequestID   string                 `json:"requestId"`
	UserAgent   string                 `json:"userAgent"`
	RequestType string                 `json:"requestType"`
	Model       string                 `json:"model"`
	Request     CloudCodeAssistRequest `json:"request"`
}

// CloudCodeAssistRequest 是 Google Cloud Code Assist streamGenerateContent 內層請求。
type CloudCodeAssistRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	SessionID         string            `json:"sessionId,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

// Content 代表 Gemini / Cloud Code Assist 訊息體。
type Content struct {
	Role  string `json:"role,omitempty"` // "user" 或 "model"
	Parts []Part `json:"parts"`
}

// Part 代表訊息中的片段（文本、思考、函式呼叫或函式回應）。
type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// FunctionCall 描述模型觸發的工具調用。
type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// FunctionResponse 描述回填給模型的工具執行結果。
type FunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// Tool 包裝 FunctionDeclarations。
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration 描述單個工具定義。
type FunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ToolConfig 控制工具呼叫行為。
type ToolConfig struct {
	FunctionCallingConfig FunctionCallingConfig `json:"functionCallingConfig"`
}

// FunctionCallingConfig 包含模式設定。
type FunctionCallingConfig struct {
	Mode                 string   `json:"mode"` // "AUTO", "ANY", "NONE", "VALIDATED"
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GenerationConfig 生成參數。
type GenerationConfig struct {
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	TopK            *int            `json:"topK,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// ThinkingConfig 思考模型參數。
type ThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts"`
	ThinkingBudget  *int `json:"thinkingBudget,omitempty"`
	ThinkingLevel   *int `json:"thinkingLevel,omitempty"`
}

// WireProfile 定義模型的後端線路代號與約束。
type WireProfile struct {
	WireModelID     string
	ModelEnum       string
	MaxOutputTokens int
	IsClaude        bool
}

// ResolveWireProfile 解析 Antigravity 線路配置。
func ResolveWireProfile(modelName string) WireProfile {
	name := strings.TrimSpace(strings.ToLower(modelName))
	switch {
	case strings.Contains(name, "claude-sonnet") || strings.Contains(name, "sonnet"):
		return WireProfile{
			WireModelID:     "claude-sonnet-4-6",
			MaxOutputTokens: 64000,
			IsClaude:        true,
		}
	case strings.Contains(name, "claude-opus") || strings.Contains(name, "opus"):
		return WireProfile{
			WireModelID:     "claude-opus-4-6-thinking",
			MaxOutputTokens: 64000,
			IsClaude:        true,
		}
	case strings.Contains(name, "3.8-flash") || strings.Contains(name, "3.8"):
		return WireProfile{
			WireModelID:     "gemini-3.8-flash-low",
			MaxOutputTokens: 65536,
		}
	case strings.Contains(name, "3.7-flash") || strings.Contains(name, "3.7"):
		return WireProfile{
			WireModelID:     "gemini-3.7-flash-low",
			MaxOutputTokens: 65536,
		}
	case strings.Contains(name, "3.6-flash") || strings.Contains(name, "3.6"):
		return WireProfile{
			WireModelID:     "gemini-3.6-flash-low",
			MaxOutputTokens: 65536,
		}
	case strings.Contains(name, "3.5-flash-low") || strings.Contains(name, "flash-low"):
		return WireProfile{
			WireModelID:     "gemini-3.5-flash-low",
			ModelEnum:       "MODEL_PLACEHOLDER_M20",
			MaxOutputTokens: 65536,
		}
	case strings.Contains(name, "3.5-flash"):
		return WireProfile{
			WireModelID:     "gemini-3.5-flash-extra-low",
			ModelEnum:       "MODEL_PLACEHOLDER_M20",
			MaxOutputTokens: 65536,
		}
	case strings.Contains(name, "flash"):
		return WireProfile{
			WireModelID:     "gemini-3.8-flash-low",
			MaxOutputTokens: 65536,
		}
	case strings.Contains(name, "3.1-pro-low") || strings.Contains(name, "pro-low"):
		return WireProfile{
			WireModelID:     "gemini-3.1-pro-low",
			ModelEnum:       "MODEL_PLACEHOLDER_M36",
			MaxOutputTokens: 65535,
		}
	case strings.Contains(name, "3.1-pro"):
		return WireProfile{
			WireModelID:     "gemini-3.1-pro-low",
			ModelEnum:       "MODEL_PLACEHOLDER_M36",
			MaxOutputTokens: 65535,
		}
	case strings.Contains(name, "2.5-pro"):
		return WireProfile{
			WireModelID:     "gemini-2.5-pro",
			MaxOutputTokens: 65535,
		}
	case strings.Contains(name, "2.5-flash"):
		return WireProfile{
			WireModelID:     "gemini-2.5-flash",
			MaxOutputTokens: 65535,
		}
	default:
		// 如果是已知具體線路 ID（例如 gemini-3.8-flash-high 等），直接採用該模型名稱
		return WireProfile{
			WireModelID:     modelName,
			MaxOutputTokens: 65535,
		}
	}
}

// ConvertMessages 將 agentcore.Message 序列轉為 CCA contents 與 systemInstruction。
func ConvertMessages(messages []agentcore.Message) (contents []Content, systemInstruction *Content) {
	var systemParts []Part

	for _, msg := range messages {
		if msg.Role == agentcore.RoleSystem {
			for _, b := range msg.Content {
				if b.Type == agentcore.ContentText && b.Text != "" {
					systemParts = append(systemParts, Part{Text: b.Text})
				}
			}
			continue
		}

		c := Content{}
		switch msg.Role {
		case agentcore.RoleAssistant:
			c.Role = "model"
			for _, b := range msg.Content {
				switch b.Type {
				case agentcore.ContentText:
					if b.Text != "" {
						c.Parts = append(c.Parts, Part{Text: b.Text})
					}
				case agentcore.ContentThinking:
					if b.Thinking != "" {
						c.Parts = append(c.Parts, Part{Text: b.Thinking, Thought: true})
					}
				case agentcore.ContentToolCall:
					if b.ToolCall != nil {
						argsMap := make(map[string]any)
						if len(b.ToolCall.Args) > 0 {
							_ = json.Unmarshal(b.ToolCall.Args, &argsMap)
						}
						c.Parts = append(c.Parts, Part{
							FunctionCall: &FunctionCall{
								Name: b.ToolCall.Name,
								Args: argsMap,
							},
						})
					}
				}
			}
		case agentcore.RoleTool:
			// Tool 回應在 Gemini / CCA 中是 user role 帶 FunctionResponse
			c.Role = "user"
			for _, b := range msg.Content {
				if b.Type == agentcore.ContentText {
					var respMap map[string]any
					if err := json.Unmarshal([]byte(b.Text), &respMap); err != nil {
						respMap = map[string]any{"response": b.Text}
					}
					toolName := ""
					if msg.Metadata != nil {
						if name, ok := msg.Metadata["tool_name"].(string); ok {
							toolName = name
						}
					}
					c.Parts = append(c.Parts, Part{
						FunctionResponse: &FunctionResponse{
							Name:     toolName,
							Response: respMap,
						},
					})
				}
			}
		case agentcore.RoleUser:
			fallthrough
		default:
			c.Role = "user"
			for _, b := range msg.Content {
				if b.Type == agentcore.ContentText && b.Text != "" {
					c.Parts = append(c.Parts, Part{Text: b.Text})
				}
			}
		}

		if len(c.Parts) > 0 {
			contents = append(contents, c)
		}
	}

	if len(systemParts) > 0 {
		systemInstruction = &Content{
			Role:  "user", // Antigravity 慣例：SystemInstruction 帶 role: "user"
			Parts: systemParts,
		}
	}

	return contents, systemInstruction
}

// ConvertTools 將 agentcore.ToolSpec 轉為 CCA Tool 宣告。
func ConvertTools(specs []agentcore.ToolSpec) []Tool {
	if len(specs) == 0 {
		return nil
	}
	var declarations []FunctionDeclaration
	for _, s := range specs {
		declarations = append(declarations, FunctionDeclaration{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  s.Parameters,
		})
	}
	return []Tool{{FunctionDeclarations: declarations}}
}

// StreamResponseChunk 是 SSE 返回的單個 JSON 塊。
type StreamResponseChunk struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

type sseEnvelope struct {
	Response      *StreamResponseChunk `json:"response,omitempty"`
	Candidates    []Candidate          `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata       `json:"usageMetadata,omitempty"`
}

// Candidate 候選響應。
type Candidate struct {
	Content      *CandidateContent `json:"content,omitempty"`
	FinishReason string            `json:"finishReason,omitempty"`
}

// CandidateContent 內容片段。
type CandidateContent struct {
	Parts []Part `json:"parts,omitempty"`
}

// UsageMetadata Token 用量統計。
type UsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
}

// ParseSSELine 解析 SSE 單行（移除 "data: " 前綴後轉為 StreamResponseChunk）。
func ParseSSELine(line string) (*StreamResponseChunk, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return nil, nil
	}
	jsonText := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if jsonText == "" || jsonText == "[DONE]" {
		return nil, nil
	}
	var env sseEnvelope
	if err := json.Unmarshal([]byte(jsonText), &env); err != nil {
		return nil, fmt.Errorf("unmarshal sse chunk: %w", err)
	}
	if env.Response != nil {
		return env.Response, nil
	}
	return &StreamResponseChunk{
		Candidates:    env.Candidates,
		UsageMetadata: env.UsageMetadata,
	}, nil
}

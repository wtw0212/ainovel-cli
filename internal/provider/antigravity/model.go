package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
)

// ModelOptions 建立 AntigravityModel 的選項。
type ModelOptions struct {
	BaseURL     string
	Credentials *Credentials
	ExtraBody   map[string]any
	Extra       map[string]any
}

// AntigravityModel 實作 agentcore.ChatModel 與 llm.CapabilityProvider。
type AntigravityModel struct {
	modelName   string
	wireProfile WireProfile
	creds       *Credentials
	httpClient  *http.Client
	extraBody   map[string]any

	agentID       string
	trajectoryID  string
	sessionID     string
	stepCounter   atomic.Int64
	lastEndpoint  string
	endpointMu    sync.RWMutex
	credsMu       sync.Mutex
}

// NewModel 建立 AntigravityModel 實例。
func NewModel(modelName string, opts ModelOptions) (*AntigravityModel, error) {
	creds := opts.Credentials
	if creds == nil {
		// 嘗試載入憑證，若尚未登入則延遲到調用時再檢查
		creds, _ = LoadCredentials()
	}

	profile := ResolveWireProfile(modelName)

	agentUUID := randomHex(16)
	trajUUID := randomHex(16)
	sessionID := randomDecimalSessionID()

	m := &AntigravityModel{
		modelName:    modelName,
		wireProfile:  profile,
		creds:        creds,
		httpClient:   &http.Client{Timeout: 5 * time.Minute},
		extraBody:    opts.ExtraBody,
		agentID:      agentUUID,
		trajectoryID: trajUUID,
		sessionID:    sessionID,
		lastEndpoint: CloudCodeAssistEndpoint,
	}
	m.stepCounter.Store(1)

	return m, nil
}

func (m *AntigravityModel) ProviderName() string {
	return "google-antigravity"
}

func (m *AntigravityModel) SupportsTools() bool {
	return true
}

func (m *AntigravityModel) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Name:     m.modelName,
		Provider: "google-antigravity",
	}
}

func (m *AntigravityModel) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Provider: "google-antigravity",
		Model:    m.modelName,
		Structured: llm.StructuredCapabilities{
			JSONSchema: llm.SupportYes,
			Strict:     llm.SupportNo, // Google Cloud Code Assist 採用 VALIDATED 模式，不支援 OpenAI 風格嚴格 strict
		},
	}
}

func (m *AntigravityModel) JSONSchemaOverride() *bool {
	// 讓框架使用 Capabilities
	return nil
}

// Generate 實作同步呼叫，內部委託 GenerateStream 執行並組裝最終響應。
func (m *AntigravityModel) Generate(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	streamCh, err := m.GenerateStream(ctx, messages, tools, opts...)
	if err != nil {
		return nil, err
	}

	var lastMsg agentcore.Message
	for ev := range streamCh {
		switch ev.Type {
		case agentcore.StreamEventError:
			if ev.Err != nil {
				return nil, ev.Err
			}
			return nil, fmt.Errorf("antigravity stream failed")
		case agentcore.StreamEventDone:
			lastMsg = ev.Message
		}
	}

	return &agentcore.LLMResponse{
		Message: lastMsg,
	}, nil
}

// GenerateStream 發起 SSE 串流並逐塊發送 agentcore.StreamEvent。
func (m *AntigravityModel) GenerateStream(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	out := make(chan agentcore.StreamEvent, 100)

	// 確保憑證已載入
	m.credsMu.Lock()
	if m.creds == nil {
		creds, err := LoadCredentials()
		if err != nil {
			m.credsMu.Unlock()
			return nil, fmt.Errorf("未找到 Google Antigravity 授權憑證，請先在終端執行 `ainovel-cli auth login antigravity` 完成登入: %w", err)
		}
		m.creds = creds
	}
	creds := m.creds
	m.credsMu.Unlock()

	// 確保 Token 有效
	token, err := EnsureValidToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("antigravity token: %w", err)
	}

	// 確保已探測 ProjectID
	projectID, err := DiscoverAndOnboardProject(ctx, creds, nil)
	if err != nil {
		return nil, fmt.Errorf("antigravity project: %w", err)
	}

	step := m.stepCounter.Add(1)

	// 轉換訊息與工具
	contents, systemInstruction := ConvertMessages(messages)
	convertedTools := ConvertTools(tools)

	callCfg := agentcore.ResolveCallConfig(opts)

	labels := map[string]string{
		"last_step_index": strconv.FormatInt(step-1, 10),
		"trajectory_id":   m.trajectoryID,
	}
	if m.wireProfile.ModelEnum != "" {
		labels["model_enum"] = m.wireProfile.ModelEnum
	}
	if m.wireProfile.IsClaude {
		labels["used_claude"] = "true"
		labels["used_claude_conservative"] = "true"
	} else {
		labels["used_claude"] = "false"
		labels["used_claude_conservative"] = "false"
	}

	genCfg := &GenerationConfig{
		MaxOutputTokens: m.wireProfile.MaxOutputTokens,
		ThinkingConfig: &ThinkingConfig{
			IncludeThoughts: true,
		},
	}
	if m.extraBody != nil {
		if tVal, ok := m.extraBody["temperature"]; ok {
			if f, ok := tVal.(float64); ok {
				genCfg.Temperature = &f
			}
		}
	}
	if callCfg.MaxTokens > 0 {
		genCfg.MaxOutputTokens = callCfg.MaxTokens
	}
	if callCfg.ThinkingBudget > 0 {
		tb := callCfg.ThinkingBudget
		genCfg.ThinkingConfig.ThinkingBudget = &tb
	}

	var toolConfig *ToolConfig
	if len(convertedTools) > 0 {
		toolConfig = &ToolConfig{
			FunctionCallingConfig: FunctionCallingConfig{
				Mode: "VALIDATED",
			},
		}
	} else if m.wireProfile.IsClaude {
		// Claude 在 Antigravity 上無 tools 時也需宣告 VALIDATED
		toolConfig = &ToolConfig{
			FunctionCallingConfig: FunctionCallingConfig{
				Mode: "VALIDATED",
			},
		}
	}

	ccaReq := CloudCodeAssistRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		Tools:             convertedTools,
		ToolConfig:        toolConfig,
		GenerationConfig:  genCfg,
		SessionID:         m.sessionID,
		Labels:            labels,
	}

	reqID := fmt.Sprintf("agent/%s/%d/%s/%d", m.agentID, time.Now().UnixMilli(), m.trajectoryID, step)

	envelope := AntigravityEnvelope{
		Project:     projectID,
		RequestID:   reqID,
		UserAgent:   "antigravity",
		RequestType: "agent",
		Model:       m.wireProfile.WireModelID,
		Request:     ccaReq,
	}

	bodyBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal antigravity envelope: %w", err)
	}

	go m.executeStream(ctx, token, bodyBytes, out)

	return out, nil
}

func (m *AntigravityModel) executeStream(ctx context.Context, token string, bodyBytes []byte, out chan<- agentcore.StreamEvent) {
	defer close(out)

	endpoints := []string{CloudCodeAssistEndpoint, CloudCodeSandboxEndpoint}
	m.endpointMu.RLock()
	if m.lastEndpoint == CloudCodeSandboxEndpoint {
		endpoints = []string{CloudCodeSandboxEndpoint, CloudCodeAssistEndpoint}
	}
	m.endpointMu.RUnlock()

	var lastErr error

	for i, endpoint := range endpoints {
		requestURL := fmt.Sprintf("%s/v1internal:streamGenerateContent?alt=sse", endpoint)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(bodyBytes))
		if err != nil {
			out <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: err}
			return
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("User-Agent", DefaultUserAgent)
		if m.wireProfile.IsClaude {
			req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
		}

		resp, err := m.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("Cloud Code Assist error (%d): %s", resp.StatusCode, string(body))
			// 若為暫態錯誤且非最後端點，切換端點重試
			if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && i < len(endpoints)-1 {
				continue
			}
			out <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: lastErr}
			return
		}

		// 成功收到響應，記錄此端點
		m.endpointMu.Lock()
		m.lastEndpoint = endpoint
		m.endpointMu.Unlock()

		m.readSSEStream(resp.Body, out)
		resp.Body.Close()
		return
	}

	if lastErr != nil {
		out <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: lastErr}
	}
}

func (m *AntigravityModel) readSSEStream(reader io.Reader, out chan<- agentcore.StreamEvent) {
	scanner := bufio.NewScanner(reader)
	// 放大單行 buffer，避免大區塊溢位（4MB）
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	var textBuilder strings.Builder
	var thinkingBuilder strings.Builder
	var toolCalls []agentcore.ToolCall
	var stopReason agentcore.StopReason = agentcore.StopReasonStop
	var usage *agentcore.Usage

	for scanner.Scan() {
		line := scanner.Text()
		chunk, err := ParseSSELine(line)
		if err != nil {
			out <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: err}
			return
		}
		if chunk == nil {
			continue
		}

		if chunk.UsageMetadata != nil {
			usage = &agentcore.Usage{
				Input:       chunk.UsageMetadata.PromptTokenCount,
				Output:      chunk.UsageMetadata.CandidatesTokenCount + chunk.UsageMetadata.ThoughtsTokenCount,
				TotalTokens: chunk.UsageMetadata.TotalTokenCount,
				CacheRead:   chunk.UsageMetadata.CachedContentTokenCount,
			}
		}

		for _, cand := range chunk.Candidates {
			if cand.FinishReason != "" {
				switch cand.FinishReason {
				case "MAX_TOKENS":
					stopReason = agentcore.StopReasonLength
				case "SAFETY":
					stopReason = agentcore.StopReasonSafety
				default:
					stopReason = agentcore.StopReasonStop
				}
			}

			if cand.Content == nil {
				continue
			}

			for _, part := range cand.Content.Parts {
				if part.Thought {
					thinkingBuilder.WriteString(part.Text)
					out <- agentcore.StreamEvent{
						Type:  agentcore.StreamEventThinkingDelta,
						Delta: part.Text,
					}
				} else if part.Text != "" {
					textBuilder.WriteString(part.Text)
					out <- agentcore.StreamEvent{
						Type:  agentcore.StreamEventTextDelta,
						Delta: part.Text,
					}
				}

				if part.FunctionCall != nil {
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					toolCalls = append(toolCalls, agentcore.ToolCall{
						ID:   fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, len(toolCalls)+1),
						Name: part.FunctionCall.Name,
						Args: argsBytes,
					})
				}
			}
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		out <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: fmt.Errorf("sse scan error: %w", err)}
		return
	}

	// 組裝最終訊息
	var blocks []agentcore.ContentBlock
	if thinkingBuilder.Len() > 0 {
		blocks = append(blocks, agentcore.ContentBlock{
			Type:     agentcore.ContentThinking,
			Thinking: thinkingBuilder.String(),
		})
	}
	if textBuilder.Len() > 0 {
		blocks = append(blocks, agentcore.TextBlock(textBuilder.String()))
	}
	if len(toolCalls) > 0 {
		stopReason = agentcore.StopReasonToolUse
		for _, tc := range toolCalls {
			blocks = append(blocks, agentcore.ToolCallBlock(tc))
		}
	}

	finalMsg := agentcore.Message{
		Role:       agentcore.RoleAssistant,
		Content:    blocks,
		StopReason: stopReason,
		Usage:      usage,
	}

	out <- agentcore.StreamEvent{
		Type:       agentcore.StreamEventDone,
		Message:    finalMsg,
		StopReason: stopReason,
	}
}

func randomHex(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func randomDecimalSessionID() string {
	// 生成有符號 64-bit 十進制字串，模擬官方 Antigravity sessionId
	n, _ := rand.Int(rand.Reader, big.NewInt(9000000000000000000))
	return fmt.Sprintf("-%d", n.Int64()+1000000000000000000)
}

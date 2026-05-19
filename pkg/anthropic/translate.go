package anthropic

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

// TranslateRequest converts an Anthropic MessageRequest to a JoyCode API body.
func TranslateRequest(req *MessageRequest, accountDefault string, systemDefault string) map[string]interface{} {
	model := resolveModel(req.Model, accountDefault, systemDefault)
	messages := buildMessages(req)

	body := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"stream":     req.Stream,
		"max_tokens": req.MaxTokens,
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		body["stop"] = req.StopSequences
	}
	if len(req.Tools) > 0 {
		body["tools"] = convertToolsToOpenAI(req.Tools)
	}
	if len(req.ToolChoice) > 0 {
		if tc := convertToolChoice(req.ToolChoice); tc != nil {
			body["tool_choice"] = tc
		}
	}
	return body
}

// TranslateAnthropicRequest converts an Anthropic request to JoyCode's native
// Anthropic endpoint body. Claude-family models reject the legacy OpenAI path.
func TranslateAnthropicRequest(req *MessageRequest, accountDefault string, systemDefault string) map[string]interface{} {
	model := resolveNativeAnthropicModel(req.Model, accountDefault, systemDefault)
	body := map[string]interface{}{
		"model":      model,
		"messages":   req.Messages,
		"stream":     true,
		"max_tokens": req.MaxTokens,
		"thinking":   map[string]string{"type": "disabled"},
	}
	if req.System != nil {
		body["system"] = normalizeAnthropicSystem(req.System)
	}
	if len(req.StopSequences) > 0 {
		body["stop_sequences"] = req.StopSequences
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if len(req.ToolChoice) > 0 {
		body["tool_choice"] = json.RawMessage(req.ToolChoice)
	}
	return body
}

func normalizeAnthropicSystem(raw json.RawMessage) interface{} {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []map[string]interface{}{{"type": "text", "text": s}}
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	return raw
}

func IsNativeAnthropicModel(model string) bool {
	m := strings.ToLower(model)
	return strings.HasPrefix(m, "claude") || strings.Contains(m, "claude-")
}

func resolveNativeAnthropicModel(model string, accountDefault string, systemDefault string) string {
	resolved := resolveModel(model, accountDefault, systemDefault)
	if resolved == "Claude-Opus-4.7" {
		return resolved
	}
	if IsNativeAnthropicModel(resolved) {
		return "Claude-Opus-4.7"
	}
	return resolved
}

// convertToolsToOpenAI converts Anthropic-format tools to OpenAI function-calling format.
func convertToolsToOpenAI(tools []Tool) []interface{} {
	result := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		tool := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		}
		result = append(result, tool)
	}
	return result
}

// TranslateResponse converts a JoyCode API response to Anthropic Message format.
func TranslateResponse(jcResp map[string]interface{}, reqModel string) *MessageResponse {
	msgID := "msg_" + newID()
	usage := extractUsage(jcResp)

	choices, _ := jcResp["choices"].([]interface{})
	if len(choices) == 0 {
		return &MessageResponse{
			ID: msgID, Type: "message", Role: "assistant",
			Model: reqModel, Content: []ContentBlock{{Type: "text", Text: ""}},
			StopReason: strPtr("end_turn"), Usage: usage,
		}
	}
	choice, _ := choices[0].(map[string]interface{})
	msg, _ := choice["message"].(map[string]interface{})

	content := []ContentBlock{}
	stopReason := "end_turn"

	// Handle tool_calls from JoyCode response
	toolCalls, _ := msg["tool_calls"].([]interface{})
	if len(toolCalls) > 0 {
		stopReason = "tool_use"
		for _, tc := range toolCalls {
			tcMap, _ := tc.(map[string]interface{})
			fn, _ := tcMap["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			id, _ := tcMap["id"].(string)
			if id == "" {
				id = "toolu_" + newID()
			}
			if argsStr == "" || !json.Valid([]byte(argsStr)) {
				argsStr = "{}"
			}

			var input json.RawMessage = json.RawMessage(argsStr)
			content = append(content, ContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  name,
				Input: input,
			})
		}
	} else {
		text, _ := msg["content"].(string)
		content = append(content, ContentBlock{Type: "text", Text: text})
	}

	return &MessageResponse{
		ID:         msgID,
		Type:       "message",
		Role:       "assistant",
		Model:      reqModel,
		Content:    content,
		StopReason: &stopReason,
		Usage:      usage,
	}
}

func resolveModel(model string, accountDefault string, systemDefault string) string {
	for _, m := range joycode.Models {
		if m == model {
			return model
		}
	}
	if accountDefault != "" {
		return accountDefault
	}
	if systemDefault != "" {
		return systemDefault
	}
	return joycode.DefaultModel
}

// contentBlock represents a single content block in Anthropic format.
type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result fields
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

func buildMessages(req *MessageRequest) []map[string]interface{} {
	msgs := make([]map[string]interface{}, 0, len(req.Messages)+1)

	if req.System != nil {
		if sys := parseContent(req.System); sys != "" {
			msgs = append(msgs, map[string]interface{}{
				"role": "system", "content": sys,
			})
		}
	}

	// Collect all tool_use IDs from assistant messages so we can strip orphaned tool_results
	toolUseIDs := map[string]bool{}
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			var blocks []contentBlock
			if json.Unmarshal(m.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "tool_use" && b.ID != "" {
						toolUseIDs[b.ID] = true
					}
				}
			}
		}
	}

	for _, m := range req.Messages {
		for _, converted := range convertMessage(m.Role, m.Content, toolUseIDs) {
			msgs = append(msgs, converted)
		}
	}
	return msgs
}

// convertMessage converts a single Anthropic message to one or more OpenAI format messages.
func convertMessage(role string, raw json.RawMessage, toolUseIDs map[string]bool) []map[string]interface{} {
	// Try simple string content first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []map[string]interface{}{{"role": role, "content": s}}
	}

	// Try as content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return []map[string]interface{}{{"role": role, "content": string(raw)}}
	}

	switch role {
	case "assistant":
		return []map[string]interface{}{convertAssistantBlocks(blocks)}
	case "user":
		return convertUserBlocks(blocks, toolUseIDs)
	default:
		return []map[string]interface{}{{"role": role, "content": extractText(blocks)}}
	}
}

// convertAssistantBlocks handles assistant messages with tool_use blocks.
func convertAssistantBlocks(blocks []contentBlock) map[string]interface{} {
	textParts := []string{}
	toolCalls := []interface{}{}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "tool_use":
			args := "{}"
			if len(b.Input) > 0 {
				args = string(b.Input)
			}
			id := b.ID
			if id == "" {
				id = "call_" + newID()
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      b.Name,
					"arguments": args,
				},
			})
		}
	}

	msg := map[string]interface{}{
		"role":      "assistant",
		"content":   strings.Join(textParts, "\n"),
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		if msg["content"] == "" {
			msg["content"] = nil
		}
	}
	return msg
}

// convertUserBlocks handles user messages that may contain tool_result blocks.
// tool_result blocks are converted to separate "tool" role messages in OpenAI format.
// Orphaned tool_results (whose tool_use was removed by truncation) are stripped.
func convertUserBlocks(blocks []contentBlock, toolUseIDs map[string]bool) []map[string]interface{} {
	var result []map[string]interface{}
	var textParts []string

	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "tool_result":
			// Skip orphaned tool_results whose tool_use was removed by truncation
			if b.ToolUseID != "" && !toolUseIDs[b.ToolUseID] {
				continue
			}
			// Convert each tool_result to an OpenAI "tool" role message
			resultText := extractToolResultContent(b.Content)
			result = append(result, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": b.ToolUseID,
				"content":      resultText,
			})
		}
	}

	// If there's remaining text, add it as a user message after tool results
	if len(textParts) > 0 && len(result) > 0 {
		result = append(result, map[string]interface{}{
			"role": "user", "content": strings.Join(textParts, "\n"),
		})
	}

	// If no tool_result blocks, return as single user message
	if len(result) == 0 {
		return []map[string]interface{}{{"role": "user", "content": strings.Join(textParts, "\n")}}
	}

	return result
}

func extractToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func extractText(blocks []contentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func parseContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func extractUsage(jcResp map[string]interface{}) Usage {
	u := Usage{}
	usage, _ := jcResp["usage"].(map[string]interface{})
	if usage == nil {
		return u
	}
	if v, ok := usage["prompt_tokens"].(float64); ok {
		u.InputTokens = int(v)
	}
	if v, ok := usage["completion_tokens"].(float64); ok {
		u.OutputTokens = int(v)
	}
	return u
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func strPtr(s string) *string { return &s }

// convertToolChoice converts Anthropic tool_choice to OpenAI format.
func convertToolChoice(raw json.RawMessage) interface{} {
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil
	}
	switch tc.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		if tc.Name != "" {
			return map[string]interface{}{
				"type": "function",
				"function": map[string]string{"name": tc.Name},
			}
		}
		return "auto"
	default:
		return nil
	}
}

// NewMessageID generates a message ID in Anthropic format.
func NewMessageID() string {
	return "msg_" + newID()
}

// StreamChunk represents a parsed SSE chunk from JoyCode.
type StreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Index    int    `json:"index"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ParseStreamChunk parses a single SSE data line into a StreamChunk.
func ParseStreamChunk(line string) *StreamChunk {
	line = strings.TrimPrefix(line, "data: ")
	line = strings.TrimSpace(line)
	if line == "" || line == "[DONE]" {
		return nil
	}
	var chunk StreamChunk
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return nil
	}
	return &chunk
}

// ParseStreamDelta extracts text content from an OpenAI SSE chunk.
func ParseStreamDelta(line string) string {
	chunk := ParseStreamChunk(line)
	if chunk == nil || len(chunk.Choices) == 0 {
		return ""
	}
	return chunk.Choices[0].Delta.Content
}

// FormatSSE writes a single SSE event to the writer.
func FormatSSE(w interface{ Write([]byte) (int, error) }, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
}

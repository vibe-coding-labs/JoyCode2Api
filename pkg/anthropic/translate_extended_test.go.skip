package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveModel (cases 1-4)
// ---------------------------------------------------------------------------

func TestResolveModel_AllModelMapping(t *testing.T) {
	// Case 1: Every entry in ModelMapping returns the correct JoyCode model.
	for anthropicModel, expected := range ModelMapping {
		got := resolveModel(anthropicModel)
		if got != expected {
			t.Errorf("resolveModel(%q) = %q, want %q", anthropicModel, got, expected)
		}
	}
}

func TestResolveModel_KnownJoyCodePassThrough(t *testing.T) {
	// Case 2: A known JoyCode model (e.g. "GLM-5.1") passes through directly.
	input := "GLM-5.1"
	got := resolveModel(input)
	if got != input {
		t.Errorf("resolveModel(%q) = %q, want %q (pass-through)", input, got, input)
	}
}

func TestResolveModel_UnknownDefaults(t *testing.T) {
	// Case 3: Unknown model defaults to JoyAI-Code.
	got := resolveModel("some-unknown-model-xyz")
	if got != "JoyAI-Code" {
		t.Errorf("resolveModel(unknown) = %q, want JoyAI-Code", got)
	}
}

func TestResolveModel_EmptyString(t *testing.T) {
	// Case 4: Empty string defaults to JoyAI-Code.
	got := resolveModel("")
	if got != "JoyAI-Code" {
		t.Errorf("resolveModel('') = %q, want JoyAI-Code", got)
	}
}

// ---------------------------------------------------------------------------
// TranslateRequest (cases 5-12)
// ---------------------------------------------------------------------------

func TestTranslateRequest_Basic(t *testing.T) {
	// Case 5: Basic request with model + messages + max_tokens.
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
	body := TranslateRequest(req)

	if body["model"] != "JoyAI-Code" {
		t.Errorf("model = %v, want JoyAI-Code", body["model"])
	}
	if body["max_tokens"] != 1024 {
		t.Errorf("max_tokens = %v, want 1024", body["max_tokens"])
	}
	msgs, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages type = %T", body["messages"])
	}
	if len(msgs) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(msgs))
	}
	if msgs[0]["role"] != "user" {
		t.Errorf("message role = %v, want user", msgs[0]["role"])
	}
}

func TestTranslateRequest_SystemString(t *testing.T) {
	// Case 6: System message as a plain string.
	req := &MessageRequest{
		Model:     "claude-sonnet-4",
		MaxTokens: 512,
		System:    json.RawMessage(`"You are a helpful assistant"`),
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}
	body := TranslateRequest(req)
	msgs := body["messages"].([]map[string]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Errorf("first message role = %q, want system", msgs[0]["role"])
	}
	if msgs[0]["content"] != "You are a helpful assistant" {
		t.Errorf("system content = %q, want 'You are a helpful assistant'", msgs[0]["content"])
	}
}

func TestTranslateRequest_SystemArray(t *testing.T) {
	// Case 7: System message as array of content blocks.
	req := &MessageRequest{
		Model:     "claude-sonnet-4",
		MaxTokens: 512,
		System:    json.RawMessage(`[{"type":"text","text":"You are helpful"},{"type":"text","text":"Be concise"}]`),
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}
	body := TranslateRequest(req)
	msgs := body["messages"].([]map[string]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Errorf("first message role = %q, want system", msgs[0]["role"])
	}
	// Two text blocks joined by newline.
	expected := "You are helpful\nBe concise"
	if msgs[0]["content"] != expected {
		t.Errorf("system content = %q, want %q", msgs[0]["content"], expected)
	}
}

func TestTranslateRequest_WithTemperature(t *testing.T) {
	// Case 8: Request with temperature set.
	temp := 0.5
	req := &MessageRequest{
		Model:       "claude-sonnet-4-20250514",
		MaxTokens:   256,
		Temperature: &temp,
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
	body := TranslateRequest(req)
	if body["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", body["temperature"])
	}
}

func TestTranslateRequest_WithTopP(t *testing.T) {
	// Case 9: Request with top_p set.
	topP := 0.9
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 256,
		TopP:      &topP,
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
	body := TranslateRequest(req)
	if body["top_p"] != 0.9 {
		t.Errorf("top_p = %v, want 0.9", body["top_p"])
	}
}

func TestTranslateRequest_WithStopSequences(t *testing.T) {
	// Case 10: Request with stop_sequences.
	req := &MessageRequest{
		Model:         "claude-sonnet-4-20250514",
		MaxTokens:     256,
		StopSequences: []string{"END", "STOP"},
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}
	body := TranslateRequest(req)
	stops, ok := body["stop"].([]string)
	if !ok {
		t.Fatalf("stop type = %T", body["stop"])
	}
	if len(stops) != 2 || stops[0] != "END" || stops[1] != "STOP" {
		t.Errorf("stop = %v, want [END STOP]", stops)
	}
}

func TestTranslateRequest_WithTools(t *testing.T) {
	// Case 11: Request with tools, verify OpenAI function format.
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Tools: []Tool{
			{
				Name:        "get_weather",
				Description: "Get the weather",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		},
		Messages: []MessageParam{
			{Role: "user", Content: json.RawMessage(`"What is the weather?"`)},
		},
	}
	body := TranslateRequest(req)
	tools, ok := body["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools type = %T", body["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(tools))
	}
	toolMap, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool entry type = %T", tools[0])
	}
	if toolMap["type"] != "function" {
		t.Errorf("tool type = %v, want function", toolMap["type"])
	}
	fn, ok := toolMap["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("function type = %T", toolMap["function"])
	}
	if fn["name"] != "get_weather" {
		t.Errorf("function name = %v, want get_weather", fn["name"])
	}
	if fn["description"] != "Get the weather" {
		t.Errorf("function description = %v, want 'Get the weather'", fn["description"])
	}
	if fn["parameters"] == nil {
		t.Error("function parameters is nil, want non-nil")
	}
}

func TestTranslateRequest_EmptyMessages(t *testing.T) {
	// Case 12: Empty messages array produces an empty messages slice.
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  []MessageParam{},
	}
	body := TranslateRequest(req)
	msgs, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages type = %T", body["messages"])
	}
	if len(msgs) != 0 {
		t.Errorf("len(messages) = %d, want 0", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// TranslateResponse (cases 13-20)
// ---------------------------------------------------------------------------

func TestTranslateResponse_NormalText(t *testing.T) {
	// Case 13: Normal text response.
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"content": "Hello there!",
				},
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(20),
			"completion_tokens": float64(10),
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	if resp.Type != "message" {
		t.Errorf("Type = %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", resp.Role)
	}
	if resp.StopReason == nil || *resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want end_turn", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "text" {
		t.Fatalf("Content unexpected: %v", resp.Content)
	}
	if resp.Content[0].Text != "Hello there!" {
		t.Errorf("Text = %q, want 'Hello there!'", resp.Content[0].Text)
	}
	if resp.Usage.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", resp.Usage.OutputTokens)
	}
}

func TestTranslateResponse_ToolUse(t *testing.T) {
	// Case 14: Tool use response with stop_reason="tool_use".
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   "call_abc123",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "get_weather",
								"arguments": `{"city":"London"}`,
							},
						},
					},
				},
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(30),
			"completion_tokens": float64(15),
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4")

	if resp.StopReason == nil || *resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %v, want tool_use", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	block := resp.Content[0]
	if block.Type != "tool_use" {
		t.Errorf("ContentBlock type = %q, want tool_use", block.Type)
	}
	if block.ID != "call_abc123" {
		t.Errorf("ContentBlock ID = %q, want call_abc123", block.ID)
	}
	if block.Name != "get_weather" {
		t.Errorf("ContentBlock Name = %q, want get_weather", block.Name)
	}
	// Input should contain the JSON arguments.
	inputBytes, _ := json.Marshal(block.Input)
	if string(inputBytes) != `{"city":"London"}` {
		t.Errorf("ContentBlock Input = %s, want {\"city\":\"London\"}", inputBytes)
	}
}

func TestTranslateResponse_EmptyChoices(t *testing.T) {
	// Case 15: Empty choices returns a text content block with empty text.
	jcResp := map[string]interface{}{
		"choices": []interface{}{},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content type = %q, want text", resp.Content[0].Type)
	}
	if resp.Content[0].Text != "" {
		t.Errorf("Content text = %q, want empty string", resp.Content[0].Text)
	}
	if resp.StopReason == nil || *resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want end_turn", resp.StopReason)
	}
}

func TestTranslateResponse_NoUsageData(t *testing.T) {
	// Case 16: Response with no usage data; Usage should be zero-valued.
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"content": "No usage info",
				},
			},
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	if resp.Usage.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0", resp.Usage.OutputTokens)
	}
}

func TestTranslateResponse_MissingMessageField(t *testing.T) {
	// Case 17: Response with missing message field (choice has no "message" key).
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				// no "message" key at all
			},
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	// Should still produce a valid response with a text block (empty content).
	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content type = %q, want text", resp.Content[0].Type)
	}
}

func TestTranslateResponse_ToolCallMissingID(t *testing.T) {
	// Case 18: Tool call with missing ID gets auto-generated "toolu_" prefix.
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"tool_calls": []interface{}{
						map[string]interface{}{
							// "id" is missing
							"type": "function",
							"function": map[string]interface{}{
								"name":      "search",
								"arguments": `{"q":"test"}`,
							},
						},
					},
				},
			},
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	if len(resp.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(resp.Content))
	}
	block := resp.Content[0]
	if !strings.HasPrefix(block.ID, "toolu_") {
		t.Errorf("auto-generated ID = %q, want toolu_ prefix", block.ID)
	}
	// The ID should have meaningful length beyond the prefix.
	if len(block.ID) <= len("toolu_") {
		t.Errorf("auto-generated ID too short: %q", block.ID)
	}
}

func TestTranslateResponse_MultipleToolCalls(t *testing.T) {
	// Case 19: Multiple tool calls in a single response.
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   "call_001",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "get_weather",
								"arguments": `{"city":"NYC"}`,
							},
						},
						map[string]interface{}{
							"id":   "call_002",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "search",
								"arguments": `{"q":"weather NYC"}`,
							},
						},
					},
				},
			},
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	if len(resp.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Name != "get_weather" {
		t.Errorf("Content[0].Name = %q, want get_weather", resp.Content[0].Name)
	}
	if resp.Content[1].Name != "search" {
		t.Errorf("Content[1].Name = %q, want search", resp.Content[1].Name)
	}
	if resp.StopReason == nil || *resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %v, want tool_use", resp.StopReason)
	}
}

func TestTranslateResponse_IDPrefix(t *testing.T) {
	// Case 20: Verify response ID has "msg_" prefix.
	jcResp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"content": "test",
				},
			},
		},
	}
	resp := TranslateResponse(jcResp, "claude-sonnet-4-20250514")

	if !strings.HasPrefix(resp.ID, "msg_") {
		t.Errorf("ID = %q, want msg_ prefix", resp.ID)
	}
	// Verify the ID has meaningful length: "msg_" (4 chars) + 24 hex chars.
	if len(resp.ID) < 10 {
		t.Errorf("ID = %q seems too short", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// ParseStreamChunk (cases 21-28)
// ---------------------------------------------------------------------------

func TestParseStreamChunk_ValidText(t *testing.T) {
	// Case 21: Valid text chunk.
	line := `data: {"choices":[{"delta":{"content":"Hello world"}}]}`
	chunk := ParseStreamChunk(line)
	if chunk == nil {
		t.Fatal("ParseStreamChunk returned nil for valid chunk")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(chunk.Choices))
	}
	if chunk.Choices[0].Delta.Content != "Hello world" {
		t.Errorf("Delta.Content = %q, want 'Hello world'", chunk.Choices[0].Delta.Content)
	}
}

func TestParseStreamChunk_Done(t *testing.T) {
	// Case 22: [DONE] returns nil.
	chunk := ParseStreamChunk("data: [DONE]")
	if chunk != nil {
		t.Errorf("ParseStreamChunk([DONE]) = %v, want nil", chunk)
	}
}

func TestParseStreamChunk_EmptyLine(t *testing.T) {
	// Case 23: Empty line returns nil.
	chunk := ParseStreamChunk("")
	if chunk != nil {
		t.Errorf("ParseStreamChunk('') = %v, want nil", chunk)
	}
}

func TestParseStreamChunk_InvalidJSON(t *testing.T) {
	// Case 24: Invalid JSON returns nil.
	chunk := ParseStreamChunk("data: {not valid json!!!}")
	if chunk != nil {
		t.Errorf("ParseStreamChunk(invalid) = %v, want nil", chunk)
	}
}

func TestParseStreamChunk_ToolCallsDelta(t *testing.T) {
	// Case 25: Chunk with tool_calls delta.
	line := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"search","arguments":"{\"q\":"}}]}}]}`
	chunk := ParseStreamChunk(line)
	if chunk == nil {
		t.Fatal("ParseStreamChunk returned nil for tool_calls chunk")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(chunk.Choices))
	}
	tcs := chunk.Choices[0].Delta.ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(tcs))
	}
	if tcs[0].ID != "call_1" {
		t.Errorf("ToolCall ID = %q, want call_1", tcs[0].ID)
	}
	if tcs[0].Function.Name != "search" {
		t.Errorf("ToolCall Function.Name = %q, want search", tcs[0].Function.Name)
	}
	if tcs[0].Function.Arguments != `{"q":` {
		t.Errorf("ToolCall Function.Arguments = %q, unexpected", tcs[0].Function.Arguments)
	}
}

func TestParseStreamChunk_FinishReason(t *testing.T) {
	// Case 26: Chunk with finish_reason.
	line := `data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`
	chunk := ParseStreamChunk(line)
	if chunk == nil {
		t.Fatal("ParseStreamChunk returned nil for finish_reason chunk")
	}
	if chunk.Choices[0].FinishReason == nil {
		t.Fatal("FinishReason is nil, want non-nil")
	}
	if *chunk.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", *chunk.Choices[0].FinishReason)
	}
}

func TestParseStreamChunk_NoChoices(t *testing.T) {
	// Case 27: Chunk with no choices array.
	line := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk"}`
	chunk := ParseStreamChunk(line)
	if chunk == nil {
		t.Fatal("ParseStreamChunk returned nil for no-choices chunk")
	}
	if len(chunk.Choices) != 0 {
		t.Errorf("len(Choices) = %d, want 0", len(chunk.Choices))
	}
}

func TestParseStreamChunk_TextAndToolCalls(t *testing.T) {
	// Case 28: Chunk with both text and tool_calls in the delta.
	line := `data: {"choices":[{"delta":{"content":"thinking","tool_calls":[{"index":0,"id":"call_x","function":{"name":"calc","arguments":"{\"a\":1"}}]}}]}`
	chunk := ParseStreamChunk(line)
	if chunk == nil {
		t.Fatal("ParseStreamChunk returned nil")
	}
	choice := chunk.Choices[0]
	if choice.Delta.Content != "thinking" {
		t.Errorf("Delta.Content = %q, want 'thinking'", choice.Delta.Content)
	}
	if len(choice.Delta.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(choice.Delta.ToolCalls))
	}
	if choice.Delta.ToolCalls[0].Function.Name != "calc" {
		t.Errorf("ToolCall name = %q, want calc", choice.Delta.ToolCalls[0].Function.Name)
	}
}

// ---------------------------------------------------------------------------
// parseContent (cases 29-32)
// ---------------------------------------------------------------------------

func TestParseContent_PlainString(t *testing.T) {
	// Case 29: Plain string.
	got := parseContent(json.RawMessage(`"hello world"`))
	if got != "hello world" {
		t.Errorf("parseContent(string) = %q, want 'hello world'", got)
	}
}

func TestParseContent_ArrayOfTextBlocks(t *testing.T) {
	// Case 30: Array of text blocks joined with newline.
	input := `[{"type":"text","text":"first line"},{"type":"text","text":"second line"}]`
	got := parseContent(json.RawMessage(input))
	want := "first line\nsecond line"
	if got != want {
		t.Errorf("parseContent(array) = %q, want %q", got, want)
	}
}

func TestParseContent_NonTextBlocksFiltered(t *testing.T) {
	// Case 31: Non-text blocks filtered out (e.g. image blocks).
	input := `[{"type":"image","source":{"type":"base64"}},{"type":"text","text":"visible"}]`
	got := parseContent(json.RawMessage(input))
	if got != "visible" {
		t.Errorf("parseContent(mixed) = %q, want 'visible'", got)
	}
}

func TestParseContent_EmptyInput(t *testing.T) {
	// Case 32: Empty input returns empty string.
	got := parseContent(json.RawMessage(""))
	if got != "" {
		t.Errorf("parseContent(empty) = %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// SSE formatting / NewMessageID (cases 33-34)
// ---------------------------------------------------------------------------

func TestNewMessageID_Format(t *testing.T) {
	// Case 33: NewMessageID has "msg_" prefix and proper length.
	id := NewMessageID()
	if !strings.HasPrefix(id, "msg_") {
		t.Errorf("NewMessageID() = %q, want msg_ prefix", id)
	}
	// newID() produces 24 hex chars (12 bytes), so msg_ + 24 = 28 chars total.
	expectedLen := len("msg_") + 24
	if len(id) != expectedLen {
		t.Errorf("len(NewMessageID()) = %d, want %d", len(id), expectedLen)
	}

	// Verify uniqueness: generate several IDs and confirm they differ.
	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		ids[NewMessageID()] = true
	}
	if len(ids) != 20 {
		t.Errorf("generated %d unique IDs from 20 attempts", len(ids))
	}
}

func TestFormatSSE_Output(t *testing.T) {
	// Case 34: FormatSSE produces correct event format.
	var buf bytes.Buffer
	FormatSSE(&buf, "message_start", map[string]string{"type": "message_start"})

	output := buf.String()

	// Must contain "event: message_start\n"
	if !strings.Contains(output, "event: message_start\n") {
		t.Errorf("FormatSSE output missing 'event: message_start\\n': %q", output)
	}
	// Must contain "data: " followed by JSON.
	if !strings.Contains(output, "data: ") {
		t.Errorf("FormatSSE output missing 'data: ': %q", output)
	}
	// Must end with double newline.
	if !strings.HasSuffix(output, "\n\n") {
		t.Errorf("FormatSSE output does not end with \\n\\n: %q", output)
	}

	// Extract and validate the JSON payload.
	dataLine := output[strings.Index(output, "data: ")+len("data: "):]
	dataLine = strings.TrimSuffix(dataLine, "\n\n")
	var parsed map[string]string
	if err := json.Unmarshal([]byte(dataLine), &parsed); err != nil {
		t.Fatalf("FormatSSE data is not valid JSON: %v, data: %q", err, dataLine)
	}
	if parsed["type"] != "message_start" {
		t.Errorf("parsed type = %q, want message_start", parsed["type"])
	}
}

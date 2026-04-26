package openai

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

// TranslateRequest converts an OpenAI ChatRequest to JoyCode API body.
func TranslateRequest(req *ChatRequest) map[string]interface{} {
	body := map[string]interface{}{
		"model":  req.Model,
		"stream": req.Stream,
	}
	if len(req.Messages) > 0 {
		var msgs []interface{}
		json.Unmarshal(req.Messages, &msgs)
		body["messages"] = msgs
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.Tools) > 0 {
		var tools []interface{}
		json.Unmarshal(req.Tools, &tools)
		body["tools"] = tools
	}
	if len(req.ToolChoice) > 0 {
		body["tool_choice"] = json.RawMessage(req.ToolChoice)
	}
	if len(req.Stop) > 0 {
		body["stop"] = json.RawMessage(req.Stop)
	}
	if len(req.Thinking) > 0 && ReasoningModels[req.Model] {
		body["thinking"] = json.RawMessage(req.Thinking)
	}
	return body
}

// TranslateResponse converts a JoyCode API response to OpenAI format.
func TranslateResponse(jcResp map[string]interface{}, model string) map[string]interface{} {
	return map[string]interface{}{
		"id":                 fmt.Sprintf("chatcmpl-%s", newShortID()),
		"object":             "chat.completion",
		"created":            time.Now().Unix(),
		"model":              model,
		"choices":            jcResp["choices"],
		"usage":              jcResp["usage"],
		"system_fingerprint": fmt.Sprintf("fp_%s", newShortID()),
	}
}

// TranslateModels converts JoyCode models to OpenAI /v1/models format.
func TranslateModels(jcModels []joycode.ModelInfo) map[string]interface{} {
	data := make([]map[string]interface{}, 0, len(jcModels))
	for _, m := range jcModels {
		mid := m.ModelID
		if mid == "" {
			mid = m.Label
		}
		entry := map[string]interface{}{
			"id": mid, "object": "model",
			"created": 1700000000, "owned_by": "joycode",
		}
		if caps, ok := ModelCapabilities[mid]; ok {
			entry["capabilities"] = caps
		}
		data = append(data, entry)
	}
	return map[string]interface{}{"object": "list", "data": data}
}

// TranslateStreamChunk converts a JoyCode SSE data line to OpenAI format.
func TranslateStreamChunk(data string, model string) string {
	if data == "[DONE]" {
		return "data: [DONE]\n\n"
	}
	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return fmt.Sprintf("data: %s\n\n", data)
	}
	if _, ok := chunk["id"]; !ok {
		chunk["id"] = fmt.Sprintf("chatcmpl-%s", newShortID())
	}
	chunk["model"] = model
	chunk["object"] = "chat.completion.chunk"
	b, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", b)
}

func newShortID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%1e12)
}

// DefaultModel returns the model name, defaulting to JoyAI-Code.
func DefaultModel(model string) string {
	if model == "" {
		return joycode.DefaultModel
	}
	return model
}

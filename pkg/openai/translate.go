package openai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

// TranslateRequest converts an OpenAI ChatRequest to JoyCode API body.
func TranslateRequest(req *ChatRequest) map[string]interface{} {
	if joycode.ModelAdapter(req.Model) == "openai-response" {
		body := map[string]interface{}{
			"model":  joycode.ChatAPIModel(req.Model),
			"stream": req.Stream,
		}
		if len(req.Messages) > 0 {
			var msgs []interface{}
			json.Unmarshal(req.Messages, &msgs)
			body["input"] = msgs
		}
		if req.MaxTokens > 0 {
			body["max_output_tokens"] = req.MaxTokens
		}
		if req.Temperature != nil {
			body["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			body["top_p"] = *req.TopP
		}
		if len(req.Tools) > 0 {
			var tools []map[string]interface{}
			if json.Unmarshal(req.Tools, &tools) == nil {
				body["tools"] = convertOpenAIToolsToResponses(tools)
			}
		}
		return body
	}

	body := map[string]interface{}{
		"model":  joycode.ChatAPIModel(req.Model),
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

func convertOpenAIToolsToResponses(tools []map[string]interface{}) []interface{} {
	result := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		if t["type"] != "function" {
			result = append(result, t)
			continue
		}
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			result = append(result, t)
			continue
		}
		result = append(result, map[string]interface{}{
			"type":        "function",
			"name":        fn["name"],
			"description": fn["description"],
			"parameters":  fn["parameters"],
		})
	}
	return result
}

// TranslateResponse converts a JoyCode API response to OpenAI format.
func TranslateResponse(jcResp map[string]interface{}, model string) map[string]interface{} {
	jcResp = unwrapJoyCodeResult(jcResp)
	choices := jcResp["choices"]
	if choices == nil {
		choices = []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": extractResponsesText(jcResp),
				},
				"finish_reason": "stop",
			},
		}
	}
	return map[string]interface{}{
		"id":                 fmt.Sprintf("chatcmpl-%s", newShortID()),
		"object":             "chat.completion",
		"created":            time.Now().Unix(),
		"model":              model,
		"choices":            choices,
		"usage":              jcResp["usage"],
		"system_fingerprint": fmt.Sprintf("fp_%s", newShortID()),
	}
}

func extractResponsesText(resp map[string]interface{}) string {
	resp = unwrapJoyCodeResult(resp)
	if text, ok := resp["output_text"].(string); ok && text != "" {
		return text
	}
	output, _ := resp["output"].([]interface{})
	parts := make([]string, 0)
	for _, item := range output {
		itemMap, _ := item.(map[string]interface{})
		content, _ := itemMap["content"].([]interface{})
		for _, c := range content {
			cMap, _ := c.(map[string]interface{})
			if text, ok := cMap["text"].(string); ok && text != "" {
				parts = append(parts, text)
				continue
			}
			if text, ok := cMap["content"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func unwrapJoyCodeResult(resp map[string]interface{}) map[string]interface{} {
	if result, ok := resp["result"].(map[string]interface{}); ok && result != nil {
		return result
	}
	return resp
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

// ResolveModel returns the model to use for the request.
// If the client-specified model is a known JoyCode model, pass it through.
// Otherwise fall back to the account's default model, then the global default.
func ResolveModel(model string, accountDefault string, systemDefault string) string {
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

package anthropic

import "encoding/json"

// --- Request types (server-side deserialization) ---

// MessageRequest is the Anthropic Messages API request body.
type MessageRequest struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	Messages      []MessageParam  `json:"messages"`
	Stream        bool            `json:"stream,omitempty"`
	System        json.RawMessage `json:"system,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []Tool          `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig `json:"thinking,omitempty"`
}

// MessageParam is a single message in the conversation.
type MessageParam struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Tool defines a tool the model may use.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ThinkingConfig enables extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// --- Response types (server-side serialization) ---

// MessageResponse is the Anthropic Messages API response.
type MessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

// ContentBlock is a single content block in the response.
type ContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

// Usage reports token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- SSE streaming event types ---

type sseMessageStart struct {
	Type    string          `json:"type"`
	Message MessageResponse `json:"message"`
}

type sseContentBlockStart struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

type sseContentBlockDelta struct {
	Type  string    `json:"type"`
	Index int       `json:"index"`
	Delta deltaText `json:"delta"`
}

type deltaText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type sseContentBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type sseMessageDelta struct {
	Type  string    `json:"type"`
	Delta deltaStop `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type deltaStop struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type ssePing struct {
	Type string `json:"type"`
}

type sseMessageStop struct {
	Type string `json:"type"`
}

// --- Model mapping: Claude model → JoyCode model ---

// ModelMapping maps Anthropic model names to JoyCode model IDs.
var ModelMapping = map[string]string{
	"claude-sonnet-4-20250514":   "JoyAI-Code",
	"claude-sonnet-4":            "JoyAI-Code",
	"claude-opus-4-20250514":     "JoyAI-Code",
	"claude-opus-4":              "JoyAI-Code",
	"claude-haiku-4-5-20251001":  "GLM-4.7",
	"claude-haiku-4-5":           "GLM-4.7",
	"claude-3-5-sonnet-latest":   "JoyAI-Code",
	"claude-3-5-sonnet-20241022": "JoyAI-Code",
	"claude-3-5-haiku-latest":    "GLM-4.7",
	"claude-3-5-haiku-20241022":  "GLM-4.7",
	"claude-3-haiku-20240307":    "GLM-4.7",
}

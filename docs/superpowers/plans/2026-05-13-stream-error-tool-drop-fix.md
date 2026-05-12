# Bug Fix: 流式超时丢 tool 参数 + content_filter 未处理 + 截断破坏 tool 消息对

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复被代理的 Claude Code 频繁出现 "Error writing file" 的三个根因：(1) 流式超时错误路径漏发 tool 参数 (2) content_filter finish_reason 未映射 (3) 截断破坏 tool_use/tool_result 消息对完整性。

**Architecture:** 三处独立修复，不互相依赖。Bug 1 和 Bug 2 改 handler.go 的 SSE 流式输出逻辑（正常完成路径 vs 超时错误路径），Bug 3 改 truncate.go 的消息裁剪策略（从简单按索引切割改为按消息对边界切割）。测试覆盖 Go 标准 testing 包。

**Tech Stack:** Go 1.25, net/http SSE streaming, testing (标准库)

**Risks:**
- Task 1 修改 scanner error 分支，需要确保不影响正常完成的流式响应 → 缓解：改动仅限于 `if err := scanner.Err()` 分支内部
- Task 3 修改截断策略，可能导致 token 估算偏高、截断不够激进 → 缓解：按对保留后仍然检查 token 阈值，必要时多轮截断

---

### Task 1: 修复流式超时错误路径 — 补发 input_json_delta + 修复 stopReason

**Root Cause:** `handler.go:435-441` 超时错误处理只发 `content_block_stop`，漏发 `input_json_delta`。Claude Code 收到 Write tool_use 但 input 为空 `{}` → "Error writing file"。同时 line 444 硬编码 `end_turn`，应使用 `tool_use` 当存在已开始的 tool 块时。

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/handler.go:435-450`

- [ ] **Step 1: 修改超时错误处理 — 补发累积的 tool 参数 + 正确设置 stopReason**
文件: `pkg/anthropic/handler.go:435-450`（`for i := 0; i < len(toolCalls); i++` 循环及其后的 message_delta 发送）

```go
			for i := 0; i < len(toolCalls); i++ {
				if toolBlockStarted[i] {
					// Send accumulated tool arguments before closing
					args := toolCalls[i].Arguments
					if args == "" {
						args = "{}"
					}
					if !json.Valid([]byte(args)) {
						// Attempt to close incomplete JSON
						args = fixPartialJSON(args)
					}
					FormatSSE(w, "content_block_delta", sseContentBlockDelta{
						Type:  "content_block_delta",
						Index: toolBlockToIdx[i],
						Delta: deltaText{Type: "input_json_delta", PartialJSON: args},
					})
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: toolBlockToIdx[i],
					})
				}
			}
			// Use tool_use stop reason when tool blocks were active
			errorStopReason := "end_turn"
			if len(toolBlockStarted) > 0 {
				errorStopReason = "tool_use"
			}
			FormatSSE(w, "message_delta", sseMessageDelta{
				Type:  "message_delta",
				Delta: deltaStop{StopReason: errorStopReason},
				Usage: struct {
					OutputTokens int `json:"output_tokens"`
				}{OutputTokens: totalOutput / 4},
			})
			FormatSSE(w, "message_stop", sseMessageStop{Type: "message_stop"})
			flusher.Flush()
```

- [ ] **Step 2: 添加 fixPartialJSON 辅助函数 — 修复截断的 JSON 参数**
文件: `pkg/anthropic/handler.go`（在 `handleStream` 函数之后、`connectStreamWithRetry` 函数之前，约 line 456 处插入）

```go
// fixPartialJSON attempts to close unclosed JSON objects/arrays in truncated tool arguments.
func fixPartialJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	// Count unclosed braces and brackets
	objDepth := 0
	arrDepth := 0
	inStr := false
	escape := false
	for _, ch := range s {
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inStr {
			escape = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			objDepth++
		case '}':
			objDepth--
		case '[':
			arrDepth++
		case ']':
			arrDepth--
		}
	}
	// Close unclosed string
	if inStr {
		s += "\""
	}
	// Close unclosed arrays and objects (inner first)
	for arrDepth > 0 {
		s += "]"
		arrDepth--
	}
	for objDepth > 0 {
		s += "}"
		objDepth--
	}
	return s
}
```

- [ ] **Step 3: 验证编译通过**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "cannot"

- [ ] **Step 4: 提交**
Run: `git add pkg/anthropic/handler.go && git commit -m "fix(stream): send accumulated tool args on timeout and use correct stop reason"`

---

### Task 2: 添加 content_filter finish_reason 处理

**Root Cause:** `handler.go:396-403` 的 switch 不处理 `content_filter`。上游返回 content_filter 时只有 1 个 chunk 无内容，stop_reason 默认为 `end_turn`，Claude Code 收到空响应不知道发生了什么。

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/handler.go:395-403`

- [ ] **Step 1: 在 stopReason switch 中添加 content_filter case**
文件: `pkg/anthropic/handler.go:395-403`（stopReason 赋值的 switch 语句）

```go
			stopReason := "end_turn"
			switch fr {
			case "tool_calls":
				stopReason = "tool_use"
			case "length":
				stopReason = "max_tokens"
			case "stop":
				stopReason = "end_turn"
			case "content_filter":
				stopReason = "end_turn"
			}
```

- [ ] **Step 2: 验证编译通过**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./...`
Expected:
  - Exit code: 0
  - Output does NOT contain: "Error" or "cannot"

- [ ] **Step 3: 提交**
Run: `git add pkg/anthropic/handler.go && git commit -m "fix(stream): handle content_filter finish reason from upstream"`

---

### Task 3: 改进截断逻辑 — 按 tool_use/tool_result 消息对边界切割

**Root Cause:** `truncate.go:71-123` 简单按索引切割消息列表，不感知 tool_use/tool_result 消息对。截断可能只保留 tool_result 而切掉对应的 tool_use（反之亦然），导致孤儿消息。虽然 `translate.go:261-264` 会剥离孤儿 tool_result，但这意味着丢失了之前写文件的完整上下文，模型不知道该写什么。

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/truncate.go:71-123`
- Create: `pkg/anthropic/truncate_tool_preserve_test.go`

- [ ] **Step 1: 添加 findToolPairBoundary 辅助函数 — 向前扫描找到第一个不在 tool 对中间的切割点**
文件: `pkg/anthropic/truncate.go`（在 `truncateMessages` 函数之前插入，约 line 67 处）

```go
// findToolPairBoundary adjusts cutEnd backward if it would split a tool_use/tool_result pair.
// In Anthropic format: assistant(tool_use) is at odd index, user(tool_result) is at even index.
// If cutEnd lands on the tool_result of a tool_use that would be removed, shift cutEnd back to
// include both messages of the pair, or forward to skip past the pair entirely.
// Since we keep messages[cutEnd:], we must ensure cutEnd doesn't start mid-pair.
func findToolPairBoundary(messages []MessageParam, cutEnd int) int {
	if cutEnd <= 1 || cutEnd >= len(messages) {
		return cutEnd
	}
	// Check if messages[cutEnd-1] is assistant and messages[cutEnd] is user
	// which could indicate a tool_use -> tool_result pair split
	prevRole := messages[cutEnd-1].Role
	curRole := messages[cutEnd].Role

	// If we're splitting between assistant and user, check if assistant has tool_use
	if prevRole == "assistant" && curRole == "user" {
		var blocks []contentBlock
		if json.Unmarshal(messages[cutEnd-1].Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "tool_use" {
					// The assistant at cutEnd-1 has tool_use; the user at cutEnd
					// likely has the tool_result. Include both by moving cutEnd back.
					if cutEnd-1 > 1 {
						return cutEnd - 1
					}
					// Can't go further back, skip forward past the pair
					if cutEnd+2 < len(messages) {
						return cutEnd + 2
					}
				}
			}
		}
	}

	// Also check if messages[cutEnd] starts with a tool_result for a removed tool_use
	if curRole == "user" {
		var blocks []contentBlock
		if json.Unmarshal(messages[cutEnd].Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "tool_result" {
					// This user message starts with a tool_result whose tool_use was removed.
					// Skip forward past this tool_result message.
					if cutEnd+1 < len(messages) {
						return cutEnd + 1
					}
				}
			}
		}
	}

	return cutEnd
}
```

- [ ] **Step 2: 修改 truncateMessages — 使用 findToolPairBoundary 调整切割点**
文件: `pkg/anthropic/truncate.go:93-111`（替换从 `// Ensure cutEnd lands on an even index` 到 `truncated = append(truncated, req.Messages[cutEnd:]...)` 的部分）

```go
	// Ensure cutEnd lands on an even index (user) for valid conversation sequence
	if cutEnd%2 != 0 {
		cutEnd++
	}
	if cutEnd >= n {
		return false
	}

	// Adjust cutEnd to avoid splitting tool_use/tool_result pairs
	cutEnd = findToolPairBoundary(req.Messages, cutEnd)
	if cutEnd >= n {
		return false
	}

	removed := cutEnd - keepFirst
	notice := "[System: Earlier conversation messages have been auto-truncated to fit within the model's context window. Some earlier context is now missing. Continue with the remaining conversation.]"
	noticeBytes, _ := json.Marshal(notice)

	var truncated []MessageParam
	truncated = append(truncated, req.Messages[:keepFirst]...)
	truncated = append(truncated, MessageParam{
		Role:    "assistant",
		Content: json.RawMessage(noticeBytes),
	})
	truncated = append(truncated, req.Messages[cutEnd:]...)
```

- [ ] **Step 3: 添加截断 tool 对保留的单元测试**

```go
// pkg/anthropic/truncate_tool_preserve_test.go
package anthropic

import (
	"encoding/json"
	"testing"
)

func TestFindToolPairBoundary_NoToolUse(t *testing.T) {
	msgs := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"hello"`)},
		{Role: "assistant", Content: json.RawMessage(`"hi"`)},
		{Role: "user", Content: json.RawMessage(`"how are you"`)},
		{Role: "assistant", Content: json.RawMessage(`"fine"`)},
	}
	// cutEnd=2 should stay 2 (no tool_use involved)
	got := findToolPairBoundary(msgs, 2)
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestFindToolPairBoundary_SplitsToolPair(t *testing.T) {
	msgs := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"hello"`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"tu_1","name":"Write","input":{"file_path":"/a.go","content":"..."}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tu_1","content":"ok"}]`)},
		{Role: "assistant", Content: json.RawMessage(`"done"`)},
		{Role: "user", Content: json.RawMessage(`"next"`)},
	}
	// cutEnd=2 splits between tool_use (idx 1) and tool_result (idx 2)
	// should move back to 1 to keep the pair together
	got := findToolPairBoundary(msgs, 2)
	if got != 1 {
		t.Errorf("expected 1 (include tool_use with its tool_result), got %d", got)
	}
}

func TestFindToolPairBoundary_OrphanedToolResult(t *testing.T) {
	msgs := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"hello"`)},
		{Role: "assistant", Content: json.RawMessage(`"some text"`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tu_1","content":"ok"}]`)},
		{Role: "assistant", Content: json.RawMessage(`"done"`)},
	}
	// cutEnd=2 starts with an orphaned tool_result (no tool_use before it)
	// should skip forward to 3
	got := findToolPairBoundary(msgs, 2)
	if got != 3 {
		t.Errorf("expected 3 (skip orphaned tool_result), got %d", got)
	}
}

func TestTruncateMessages_PreservesToolPairs(t *testing.T) {
	msgs := []MessageParam{
		{Role: "user", Content: json.RawMessage(`"first user message"`)},
		{Role: "assistant", Content: json.RawMessage(`"response 1"`)},
		{Role: "user", Content: json.RawMessage(`"msg 2"`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"tu_1","name":"Write","input":{"file_path":"/a.go","content":"package main"}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tu_1","content":"File written successfully"}]`)},
		{Role: "assistant", Content: json.RawMessage(`"done"`)},
		{Role: "user", Content: json.RawMessage(`"latest"`)},
	}

	req := &MessageRequest{Messages: msgs}
	result := truncateMessages(req)
	if !result {
		t.Fatal("expected truncation to succeed")
	}

	// After truncation, no tool_result should be orphaned
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
	// Verify all tool_results have matching tool_use
	for _, m := range req.Messages {
		if m.Role == "user" {
			var blocks []contentBlock
			if json.Unmarshal(m.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "tool_result" && b.ToolUseID != "" {
						if !toolUseIDs[b.ToolUseID] {
							t.Errorf("orphaned tool_result found: tool_use_id=%s", b.ToolUseID)
						}
					}
				}
			}
		}
	}
}
```

- [ ] **Step 4: 验证测试通过**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go test ./pkg/anthropic/ -run TestFindToolPair -v`
Expected:
  - Exit code: 0
  - Output contains: "PASS"
  - Output does NOT contain: "FAIL"

- [ ] **Step 5: 验证全部测试通过**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go test ./pkg/anthropic/ -v`
Expected:
  - Exit code: 0
  - Output contains: "PASS"
  - Output does NOT contain: "FAIL"

- [ ] **Step 6: 提交**
Run: `git add pkg/anthropic/truncate.go pkg/anthropic/truncate_tool_preserve_test.go && git commit -m "fix(truncate): preserve tool_use/tool_result pairs during context truncation"`

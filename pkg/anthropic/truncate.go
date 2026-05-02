package anthropic

import (
	"encoding/json"
	"log/slog"
)

const (
	truncationKeepRatio = 0.4
	truncationMinKeep   = 4
)

// truncateMessages removes the oldest messages from the middle of the
// conversation, keeping the first message + a truncation notice + the last
// 40% of messages. Returns true if truncation was performed.
func truncateMessages(req *MessageRequest) bool {
	n := len(req.Messages)
	if n <= truncationMinKeep {
		return false
	}

	keepFirst := 1
	keepLast := truncationMinKeep
	if ratio := int(float64(n) * truncationKeepRatio); ratio > keepLast {
		keepLast = ratio
	}
	cutEnd := n - keepLast
	if cutEnd <= keepFirst {
		return false
	}

	// message[0] is user, so even indices are user, odd are assistant.
	// Ensure cutEnd lands on an even index (user) so the sequence
	// user(0) -> assistant(notice) -> user(cutEnd) is valid.
	if cutEnd%2 != 0 {
		cutEnd++
	}
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

	slog.Warn("auto-truncated messages for context limit",
		"original_count", n,
		"truncated_count", len(truncated),
		"removed", removed,
		"kept_first", keepFirst,
		"kept_last", len(req.Messages)-cutEnd,
	)

	req.Messages = truncated
	return true
}

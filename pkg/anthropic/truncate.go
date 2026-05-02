package anthropic

import (
	"encoding/json"
	"log/slog"
)

const (
	// Upstream model context window size (JoyCode's ModelArts limit)
	contextWindowSize = 196608
	// Safety margin: start truncation when estimated tokens exceed this ratio of contextWindowSize
	preemptiveThresholdRatio = 0.85
	// Rough approximation: 1 token ≈ 3.5 bytes for mixed Chinese/English code content
	bytesPerToken = 3.5
	// Maximum truncation rounds before giving up
	maxTruncationRounds = 5
)

// estimateTokens gives a rough token count estimate for the request messages.
// Uses byte-length / bytesPerToken as approximation. Overestimates slightly which is safe.
func estimateTokens(req *MessageRequest) int {
	totalBytes := 0
	if req.System != nil {
		totalBytes += len(req.System)
	}
	for _, m := range req.Messages {
		totalBytes += len(m.Content)
	}
	if totalBytes == 0 {
		return 0
	}
	return int(float64(totalBytes) / bytesPerToken)
}

// PreemptiveTruncate checks if the request likely exceeds the model's context limit
// and proactively truncates before sending. Returns the number of truncation rounds performed.
// Returns -1 if truncation failed to bring estimated tokens below threshold.
func PreemptiveTruncate(req *MessageRequest) int {
	window := float64(contextWindowSize)
	ratio := float64(preemptiveThresholdRatio)
	threshold := int(window * ratio)

	rounds := 0
	for rounds < maxTruncationRounds {
		estimated := estimateTokens(req)
		if estimated <= threshold {
			if rounds > 0 {
				slog.Info("preemptive truncation complete", "rounds", rounds, "estimated_tokens", estimated, "threshold", threshold)
			}
			return rounds
		}
		if !truncateMessages(req) {
			slog.Warn("preemptive truncation: cannot truncate further", "estimated_tokens", estimated, "threshold", threshold, "rounds", rounds)
			return -1
		}
		rounds++
		slog.Warn("preemptive truncation round", "round", rounds, "estimated_tokens_before", estimated, "threshold", threshold)
	}
	// Check if we actually got below threshold
	if estimateTokens(req) > threshold {
		slog.Warn("preemptive truncation exhausted rounds without reaching threshold")
		return -1
	}
	return rounds
}

// truncateMessages removes the oldest messages from the middle of the
// conversation, keeping the first message + a truncation notice + the last
// portion of messages. Each call removes ~40% of remaining messages.
// Returns true if truncation was performed.
func truncateMessages(req *MessageRequest) bool {
	n := len(req.Messages)
	if n <= 4 {
		return false
	}

	keepFirst := 1
	// Remove 40% of messages each round (more aggressive than before)
	keepLast := int(float64(n) * 0.6)
	if keepLast < 4 {
		keepLast = 4
	}
	cutEnd := n - keepLast
	if cutEnd <= keepFirst {
		// Even more aggressive: only keep first + last 2
		keepLast = 2
		cutEnd = n - keepLast
		if cutEnd <= keepFirst {
			return false
		}
	}

	// Ensure cutEnd lands on an even index (user) for valid conversation sequence
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
		"kept_last", n-cutEnd,
	)

	req.Messages = truncated
	return true
}

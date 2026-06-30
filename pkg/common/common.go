// Package common provides small shared helpers used across protocol, client,
// and CLI packages to avoid duplicate implementations.
package common

import "strings"

// IsTimeoutError reports whether err looks like an HTTP client / context
// timeout. It matches on the error message because the upstream JoyCode API
// and Go's net/http wrap timeouts in several different ways.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "i/o timeout")
}

// Truncate shortens s to at most maxLen bytes, appending "..." if it was
// truncated. Safe for logging previews of long strings.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

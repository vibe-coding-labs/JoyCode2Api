package store

import (
	"context"
	"net/http"
)

type ctxKey struct{}

type modelCtxKey struct{}

type accountModelCtxKey struct{}

type upstreamErrorCtxKey struct{}

type upstreamRequestCtxKey struct{}

type upstreamResponseCtxKey struct{}

// InitTokenUsage initializes token usage tracking in the request context.
// Returns a pointer that handlers can update with SetTokenUsage.
func InitTokenUsage(r *http.Request) *http.Request {
	usage := &[2]int{0, 0}
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, usage))
}

// SetTokenUsage updates the token usage for the request.
func SetTokenUsage(r *http.Request, inputTokens, outputTokens int) {
	if usage, ok := r.Context().Value(ctxKey{}).(*[2]int); ok {
		usage[0] = inputTokens
		usage[1] = outputTokens
	}
}

// GetTokenUsage retrieves the token usage from the request context.
func GetTokenUsage(r *http.Request) (int, int) {
	if usage, ok := r.Context().Value(ctxKey{}).(*[2]int); ok {
		return usage[0], usage[1]
	}
	return 0, 0
}

// SetModel stores the resolved (translated) model name in the request context.
func SetModel(r *http.Request, model string) {
	if m, ok := r.Context().Value(modelCtxKey{}).(*string); ok {
		*m = model
	}
}

// GetModel retrieves the resolved model name from the request context.
func GetModel(r *http.Request) string {
	if m, ok := r.Context().Value(modelCtxKey{}).(*string); ok {
		return *m
	}
	return ""
}

// InitModel initializes model tracking in the request context.
func InitModel(r *http.Request) *http.Request {
	m := new(string)
	return r.WithContext(context.WithValue(r.Context(), modelCtxKey{}, m))
}

// SetAccountDefaultModel stores the account's default model in the request context.
func SetAccountDefaultModel(r *http.Request, defaultModel string) {
	if m, ok := r.Context().Value(accountModelCtxKey{}).(*string); ok {
		*m = defaultModel
	}
}

// GetAccountDefaultModel retrieves the account's default model from the request context.
func GetAccountDefaultModel(r *http.Request) string {
	if m, ok := r.Context().Value(accountModelCtxKey{}).(*string); ok {
		return *m
	}
	return ""
}

// InitAccountModel initializes account default model tracking in the request context.
func InitAccountModel(r *http.Request) *http.Request {
	m := new(string)
	return r.WithContext(context.WithValue(r.Context(), accountModelCtxKey{}, m))
}

// SetUpstreamError stores raw upstream error details in the request context.
func SetUpstreamError(r *http.Request, errMsg string) {
	if m, ok := r.Context().Value(upstreamErrorCtxKey{}).(*string); ok {
		*m = errMsg
	}
}

// GetUpstreamError retrieves raw upstream error details from the request context.
func GetUpstreamError(r *http.Request) string {
	if m, ok := r.Context().Value(upstreamErrorCtxKey{}).(*string); ok {
		return *m
	}
	return ""
}

// InitUpstreamError initializes upstream error tracking in the request context.
func InitUpstreamError(r *http.Request) *http.Request {
	m := new(string)
	return r.WithContext(context.WithValue(r.Context(), upstreamErrorCtxKey{}, m))
}

// SetUpstreamRequest stores the final JoyCode request payload for request logs.
func SetUpstreamRequest(r *http.Request, payload string) {
	if m, ok := r.Context().Value(upstreamRequestCtxKey{}).(*string); ok {
		*m = payload
	}
}

// GetUpstreamRequest retrieves the final JoyCode request payload for request logs.
func GetUpstreamRequest(r *http.Request) string {
	if m, ok := r.Context().Value(upstreamRequestCtxKey{}).(*string); ok {
		return *m
	}
	return ""
}

// InitUpstreamRequest initializes upstream request tracking in the request context.
func InitUpstreamRequest(r *http.Request) *http.Request {
	m := new(string)
	return r.WithContext(context.WithValue(r.Context(), upstreamRequestCtxKey{}, m))
}

// SetUpstreamResponse stores raw upstream response details for request logs.
func SetUpstreamResponse(r *http.Request, payload string) {
	if m, ok := r.Context().Value(upstreamResponseCtxKey{}).(*string); ok {
		*m = payload
	}
}

// GetUpstreamResponse retrieves raw upstream response details for request logs.
func GetUpstreamResponse(r *http.Request) string {
	if m, ok := r.Context().Value(upstreamResponseCtxKey{}).(*string); ok {
		return *m
	}
	return ""
}

// InitUpstreamResponse initializes upstream response tracking in the request context.
func InitUpstreamResponse(r *http.Request) *http.Request {
	m := new(string)
	return r.WithContext(context.WithValue(r.Context(), upstreamResponseCtxKey{}, m))
}

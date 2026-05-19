package joycode

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	BaseURL       = "https://joycode-api.jd.com"
	SaasBaseURL   = "http://joycode-api-saas.jd.com"
	DefaultModel  = "JoyAI-Code"
	ClientVersion = "2.4.5"
	UserAgent     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"
)

var Models = []string{
	"JoyAI-Code",
	"Claude-Opus-4.7",
	"MiniMax-M2.7",
	"Kimi-K2.6",
	"Kimi-K2.5",
	"GLM-5.1",
	"GLM-5",
	"GLM-4.7",
	"Doubao-Seed-2.0-pro",
}

type Client struct {
	PtKey          string
	AnthropicPtKey string
	UserID         string
	SessionID      string
	httpClient     *http.Client
}

type gzipReadCloser struct {
	io.Reader
	body io.Closer
	gzip io.Closer
}

func (r *gzipReadCloser) Close() error {
	gzipErr := r.gzip.Close()
	bodyErr := r.body.Close()
	if gzipErr != nil {
		return gzipErr
	}
	return bodyErr
}

func NewClient(ptKey, userID string) *Client {
	return &Client{
		PtKey:      ptKey,
		UserID:     userID,
		SessionID:  newHexID(),
		httpClient: &http.Client{Timeout: 30 * time.Minute},
	}
}

// SetHTTPClient replaces the internal HTTP client. Intended for testing.
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

func (c *Client) SetTimeout(d time.Duration) {
	c.httpClient.Timeout = d
}

func (c *Client) SetTransport(transport http.RoundTripper) {
	c.httpClient.Transport = transport
}

func (c *Client) SetAnthropicPtKey(ptKey string) {
	c.AnthropicPtKey = ptKey
}

func newHexID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) headers() http.Header {
	return http.Header{
		"Content-Type":    {"application/json; charset=UTF-8"},
		"ptKey":           {c.PtKey},
		"loginType":       {"N_PIN_PC"},
		"User-Agent":      {UserAgent},
		"Accept":          {"*/*"},
		"Accept-Encoding": {"gzip, deflate, br"},
		"Accept-Language": {"zh-CN,zh;q=0.9,en;q=0.8"},
		"Connection":      {"keep-alive"},
	}
}

func (c *Client) anthropicHeaders() http.Header {
	ptKey := c.PtKey
	if c.AnthropicPtKey != "" {
		ptKey = c.AnthropicPtKey
	}
	return http.Header{
		"Content-Type":    {"application/json; charset=utf-8"},
		"ptKey":           {ptKey},
		"loginType":       {"PIN_JD_CLOUD"},
		"User-Agent":      {UserAgent},
		"Accept":          {"*/*"},
		"Accept-Encoding": {"gzip, deflate"},
		"Accept-Language": {"zh-CN,zh;q=0.9,en;q=0.8"},
		"Connection":      {"keep-alive"},
	}
}

func (c *Client) prepareBody(extra map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"tenant": "JOYCODE", "userId": c.UserID,
		"client": "JoyCode", "clientVersion": ClientVersion,
		"sessionId": c.SessionID,
	}
	if _, ok := extra["chatId"]; !ok {
		body["chatId"] = newHexID()
	}
	if _, ok := extra["requestId"]; !ok {
		body["requestId"] = newHexID()
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func (c *Client) prepareAnthropicBody(extra map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"tenant":        "JD",
		"userId":        c.UserID,
		"client":        "JoyCode",
		"clientVersion": ClientVersion,
		"language":      "UNKNOWN",
		"stream":        true,
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func (c *Client) doPost(endpoint string, body map[string]interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		slog.Error("marshal request body", "endpoint", endpoint, "error", err)
		return nil, err
	}
	req, err := http.NewRequest("POST", BaseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		slog.Error("create request", "endpoint", endpoint, "error", err)
		return nil, err
	}
	req.Header = c.headers()
	return c.httpClient.Do(req)
}

func (c *Client) doAnthropicPost(endpoint string, body map[string]interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		slog.Error("marshal anthropic request body", "endpoint", endpoint, "error", err)
		return nil, err
	}
	req, err := http.NewRequest("POST", SaasBaseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		slog.Error("create anthropic request", "endpoint", endpoint, "error", err)
		return nil, err
	}
	req.Header = c.anthropicHeaders()
	return c.httpClient.Do(req)
}

func decodeBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var r io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(r)
}

func decodeStreamBody(resp *http.Response) error {
	if resp.Header.Get("Content-Encoding") != "gzip" {
		return nil
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	resp.Body = &gzipReadCloser{Reader: gz, body: resp.Body, gzip: gz}
	resp.Header.Del("Content-Encoding")
	return nil
}

func (c *Client) Post(endpoint string, body map[string]interface{}) (map[string]interface{}, error) {
	resp, err := c.doPost(endpoint, c.prepareBody(body))
	if err != nil {
		slog.Error("upstream request failed", "endpoint", endpoint, "error", err)
		return nil, err
	}
	data, err := decodeBody(resp)
	if err != nil {
		slog.Error("decode upstream response", "endpoint", endpoint, "status", resp.StatusCode, "error", err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		slog.Error("upstream non-200", "endpoint", endpoint, "status", resp.StatusCode, "body", truncate(string(data), 500))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		slog.Error("unmarshal upstream response", "endpoint", endpoint, "error", err)
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return result, nil
}

func (c *Client) PostStream(endpoint string, body map[string]interface{}) (*http.Response, error) {
	resp, err := c.doPost(endpoint, c.prepareBody(body))
	if err != nil {
		slog.Error("upstream stream connect", "endpoint", endpoint, "error", err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		slog.Error("upstream stream non-200", "endpoint", endpoint, "status", resp.StatusCode, "body", truncate(string(data), 500))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	if err := decodeStreamBody(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func (c *Client) PostAnthropicStream(endpoint string, body map[string]interface{}) (*http.Response, error) {
	resp, err := c.doAnthropicPost(endpoint, c.prepareAnthropicBody(body))
	if err != nil {
		slog.Error("upstream anthropic stream connect", "endpoint", endpoint, "error", err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		slog.Error("upstream anthropic stream non-200", "endpoint", endpoint, "status", resp.StatusCode, "body", truncate(string(data), 500))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	if err := decodeStreamBody(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListModels() ([]ModelInfo, error) {
	resp, err := c.Post("/api/saas/models/v1/modelList", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	data, ok := resp["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected models response format: missing data array")
	}
	models := make([]ModelInfo, 0, len(data))
	for _, item := range data {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var m ModelInfo
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		models = append(models, m)
	}
	return models, nil
}

func (c *Client) WebSearch(query string) ([]interface{}, error) {
	body := map[string]interface{}{
		"messages": []map[string]string{{"role": "user", "content": query}},
		"stream": false, "model": "search_pro_jina", "language": "UNKNOWN",
	}
	resp, err := c.Post("/api/saas/openai/v1/web-search", body)
	if err != nil {
		return nil, err
	}
	results, _ := resp["search_result"].([]interface{})
	return results, nil
}

func (c *Client) Rerank(query string, documents []string, topN int) (map[string]interface{}, error) {
	return c.Post("/api/saas/openai/v1/rerank", map[string]interface{}{
		"model": "Qwen3-Reranker-8B", "query": query,
		"documents": documents, "top_n": topN,
	})
}

func (c *Client) UserInfo() (map[string]interface{}, error) {
	return c.Post("/api/saas/user/v1/userInfo", map[string]interface{}{})
}

func (c *Client) Validate() error {
	resp, err := c.UserInfo()
	if err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}
	code, ok := resp["code"].(float64)
	if !ok || code != 0 {
		msg, _ := resp["msg"].(string)
		if msg == "" {
			msg = "unknown error"
		}
		return fmt.Errorf("credential validation failed (code=%.0f): %s", code, msg)
	}
	return nil
}

// UserInfoWithRefresh calls the UserInfo API and returns the refreshed ptKey
// from the response data, if present. Returns (refreshedPtKey, nil) on success.
func (c *Client) UserInfoWithRefresh() (string, error) {
	resp, err := c.UserInfo()
	if err != nil {
		return "", fmt.Errorf("user info request failed: %w", err)
	}
	code, ok := resp["code"].(float64)
	if !ok || code != 0 {
		msg, _ := resp["msg"].(string)
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("user info failed (code=%.0f): %s", code, msg)
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return "", nil
	}
	if ptKey, ok := data["ptKey"].(string); ok && ptKey != "" {
		return ptKey, nil
	}
	return "", nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

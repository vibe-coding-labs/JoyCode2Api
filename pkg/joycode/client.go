package joycode

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	BaseURL       = "https://joycode-api.jd.com"
	DefaultModel  = "JoyAI-Code"
	ClientVersion = "2.4.5"
	UserAgent     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"
)

// Client is the JoyCode API HTTP client.
type Client struct {
	PtKey      string
	UserID     string
	SessionID  string
	httpClient *http.Client
}

// NewClient creates a JoyCode API client with the given credentials.
func NewClient(ptKey, userID string) *Client {
	return &Client{
		PtKey:      ptKey,
		UserID:     userID,
		SessionID:  newHexID(),
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
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

func (c *Client) doPost(endpoint string, body map[string]interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", BaseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header = c.headers()
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

// Post sends a POST request and returns parsed JSON.
func (c *Client) Post(endpoint string, body map[string]interface{}) (map[string]interface{}, error) {
	resp, err := c.doPost(endpoint, c.prepareBody(body))
	if err != nil {
		return nil, err
	}
	data, err := decodeBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// PostStream sends a POST and returns the raw response for SSE streaming.
func (c *Client) PostStream(endpoint string, body map[string]interface{}) (*http.Response, error) {
	resp, err := c.doPost(endpoint, c.prepareBody(body))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}
	return resp, nil
}

// ListModels fetches the available model list.
func (c *Client) ListModels() ([]ModelInfo, error) {
	resp, err := c.Post("/api/saas/models/v1/modelList", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	data, _ := resp["data"].([]interface{})
	models := make([]ModelInfo, 0, len(data))
	for _, item := range data {
		b, _ := json.Marshal(item)
		var m ModelInfo
		json.Unmarshal(b, &m)
		models = append(models, m)
	}
	return models, nil
}

// WebSearch performs a web search.
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

// Rerank reranks documents by relevance to the query.
func (c *Client) Rerank(query string, documents []string, topN int) (map[string]interface{}, error) {
	return c.Post("/api/saas/openai/v1/rerank", map[string]interface{}{
		"model": "Qwen3-Reranker-8B", "query": query,
		"documents": documents, "top_n": topN,
	})
}

// UserInfo returns the current authenticated user info.
func (c *Client) UserInfo() (map[string]interface{}, error) {
	return c.Post("/api/saas/user/v1/userInfo", map[string]interface{}{})
}

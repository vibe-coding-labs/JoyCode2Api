package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/anthropic"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

// Regression for issue #4 (vision sub-bug): the Anthropic /v1/messages path
// dropped image content blocks during translation. They must now be forwarded
// to upstream as OpenAI image_url parts (same format the OpenAI path forwards).

type captureRT struct {
	lastBody []byte
}

func (c *captureRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		c.lastBody, _ = io.ReadAll(r.Body)
	}
	// Minimal valid SSE stream so the handler completes cleanly.
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n"
	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(sse)),
	}, nil
}

func TestAnthropicImageBlockForwardedToUpstream(t *testing.T) {
	rt := &captureRT{}
	client := joycode.NewClient("pt-test", "user-test")
	client.SetHTTPClient(&http.Client{Transport: rt})

	h := anthropic.NewHandler(client, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	reqBody := `{
		"model": "GLM-5.1",
		"stream": true,
		"max_tokens": 64,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is this"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}}
			]}
		]
	}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(httptest.NewRecorder(), req)

	if rt.lastBody == nil {
		t.Fatal("no request reached upstream")
	}

	var upstream map[string]interface{}
	if err := json.Unmarshal(rt.lastBody, &upstream); err != nil {
		t.Fatalf("upstream body not JSON: %v\n%s", err, rt.lastBody)
	}
	msgs, _ := upstream["messages"].([]interface{})
	if len(msgs) == 0 {
		t.Fatalf("no messages in upstream body: %s", rt.lastBody)
	}

	// Find the user message and assert it carries a multimodal image_url part.
	var foundImage, foundText bool
	for _, m := range msgs {
		mm, _ := m.(map[string]interface{})
		if mm["role"] != "user" {
			continue
		}
		parts, ok := mm["content"].([]interface{})
		if !ok {
			t.Fatalf("user content is not a multimodal array: %#v", mm["content"])
		}
		for _, p := range parts {
			pp, _ := p.(map[string]interface{})
			switch pp["type"] {
			case "text":
				foundText = true
			case "image_url":
				iu, _ := pp["image_url"].(map[string]interface{})
				if url, _ := iu["url"].(string); url == "data:image/png;base64,iVBORw0KGgo=" {
					foundImage = true
				} else {
					t.Errorf("unexpected image_url url: %v", iu["url"])
				}
			}
		}
	}
	if !foundImage {
		t.Errorf("image block was not forwarded as image_url; upstream body:\n%s", rt.lastBody)
	}
	if !foundText {
		t.Errorf("text part missing from multimodal content; upstream body:\n%s", rt.lastBody)
	}
}

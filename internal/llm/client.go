// Package llm implements a minimal streaming client for OpenAI-compatible
// chat completion endpoints (NVIDIA NIM, local Ollama).
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// defaultIdleTimeout bounds how long StreamChat will wait for the *next*
// chunk once a stream is under way. NVIDIA's free-tier endpoint has been
// observed to emit one token and then go silent for 25+ seconds with no
// [DONE] and no connection close, which would otherwise hang forever.
const defaultIdleTimeout = 45 * time.Second

// Client talks to an OpenAI-compatible chat completions endpoint.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// IdleTimeout bounds the gap between consecutive stream chunks (reset
	// on every line received). Zero means defaultIdleTimeout.
	IdleTimeout time.Duration
}

// NewClient builds a Client that times out if the server never starts
// responding, but never cuts off an in-progress stream on total duration:
// http.Client.Timeout bounds the whole request including body reads, so it
// is deliberately left unset here in favor of a transport-level
// ResponseHeaderTimeout plus StreamChat's own idle-timeout watchdog.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
		IdleTimeout: defaultIdleTimeout,
	}
}

// Message is a single chat message. If ImageDataURL is set, it is marshaled
// as a multimodal content array (text part plus image_url part); otherwise
// Content is sent as a plain string, matching the OpenAI-compatible shapes.
type Message struct {
	Role         string
	Content      string
	ImageDataURL string
}

type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL string `json:"url"`
}

// MarshalJSON implements the text-only vs. multimodal content shapes.
func (m Message) MarshalJSON() ([]byte, error) {
	if m.ImageDataURL == "" {
		return json.Marshal(struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{m.Role, m.Content})
	}
	return json.Marshal(struct {
		Role    string        `json:"role"`
		Content []contentPart `json:"content"`
	}{
		Role: m.Role,
		Content: []contentPart{
			{Type: "text", Text: m.Content},
			{Type: "image_url", ImageURL: &imageURLPart{URL: m.ImageDataURL}},
		},
	})
}

type chatRequest struct {
	Model    string    `json:"model"`
	Stream   bool      `json:"stream"`
	Messages []Message `json:"messages"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// StreamChat sends messages to the /chat/completions endpoint and calls
// onToken for each piece of assistant text as it arrives.
func (c *Client) StreamChat(model string, messages []Message, onToken func(string)) error {
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Stream:   true,
		Messages: messages,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("backend returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	idleTimeout := c.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}
	var stalled atomic.Bool
	watchdog := time.AfterFunc(idleTimeout, func() {
		stalled.Store(true)
		// Canceling the request context (rather than only closing the
		// body) reliably aborts an in-flight read for both HTTP/1.1 and
		// HTTP/2, where a bare Body.Close() from another goroutine isn't
		// always guaranteed to unblock a pending Read promptly.
		cancel()
	})
	defer watchdog.Stop()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		watchdog.Reset(idleTimeout)
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		if data == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				onToken(choice.Delta.Content)
			}
		}
	}
	if stalled.Load() {
		return fmt.Errorf("stream stalled: no data received for %s", idleTimeout)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stream: %w", err)
	}

	return nil
}

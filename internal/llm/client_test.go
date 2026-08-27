package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMessageMarshalJSONTextOnly(t *testing.T) {
	msg := Message{Role: "user", Content: "hello"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["role"] != "user" {
		t.Errorf("role = %v, want user", got["role"])
	}
	if got["content"] != "hello" {
		t.Errorf("content = %v, want %q (plain string, not an array)", got["content"], "hello")
	}
}

func TestMessageMarshalJSONWithImage(t *testing.T) {
	msg := Message{Role: "user", Content: "what is this?", ImageDataURL: "data:image/jpeg;base64,AAAA"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text,omitempty"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url,omitempty"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal into multimodal shape: %v (raw: %s)", err, data)
	}
	if len(got.Content) != 2 {
		t.Fatalf("len(content) = %d, want 2 (text part + image_url part)", len(got.Content))
	}
	if got.Content[0].Type != "text" || got.Content[0].Text != "what is this?" {
		t.Errorf("content[0] = %+v, want text part", got.Content[0])
	}
	if got.Content[1].Type != "image_url" || got.Content[1].ImageURL.URL != "data:image/jpeg;base64,AAAA" {
		t.Errorf("content[1] = %+v, want image_url part", got.Content[1])
	}
}

func TestMessageMarshalJSONToolMessages(t *testing.T) {
	call := ToolCall{ID: "call_1", Type: "function", Function: FunctionCall{Name: "web_search", Arguments: `{"query":"linux"}`}}
	assistant, err := json.Marshal(Message{Role: "assistant", ToolCalls: []ToolCall{call}})
	if err != nil {
		t.Fatalf("Marshal assistant: %v", err)
	}
	if !strings.Contains(string(assistant), `"tool_calls"`) || !strings.Contains(string(assistant), `"call_1"`) {
		t.Errorf("assistant JSON = %s, want tool_calls and call id", assistant)
	}

	toolResult, err := json.Marshal(Message{Role: "tool", ToolCallID: "call_1", Content: `{"results":[]}`})
	if err != nil {
		t.Fatalf("Marshal tool result: %v", err)
	}
	if !strings.Contains(string(toolResult), `"tool_call_id":"call_1"`) {
		t.Errorf("tool result JSON = %s, want tool_call_id", toolResult)
	}
}

func TestStreamChatToolsAggregatesToolCallDeltas(t *testing.T) {
	var request struct {
		Tools []Tool `json:"tools"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_","arguments":"{\"query\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"search","arguments":"\"latest kernel\"}"}}]},"finish_reason":"tool_calls"}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	tools := []Tool{{Type: "function", Function: FunctionDefinition{Name: "web_search", Parameters: map[string]any{"type": "object"}}}}
	response, err := client.StreamChatTools("m", []Message{{Role: "user", Content: "latest?"}}, tools, func(string) {})
	if err != nil {
		t.Fatalf("StreamChatTools: %v", err)
	}
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "web_search" {
		t.Errorf("request tools = %+v, want web_search", request.Tools)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v, want one", response.ToolCalls)
	}
	call := response.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "web_search" || call.Function.Arguments != `{"query":"latest kernel"}` {
		t.Errorf("tool call = %+v", call)
	}
	if response.FinishReason != "tool_calls" {
		t.Errorf("finish reason = %q, want tool_calls", response.FinishReason)
	}
}

func TestStreamChatToolsSendsExplicitToolChoice(t *testing.T) {
	var request struct {
		Tools      []Tool `json:"tools"`
		ToolChoice string `json:"tool_choice"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"final\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	tools := []Tool{{Type: "function", Function: FunctionDefinition{Name: "web_search"}}}
	response, err := client.StreamChatToolsWithChoice("m", nil, tools, "none", func(string) {})
	if err != nil {
		t.Fatalf("StreamChatToolsWithChoice: %v", err)
	}
	if request.ToolChoice != "none" || len(request.Tools) != 1 {
		t.Errorf("tool_choice = %q, tools = %d", request.ToolChoice, len(request.Tools))
	}
	if response.Content != "final" {
		t.Errorf("content = %q", response.Content)
	}
}

func TestStreamChatDeliversTokensAndSetsAuthHeader(t *testing.T) {
	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		chunks := []string{"Hello", ", ", "world", "!"}
		for _, c := range chunks {
			payload, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"delta": map[string]string{"content": c}},
				},
			})
			w.Write([]byte("data: " + string(payload) + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")

	var got strings.Builder
	err := client.StreamChat("some-model", []Message{{Role: "user", Content: "hi"}}, func(tok string) {
		got.WriteString(tok)
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if got.String() != "Hello, world!" {
		t.Errorf("streamed text = %q, want %q", got.String(), "Hello, world!")
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want %q", gotPath, "/chat/completions")
	}
}

func TestStreamChatOmitsAuthHeaderWhenKeyEmpty(t *testing.T) {
	var gotAuth string
	sawAuth := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, sawAuth = r.Header.Get("Authorization"), r.Header.Get("Authorization") != ""
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	if err := client.StreamChat("m", []Message{{Role: "user", Content: "hi"}}, func(string) {}); err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if sawAuth {
		t.Errorf("Authorization header = %q, want none when APIKey is empty", gotAuth)
	}
}

func TestStreamChatReturnsErrorOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid api key"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-key")
	err := client.StreamChat("m", []Message{{Role: "user", Content: "hi"}}, func(string) {})
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want it to mention the 401 status", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %v, want it to include the response body", err)
	}
}

func TestStreamChatIdleTimeoutOnStall(t *testing.T) {
	// Reproduces an observed real-world failure: NVIDIA's free-tier
	// endpoint sending one token, then going silent forever with no
	// [DONE] and no connection close. Without a bound, StreamChat would
	// hang indefinitely.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		payload, _ := json.Marshal(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": "To"}},
			},
		})
		w.Write([]byte("data: " + string(payload) + "\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // go silent until the client disconnects
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	client.IdleTimeout = 100 * time.Millisecond

	var got strings.Builder
	start := time.Now()
	err := client.StreamChat("m", []Message{{Role: "user", Content: "hi"}}, func(tok string) {
		got.WriteString(tok)
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a stall error, got nil")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Errorf("error = %v, want it to mention the stream stalled", err)
	}
	if got.String() != "To" {
		t.Errorf("streamed text = %q, want %q (the token received before the stall)", got.String(), "To")
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %s, want it bounded by the short IdleTimeout instead of hanging", elapsed)
	}
}

func TestStreamChatIgnoresMalformedChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: not-json\n\n"))
		w.Write([]byte(": a comment line, not data\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	var got strings.Builder
	err := client.StreamChat("m", nil, func(tok string) { got.WriteString(tok) })
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if got.String() != "ok" {
		t.Errorf("streamed text = %q, want %q (malformed/comment lines skipped)", got.String(), "ok")
	}
}

// A stream that closes without content or tool calls must not look like a
// successful empty answer, which would exit silently.
func TestStreamChatReportsEmptyResponse(t *testing.T) {
	cases := map[string]string{
		"immediate done": "data: [DONE]\n\n",
		"empty delta":    "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		"closed early":   "",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(body))
			}))
			defer server.Close()

			client := NewClient(server.URL, "key")
			err := client.StreamChat("m", nil, func(string) {})
			if err == nil {
				t.Fatal("expected an error for a stream with no content")
			}
			if !strings.Contains(err.Error(), "empty response") {
				t.Errorf("error = %v, want it to mention an empty response", err)
			}
		})
	}
}

func TestStreamChatToolsAllowsEmptyContentWithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	response, err := client.StreamChatTools("m", nil, nil, func(string) {})
	if err != nil {
		t.Fatalf("StreamChatTools: %v", err)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Function.Name != "web_search" {
		t.Errorf("tool calls = %+v, want one web_search call", response.ToolCalls)
	}
}

package webagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linuxai/internal/llm"
	"linuxai/internal/webread"
)

type fakeClient struct {
	responses []llm.Response
	calls     int
	messages  [][]llm.Message
	tools     [][]llm.Tool
	choices   []string
}

type fakePageReader struct {
	page  webread.Page
	pages []webread.Page
	err   error
	urls  []string
}

func (f *fakePageReader) Read(_ context.Context, rawURL string) (webread.Page, error) {
	f.urls = append(f.urls, rawURL)
	if len(f.pages) >= len(f.urls) {
		return f.pages[len(f.urls)-1], f.err
	}
	return f.page, f.err
}

func (f *fakeClient) StreamChatTools(_ string, messages []llm.Message, tools []llm.Tool, onToken func(string)) (llm.Response, error) {
	return f.stream(messages, tools, "", onToken)
}

func (f *fakeClient) StreamChatToolsWithChoice(_ string, messages []llm.Message, tools []llm.Tool, toolChoice string, onToken func(string)) (llm.Response, error) {
	return f.stream(messages, tools, toolChoice, onToken)
}

func (f *fakeClient) stream(messages []llm.Message, tools []llm.Tool, toolChoice string, onToken func(string)) (llm.Response, error) {
	f.messages = append(f.messages, append([]llm.Message(nil), messages...))
	f.tools = append(f.tools, append([]llm.Tool(nil), tools...))
	f.choices = append(f.choices, toolChoice)
	response := f.responses[f.calls]
	f.calls++
	if response.Content != "" {
		onToken(response.Content)
	}
	return response, nil
}

func TestRunnerExecutesSearchAndReturnsFinalAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "latest kernel" {
			t.Errorf("query = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{
			"title": "Kernel Archives", "url": "https://kernel.org", "content": "Current releases",
		}}})
	}))
	defer server.Close()

	client := &fakeClient{responses: []llm.Response{
		{Content: "I should search and inspect sources first.", ToolCalls: []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "web_search", Arguments: `{"query":"latest kernel"}`}}}, FinishReason: "tool_calls"},
		{Content: "Linux is current.", FinishReason: "stop"},
	}}
	var output, activity bytes.Buffer
	runner := Runner{SearXNGURL: server.URL, Activity: &activity}
	reply, err := runner.Run(client, "model", []llm.Message{{Role: "user", Content: "latest?"}}, func(token string) {
		output.WriteString(token)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "Linux is current." || output.String() != reply {
		t.Errorf("reply = %q, streamed = %q", reply, output.String())
	}
	if client.calls != 2 || len(client.tools[0]) != 2 {
		t.Errorf("calls = %d, tools = %d", client.calls, len(client.tools[0]))
	}
	secondTurn := client.messages[1]
	if len(secondTurn) != 3 || secondTurn[1].Role != "assistant" || secondTurn[2].Role != "tool" {
		t.Fatalf("second turn messages = %+v", secondTurn)
	}
	if !strings.Contains(secondTurn[2].Content, "Kernel Archives") || secondTurn[2].ToolCallID != "call_1" {
		t.Errorf("tool result = %+v", secondTurn[2])
	}
	if !strings.Contains(activity.String(), "Searching: latest kernel") {
		t.Errorf("activity = %q", activity.String())
	}
}

// An empty answer must surface as an error; returning it silently made the
// command exit successfully with no output at all.
func TestRunnerRejectsEmptyAnswerAfterToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{
			"title": "Kernel Archives", "url": "https://kernel.org", "content": "Current releases",
		}}})
	}))
	defer server.Close()

	client := &fakeClient{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "web_search", Arguments: `{"query":"latest kernel"}`}}}, FinishReason: "tool_calls"},
		{Content: "   ", FinishReason: "stop"},
	}}
	var output bytes.Buffer
	runner := Runner{SearXNGURL: server.URL, Activity: io.Discard}
	reply, err := runner.Run(client, "model", []llm.Message{{Role: "user", Content: "latest?"}}, func(token string) {
		output.WriteString(token)
	})
	if err == nil {
		t.Fatalf("expected an error, got reply %q", reply)
	}
	if !strings.Contains(err.Error(), "empty answer") {
		t.Errorf("error = %v, want it to mention an empty answer", err)
	}
}

func TestRunnerAccumulatesTokenUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{
			"title": "Kernel Archives", "url": "https://kernel.org", "content": "Current releases",
		}}})
	}))
	defer server.Close()

	client := &fakeClient{responses: []llm.Response{
		{
			ToolCalls:    []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "web_search", Arguments: `{"query":"latest kernel"}`}}},
			FinishReason: "tool_calls",
			Usage:        llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		},
		{
			Content:      "Linux is current.",
			FinishReason: "stop",
			Usage:        llm.Usage{PromptTokens: 300, CompletionTokens: 40, TotalTokens: 340},
		},
	}}
	runner := Runner{SearXNGURL: server.URL, Activity: io.Discard}
	if _, err := runner.Run(client, "model", []llm.Message{{Role: "user", Content: "latest?"}}, func(string) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := llm.Usage{PromptTokens: 400, CompletionTokens: 60, TotalTokens: 460}
	if runner.Usage != want {
		t.Errorf("Usage = %+v, want %+v", runner.Usage, want)
	}
}

func TestRunnerDoesNotFetchDuplicateReadURL(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "read_1", Function: llm.FunctionCall{Name: "web_read", Arguments: `{"url":"https://example.com/"}`}}}},
		{ToolCalls: []llm.ToolCall{{ID: "read_2", Function: llm.FunctionCall{Name: "web_read", Arguments: `{"url":"https://example.com/"}`}}}},
		{Content: "Final."},
	}}
	reader := &fakePageReader{page: webread.Page{URL: "https://example.com/", Kind: "index"}}
	runner := Runner{Reader: reader, Activity: io.Discard}
	reply, err := runner.Run(client, "model", nil, func(string) {})
	if err != nil || reply != "Final." {
		t.Fatalf("reply = %q, error = %v", reply, err)
	}
	if len(reader.urls) != 1 {
		t.Errorf("reader calls = %v, want one", reader.urls)
	}
	thirdTurn := client.messages[2]
	if !strings.Contains(thirdTurn[len(thirdTurn)-1].Content, "already attempted") {
		t.Errorf("duplicate result = %+v", thirdTurn[len(thirdTurn)-1])
	}
}

func TestConsentSessionApprovalIsRemembered(t *testing.T) {
	var output bytes.Buffer
	consent := NewConsent(strings.NewReader("s\n"), &output)
	if !consent.Authorize("https://example.com", "https://example.com/a") {
		t.Fatal("first authorization denied")
	}
	if !consent.Authorize("https://example.com", "https://example.com/b") {
		t.Fatal("session authorization was not remembered")
	}
	if got := strings.Count(output.String(), "authorization required"); got != 1 {
		t.Errorf("prompt count = %d, want 1", got)
	}
}

func TestRunnerExecutesPageRead(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: "web_read", Arguments: `{"url":"https://kernel.org/"}`}}}},
		{Content: "Source read."},
	}}
	reader := &fakePageReader{page: webread.Page{Title: "Kernel", URL: "https://kernel.org/", Content: "Linux releases"}}
	runner := Runner{Reader: reader, Activity: io.Discard}
	reply, err := runner.Run(client, "model", []llm.Message{{Role: "user", Content: "read it"}}, func(string) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "Source read." || len(reader.urls) != 1 || reader.urls[0] != "https://kernel.org/" {
		t.Errorf("reply = %q, read URLs = %q", reply, reader.urls)
	}
	toolResult := client.messages[1][2]
	if toolResult.Role != "tool" || toolResult.ToolCallID != "call_2" || !strings.Contains(toolResult.Content, "Linux releases") {
		t.Errorf("tool result = %+v", toolResult)
	}
}

func TestIndexReadResultRequiresFollowingArticleLink(t *testing.T) {
	reader := &fakePageReader{page: webread.Page{
		URL:   "https://example.com/",
		Kind:  "index",
		Links: []webread.Link{{Text: "Full article", URL: "https://example.com/article"}},
	}}
	runner := Runner{Reader: reader, Activity: io.Discard}
	result := runner.read(`{"url":"https://example.com/"}`)
	for _, want := range []string{"not a full article", "https://example.com/article", "call web_read again"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q: %s", want, result)
		}
	}
}

func TestRunnerReadsFullArticleAfterSearchLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()

	responses := make([]llm.Response, maxSearchCalls+3)
	for index := 0; index < maxSearchCalls; index++ {
		responses[index] = llm.Response{ToolCalls: []llm.ToolCall{{
			ID: fmt.Sprintf("call_%d", index), Function: llm.FunctionCall{Name: "web_search", Arguments: `{"query":"again"}`},
		}}}
	}
	responses[maxSearchCalls] = llm.Response{ToolCalls: []llm.ToolCall{{
		ID: "read_1", Function: llm.FunctionCall{Name: "web_read", Arguments: `{"url":"https://example.com/"}`},
	}}}
	responses[maxSearchCalls+1] = llm.Response{ToolCalls: []llm.ToolCall{{
		ID: "read_2", Function: llm.FunctionCall{Name: "web_read", Arguments: `{"url":"https://example.com/article"}`},
	}}}
	responses[len(responses)-1] = llm.Response{Content: "Final answer from existing sources."}
	client := &fakeClient{responses: responses}
	reader := &fakePageReader{pages: []webread.Page{
		{URL: "https://example.com/", Kind: "index", Links: []webread.Link{{Text: "Article", URL: "https://example.com/article"}}, Content: "Article listing"},
		{URL: "https://example.com/article", Kind: "article", Content: "Full article text"},
	}}
	runner := Runner{SearXNGURL: server.URL, Reader: reader, Activity: io.Discard}
	reply, err := runner.Run(client, "model", []llm.Message{{Role: "user", Content: "Find and summarize the article"}}, func(string) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "Final answer from existing sources." {
		t.Errorf("reply = %q", reply)
	}
	if requests != maxSearchCalls {
		t.Errorf("search requests = %d, want %d", requests, maxSearchCalls)
	}
	readCall := maxSearchCalls
	if len(client.tools[readCall]) != 1 || client.tools[readCall][0].Function.Name != "web_read" {
		t.Errorf("post-search tools = %+v, want only web_read", client.tools[readCall])
	}
	if client.choices[readCall] != "required" || client.choices[readCall+1] != "required" {
		t.Errorf("read choices = %q, %q; want required", client.choices[readCall], client.choices[readCall+1])
	}
	readMessages := client.messages[readCall]
	if readMessages[len(readMessages)-1].Role != "system" || !strings.Contains(readMessages[len(readMessages)-1].Content, "call web_read now") {
		t.Errorf("reading instruction = %+v", readMessages[len(readMessages)-1])
	}
	if len(reader.urls) != 2 || reader.urls[0] != "https://example.com/" || reader.urls[1] != "https://example.com/article" {
		t.Errorf("read URLs = %v", reader.urls)
	}
}

func TestRunnerForcesFinalAnswerAfterRoundLimit(t *testing.T) {
	responses := make([]llm.Response, maxToolRounds+1)
	for index := 0; index < maxToolRounds; index++ {
		responses[index] = llm.Response{ToolCalls: []llm.ToolCall{{
			ID: fmt.Sprintf("call_%d", index), Function: llm.FunctionCall{Name: "unknown", Arguments: `{}`},
		}}}
	}
	responses[maxToolRounds] = llm.Response{Content: "Forced synthesis."}
	client := &fakeClient{responses: responses}
	runner := Runner{Activity: io.Discard}
	reply, err := runner.Run(client, "model", nil, func(string) {})
	if err != nil || reply != "Forced synthesis." {
		t.Fatalf("reply = %q, error = %v", reply, err)
	}
	if len(client.tools[maxToolRounds]) != 2 || client.choices[maxToolRounds] != "none" {
		t.Errorf("final synthesis tools = %d, choice = %q", len(client.tools[maxToolRounds]), client.choices[maxToolRounds])
	}
}

func TestRunnerRetriesWhenBackendIgnoresDisabledTools(t *testing.T) {
	responses := make([]llm.Response, maxToolRounds+2)
	for index := 0; index < maxToolRounds; index++ {
		responses[index] = llm.Response{ToolCalls: []llm.ToolCall{{
			ID: fmt.Sprintf("call_%d", index), Function: llm.FunctionCall{Name: "unknown", Arguments: `{}`},
		}}}
	}
	responses[maxToolRounds] = llm.Response{ToolCalls: []llm.ToolCall{{ID: "ignored", Function: llm.FunctionCall{Name: "web_search", Arguments: `{}`}}}}
	responses[maxToolRounds+1] = llm.Response{Content: "Recovered final answer."}

	client := &fakeClient{responses: responses}
	runner := Runner{Activity: io.Discard}
	reply, err := runner.Run(client, "model", nil, func(string) {})
	if err != nil || reply != "Recovered final answer." {
		t.Fatalf("reply = %q, error = %v", reply, err)
	}
	if client.choices[len(client.choices)-2] != "none" || client.choices[len(client.choices)-1] != "none" {
		t.Errorf("synthesis choices = %v", client.choices[len(client.choices)-2:])
	}
	retryMessages := client.messages[len(client.messages)-1]
	foundDisabledResult := false
	for _, message := range retryMessages {
		if message.Role == "tool" && strings.Contains(message.Content, "tools are disabled") {
			foundDisabledResult = true
		}
	}
	if !foundDisabledResult {
		t.Error("retry did not include a disabled-tool result")
	}
}

func TestRunnerBoundsTotalToolContext(t *testing.T) {
	large := strings.Repeat("x", 40000)
	reader := &fakePageReader{page: webread.Page{URL: "https://kernel.org/", Content: large}}
	calls := make([]llm.ToolCall, 3)
	for index := range calls {
		calls[index] = llm.ToolCall{ID: fmt.Sprintf("call_%d", index), Function: llm.FunctionCall{
			Name: "web_read", Arguments: fmt.Sprintf(`{"url":"https://kernel.org/article-%d"}`, index),
		}}
	}
	client := &fakeClient{responses: []llm.Response{{ToolCalls: calls}, {Content: "done"}}}
	runner := Runner{Reader: reader, Activity: io.Discard}
	if _, err := runner.Run(client, "model", nil, func(string) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	toolMessages := client.messages[1]
	if len(toolMessages) != 5 {
		t.Fatalf("message count = %d, want 5", len(toolMessages))
	}
	if !strings.Contains(toolMessages[3].Content, "context limit") {
		t.Errorf("third tool result = %q, want context-limit error", toolMessages[3].Content)
	}
	if toolMessages[4].Role != "system" || !strings.Contains(toolMessages[4].Content, "Answer the original user") {
		t.Errorf("synthesis instruction = %+v", toolMessages[4])
	}
}

func TestToolArgumentErrorsAreSpecific(t *testing.T) {
	runner := Runner{}
	if got := runner.search(`{"query":]`); !strings.Contains(got, "invalid JSON") {
		t.Errorf("search error = %q", got)
	}
	if got := runner.read(`{"url":]`); !strings.Contains(got, "invalid JSON") {
		t.Errorf("read error = %q", got)
	}
}

func TestConsentOnceDoesNotPersistAndNoTTYDenies(t *testing.T) {
	consent := NewConsent(strings.NewReader("o\nd\n"), io.Discard)
	if !consent.Authorize("https://example.com", "https://example.com/a") {
		t.Fatal("once authorization denied")
	}
	if consent.Authorize("https://example.com", "https://example.com/b") {
		t.Fatal("once authorization unexpectedly persisted")
	}

	var output bytes.Buffer
	nonInteractive := NewConsent(nil, &output)
	if nonInteractive.Authorize("https://example.com", "https://example.com") {
		t.Fatal("non-interactive authorization unexpectedly allowed")
	}
	if !strings.Contains(output.String(), "no interactive terminal") {
		t.Errorf("output = %q", output.String())
	}
}

func TestConsentNilOutputIsSafe(t *testing.T) {
	if NewConsent(nil, nil).Authorize("https://example.com", "https://example.com") {
		t.Fatal("non-interactive nil-output consent unexpectedly allowed")
	}
	if !NewConsent(strings.NewReader("o\n"), nil).Authorize("https://example.com", "https://example.com") {
		t.Fatal("interactive nil-output consent unexpectedly denied")
	}
}

package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraceRequestsUsageAndRecordsIt(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":67,\"completion_tokens\":38,\"total_tokens\":105}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var trace strings.Builder
	client := NewClient(server.URL, "key")
	client.Trace = &trace

	response, err := client.StreamChatTools("m", []Message{{Role: "user", Content: "hi"}}, nil, func(string) {})
	if err != nil {
		t.Fatalf("StreamChatTools: %v", err)
	}
	if response.Usage.PromptTokens != 67 || response.Usage.CompletionTokens != 38 || response.Usage.TotalTokens != 105 {
		t.Errorf("usage = %+v", response.Usage)
	}

	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	options, ok := sent["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage true", sent["stream_options"])
	}
	for _, want := range []string{"llm: request", "llm: response", "105 total"} {
		if !strings.Contains(trace.String(), want) {
			t.Errorf("trace missing %q:\n%s", want, trace.String())
		}
	}
}

// Backends that reject unknown fields must not see stream_options unless the
// caller opted into tracing.
func TestUsageNotRequestedWithoutTrace(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key")
	if err := client.StreamChat("m", nil, func(string) {}); err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if strings.Contains(string(gotBody), "stream_options") {
		t.Errorf("request should omit stream_options without a Trace writer: %s", gotBody)
	}
}

func TestUsageAddAndEmpty(t *testing.T) {
	var total Usage
	if !total.Empty() {
		t.Error("a zero Usage should be empty")
	}
	total.Add(Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	total.Add(Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5})
	if total != (Usage{PromptTokens: 13, CompletionTokens: 7, TotalTokens: 20}) {
		t.Errorf("total = %+v", total)
	}
	if total.Empty() {
		t.Error("an accumulated Usage should not be empty")
	}
	if got := total.String(); got != "13 prompt + 7 completion = 20 total" {
		t.Errorf("String() = %q", got)
	}
}

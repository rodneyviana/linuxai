package searxng

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchReturnsTopResults(t *testing.T) {
	var gotQuery, gotFormat string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotFormat = r.URL.Query().Get("format")

		results := make([]Result, 8)
		for i := range results {
			results[i] = Result{Title: "title", URL: "https://example.com", Content: "content"}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
	}))
	defer server.Close()

	results, err := Search(server.URL, "how do I use grep")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != maxResults {
		t.Errorf("len(results) = %d, want %d (capped)", len(results), maxResults)
	}
	if gotQuery != "how do I use grep" {
		t.Errorf("query = %q, want %q", gotQuery, "how do I use grep")
	}
	if gotFormat != "json" {
		t.Errorf("format = %q, want %q", gotFormat, "json")
	}
}

func TestSearchEmptyBaseURL(t *testing.T) {
	if _, err := Search("", "query"); err == nil {
		t.Error("expected an error when baseURL is empty")
	}
}

func TestSearchNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := Search(server.URL, "query")
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention the 500 status", err)
	}
}

func TestSearchNonJSONContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>not json, format=json was ignored</html>"))
	}))
	defer server.Close()

	_, err := Search(server.URL, "query")
	if err == nil {
		t.Fatal("expected an error when SearXNG doesn't return JSON")
	}
	if !strings.Contains(err.Error(), "search.formats") {
		t.Errorf("error = %v, want a hint about enabling json in search.formats", err)
	}
}

func TestGroundingBlockFormatsResults(t *testing.T) {
	results := []Result{
		{Title: "Kernel Archives", URL: "https://kernel.org", Content: "Latest releases"},
	}
	block := GroundingBlock(results, "what is the latest kernel")

	for _, want := range []string{"what is the latest kernel", "Kernel Archives", "https://kernel.org", "Latest releases"} {
		if !strings.Contains(block, want) {
			t.Errorf("grounding block missing %q\ngot: %s", want, block)
		}
	}
}

func TestGroundingBlockEmptyResults(t *testing.T) {
	if got := GroundingBlock(nil, "query"); got != "" {
		t.Errorf("GroundingBlock(nil) = %q, want empty string", got)
	}
}

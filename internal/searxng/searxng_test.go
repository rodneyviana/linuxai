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

func TestSearchRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"content":"`))
		w.Write([]byte(strings.Repeat("x", maxResponseBytes)))
		w.Write([]byte(`"}]}`))
	}))
	defer server.Close()

	_, err := Search(server.URL, "query")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Search error = %v, want size-limit error", err)
	}
}

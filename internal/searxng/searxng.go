// Package searxng queries a self-hosted SearXNG instance's JSON API and
// formats results as a grounding block for the LLM prompt.
package searxng

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is one search hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type searchResponse struct {
	Results []Result `json:"results"`
}

// maxResults caps how many hits are folded into the grounding block.
const maxResults = 5

// Search queries baseURL's /search endpoint (format=json) for query and
// returns the top results. baseURL must not include a trailing slash.
func Search(baseURL, query string) ([]Result, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("no SearXNG host configured (set LINUXAI_SEARXNG_URL)")
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/search?q=" + url.QueryEscape(query) + "&format=json"

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("querying searxng: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned %s (is search.formats: json enabled in settings.yml?)", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
		return nil, fmt.Errorf("searxng did not return JSON (got %q) - add \"json\" under search.formats in its settings.yml and restart it", ct)
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding searxng response: %w", err)
	}

	if len(parsed.Results) > maxResults {
		parsed.Results = parsed.Results[:maxResults]
	}
	return parsed.Results, nil
}

// GroundingBlock formats search results as a prefix to prepend to the
// user's question, instructing the model to use and cite the sources.
func GroundingBlock(results []Result, query string) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Web search results for \"" + query + "\". Use them to help answer, and cite sources by URL where relevant:\n\n")
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s (%s)\n   %s\n", i+1, r.Title, r.URL, r.Content)
	}
	b.WriteString("\nQuestion: " + query + "\n")
	return b.String()
}

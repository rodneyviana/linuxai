// Package searxng queries a self-hosted SearXNG instance's JSON API.
package searxng

import (
	"encoding/json"
	"fmt"
	"io"
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

// maxResults caps how many hits are returned to the web_search tool.
const (
	maxResults       = 5
	maxResponseBytes = 1024 * 1024
)

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

	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading searxng response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("searxng response exceeds %d byte limit", maxResponseBytes)
	}

	var parsed searchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding searxng response: %w", err)
	}

	if len(parsed.Results) > maxResults {
		parsed.Results = parsed.Results[:maxResults]
	}
	return parsed.Results, nil
}

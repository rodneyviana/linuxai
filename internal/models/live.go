package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// maxListBytes bounds the /models response.
const maxListBytes = 4 << 20

// FetchAvailable lists the model IDs the backend will actually serve, using
// the OpenAI-compatible /models endpoint. Both NVIDIA and Ollama implement it.
func FetchAvailable(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(baseURL), "/") + "/models"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	request.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing models: unexpected status %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxListBytes))
	if err != nil {
		return nil, fmt.Errorf("reading model list: %w", err)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decoding model list: %w", err)
	}

	ids := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		if model.ID != "" {
			ids = append(ids, model.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

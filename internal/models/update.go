package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// ProfilesURL is the upstream capability data, derived by langchain-nvidia
// from the models.dev project.
const ProfilesURL = "https://raw.githubusercontent.com/langchain-ai/langchain-nvidia/main/libs/ai-endpoints/langchain_nvidia_ai_endpoints/data/_profiles.py"

// maxProfilesBytes bounds the download so a hostile or broken response cannot
// exhaust memory.
const maxProfilesBytes = 8 << 20

// allowedProfileHosts restricts the fetch, including redirects, to the hosts
// that legitimately serve the upstream file.
var allowedProfileHosts = map[string]bool{
	"raw.githubusercontent.com":     true,
	"github.com":                    true,
	"objects.githubusercontent.com": true,
}

// UpdateResult reports the outcome of refreshing the catalog.
type UpdateResult struct {
	Changed bool
	Count   int
	Path    string
}

// Update downloads the upstream profiles and writes them to path when they
// differ from what is already in use.
func Update(ctx context.Context, path string) (UpdateResult, error) {
	return UpdateFrom(ctx, ProfilesURL, path)
}

// UpdateFrom is Update against an explicit source URL.
func UpdateFrom(ctx context.Context, rawURL, path string) (UpdateResult, error) {
	catalog, err := Fetch(ctx, rawURL)
	if err != nil {
		return UpdateResult{}, err
	}

	current, _, err := Load(path)
	if err == nil && current.SHA256 == catalog.SHA256 {
		return UpdateResult{Changed: false, Count: len(current.Profiles), Path: path}, nil
	}
	if err := save(path, catalog); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Changed: true, Count: len(catalog.Profiles), Path: path}, nil
}

// Fetch downloads and parses the profile source at rawURL.
func Fetch(ctx context.Context, rawURL string) (Catalog, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Catalog{}, fmt.Errorf("invalid profiles URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return Catalog{}, fmt.Errorf("unsupported profiles URL scheme %q", parsed.Scheme)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Only enforced for the real upstream host, so tests can use httptest.
			if allowedProfileHosts[parsed.Hostname()] && !allowedProfileHosts[req.URL.Hostname()] {
				return fmt.Errorf("refusing redirect to %s", req.URL.Hostname())
			}
			return nil
		},
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Catalog{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Catalog{}, fmt.Errorf("downloading profiles: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("downloading profiles: unexpected status %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxProfilesBytes+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("reading profiles: %w", err)
	}
	if len(body) > maxProfilesBytes {
		return Catalog{}, fmt.Errorf("profiles response exceeds %d bytes", maxProfilesBytes)
	}

	profiles, err := ParseProfiles(body)
	if err != nil {
		return Catalog{}, fmt.Errorf("parsing profiles: %w", err)
	}
	sum := sha256.Sum256(body)
	return Catalog{
		Source:   rawURL,
		Fetched:  time.Now().UTC().Format(time.RFC3339),
		SHA256:   hex.EncodeToString(sum[:]),
		Profiles: profiles,
	}, nil
}

// save writes the catalog to path atomically with private permissions.
func save(path string, catalog Catalog) error {
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding catalog: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, "models-*.json")
	if err != nil {
		return fmt.Errorf("creating temp catalog: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("writing catalog: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("writing catalog: %w", err)
	}
	if err := os.Chmod(tempName, 0o600); err != nil {
		return fmt.Errorf("setting catalog permissions: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("saving catalog: %w", err)
	}
	return nil
}

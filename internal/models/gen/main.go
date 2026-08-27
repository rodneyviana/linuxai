// Command gen regenerates internal/models/profiles.json, the capability
// baseline compiled into the binary. It keeps only chat-capable, non-deprecated
// models so the embedded copy stays small.
//
// Usage: go run ./internal/models/gen [source.py]
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"linuxai/internal/models"
)

const outputPath = "internal/models/profiles.json"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	catalog, err := loadSource()
	if err != nil {
		return err
	}

	kept := map[string]models.Profile{}
	for id, profile := range catalog.Profiles {
		if profile.Chat() {
			kept[id] = profile
		}
	}
	if len(kept) == 0 {
		return fmt.Errorf("no chat-capable profiles found")
	}
	catalog.Profiles = kept

	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s with %d profiles\n", outputPath, len(kept))
	return nil
}

func loadSource() (models.Catalog, error) {
	if len(os.Args) > 1 {
		source, err := os.ReadFile(os.Args[1])
		if err != nil {
			return models.Catalog{}, err
		}
		profiles, err := models.ParseProfiles(source)
		if err != nil {
			return models.Catalog{}, err
		}
		sum := sha256.Sum256(source)
		return models.Catalog{
			Source:   models.ProfilesURL,
			Fetched:  time.Now().UTC().Format(time.RFC3339),
			SHA256:   hex.EncodeToString(sum[:]),
			Profiles: profiles,
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return models.Fetch(ctx, models.ProfilesURL)
}

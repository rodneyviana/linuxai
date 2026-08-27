package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// profilesJSON is the baseline catalog compiled into the binary so the picker
// works with no network and no config file. Regenerate with:
//
//	go run ./internal/models/gen
//
//go:embed profiles.json
var profilesJSON []byte

// Catalog is the on-disk and embedded catalog format.
type Catalog struct {
	Source   string             `json:"source"`
	Fetched  string             `json:"fetched"`
	SHA256   string             `json:"sha256"`
	Profiles map[string]Profile `json:"profiles"`
}

// Embedded returns the catalog compiled into the binary.
func Embedded() (Catalog, error) {
	return decodeCatalog(profilesJSON)
}

// Load reads the catalog from path, falling back to the embedded baseline
// when the file is missing or unreadable. The bool reports whether the
// on-disk copy was used.
func Load(path string) (Catalog, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		catalog, embedErr := Embedded()
		return catalog, false, embedErr
	}
	catalog, err := decodeCatalog(data)
	if err != nil {
		catalog, embedErr := Embedded()
		return catalog, false, embedErr
	}
	return catalog, true, nil
}

func decodeCatalog(data []byte) (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decoding catalog: %w", err)
	}
	if len(catalog.Profiles) == 0 {
		return Catalog{}, fmt.Errorf("catalog contains no profiles")
	}
	return catalog, nil
}

// Merge combines capability profiles with the live model IDs reported by the
// backend. Profiles that cannot be used for chat are dropped; live IDs with
// no profile are kept so a model can still be chosen without capability data.
func Merge(catalog Catalog, live []string) []Entry {
	available := make(map[string]bool, len(live))
	for _, id := range live {
		available[id] = true
	}

	entries := make([]Entry, 0, len(catalog.Profiles)+len(live))
	for id, profile := range catalog.Profiles {
		if !profile.Chat() {
			continue
		}
		entries = append(entries, Entry{
			ID:         id,
			Profile:    profile,
			HasProfile: true,
			Available:  available[id],
		})
		delete(available, id)
	}
	for _, id := range live {
		if available[id] {
			entries = append(entries, Entry{ID: id, Available: true})
			delete(available, id)
		}
	}
	return entries
}

// Filter narrows the entry list shown in the picker.
type Filter struct {
	Query         string
	RequireImages bool
	RequireTools  bool
	AvailableOnly bool
	// IncludeUnprofiled admits models the endpoint lists but the catalog does
	// not describe. They are often not callable, so they are opt-in.
	IncludeUnprofiled bool
}

// Apply returns the entries matching f, preserving input order.
func (f Filter) Apply(entries []Entry) []Entry {
	query := strings.ToLower(strings.TrimSpace(f.Query))
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if !f.IncludeUnprofiled && !entry.HasProfile {
			continue
		}
		if f.AvailableOnly && !entry.Available {
			continue
		}
		if f.RequireImages && !entry.Profile.ImageInputs {
			continue
		}
		if f.RequireTools && !entry.Profile.ToolCalling {
			continue
		}
		if query != "" && !matches(entry, query) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func matches(entry Entry, query string) bool {
	return strings.Contains(strings.ToLower(entry.ID), query) ||
		strings.Contains(strings.ToLower(entry.Profile.Name), query)
}

// Order selects the sort applied to the picker list.
type Order int

const (
	ByName Order = iota
	ByContext
)

func (o Order) String() string {
	if o == ByContext {
		return "context size"
	}
	return "name"
}

// Next cycles through the available sort orders.
func (o Order) Next() Order {
	if o == ByName {
		return ByContext
	}
	return ByName
}

// Sort orders entries in place.
func Sort(entries []Entry, order Order) {
	sort.SliceStable(entries, func(i, j int) bool {
		if order == ByContext {
			a, b := entries[i].Profile.MaxInputTokens, entries[j].Profile.MaxInputTokens
			if a != b {
				return a > b
			}
		}
		return entries[i].ID < entries[j].ID
	})
}

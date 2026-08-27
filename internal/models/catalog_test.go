package models

import (
	"path/filepath"
	"testing"
)

func fixtureCatalog(t *testing.T) Catalog {
	t.Helper()
	profiles, err := ParseProfiles([]byte(fixtureSource))
	if err != nil {
		t.Fatalf("ParseProfiles: %v", err)
	}
	return Catalog{Profiles: profiles}
}

func ids(entries []Entry) map[string]Entry {
	out := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		out[entry.ID] = entry
	}
	return out
}

func TestMergeKeepsChatModelsAndLiveOnlyIDs(t *testing.T) {
	entries := Merge(fixtureCatalog(t), []string{"vendor/chat-model", "vendor/live-only"})
	byID := ids(entries)

	if len(entries) != 2 {
		t.Fatalf("merged %d entries, want 2: %v", len(entries), byID)
	}
	chat, ok := byID["vendor/chat-model"]
	if !ok || !chat.HasProfile || !chat.Available {
		t.Errorf("chat model entry = %+v", chat)
	}
	live, ok := byID["vendor/live-only"]
	if !ok || live.HasProfile || !live.Available {
		t.Errorf("live-only entry = %+v", live)
	}
	if _, present := byID["vendor/embed-model"]; present {
		t.Error("non-chat profiles must be dropped")
	}
}

func TestMergeMarksProfilesMissingFromTheEndpoint(t *testing.T) {
	entries := Merge(fixtureCatalog(t), nil)
	byID := ids(entries)
	if entry := byID["vendor/chat-model"]; entry.Available {
		t.Error("model absent from the live list must not be marked available")
	}
}

func TestFilterApply(t *testing.T) {
	entries := Merge(fixtureCatalog(t), []string{"vendor/chat-model", "vendor/live-only"})

	if got := (Filter{}).Apply(entries); len(got) != 1 || got[0].ID != "vendor/chat-model" {
		t.Errorf("default filter = %v, want only the profiled model", ids(got))
	}
	if got := (Filter{IncludeUnprofiled: true}).Apply(entries); len(got) != 2 {
		t.Errorf("IncludeUnprofiled = %v, want both models", ids(got))
	}
	if got := (Filter{RequireTools: true}).Apply(entries); len(got) != 1 || got[0].ID != "vendor/chat-model" {
		t.Errorf("RequireTools = %v, want only the chat model", ids(got))
	}
	if got := (Filter{RequireImages: true}).Apply(entries); len(got) != 1 || got[0].ID != "vendor/chat-model" {
		t.Errorf("RequireImages = %v, want only the chat model", ids(got))
	}
	if got := (Filter{Query: "LIVE", IncludeUnprofiled: true}).Apply(entries); len(got) != 1 || got[0].ID != "vendor/live-only" {
		t.Errorf("Query = %v, want the live-only model", ids(got))
	}
	if got := (Filter{Query: "LIVE"}).Apply(entries); len(got) != 0 {
		t.Errorf("unlisted models must stay hidden by default, got %v", ids(got))
	}
	if got := (Filter{Query: "chat model"}).Apply(entries); len(got) != 1 {
		t.Errorf("query should match the display name, got %v", ids(got))
	}
	if got := (Filter{AvailableOnly: true}).Apply(Merge(fixtureCatalog(t), nil)); len(got) != 0 {
		t.Errorf("AvailableOnly = %v, want nothing", ids(got))
	}
}

func TestSortOrders(t *testing.T) {
	entries := []Entry{
		{ID: "b", Profile: Profile{MaxInputTokens: 8000}},
		{ID: "a", Profile: Profile{MaxInputTokens: 128000}},
		{ID: "c"},
	}
	Sort(entries, ByName)
	if entries[0].ID != "a" || entries[1].ID != "b" || entries[2].ID != "c" {
		t.Errorf("ByName order = %v", []string{entries[0].ID, entries[1].ID, entries[2].ID})
	}
	Sort(entries, ByContext)
	if entries[0].ID != "a" || entries[1].ID != "b" || entries[2].ID != "c" {
		t.Errorf("ByContext order = %v", []string{entries[0].ID, entries[1].ID, entries[2].ID})
	}
}

func TestOrderNextCycles(t *testing.T) {
	if ByName.Next() != ByContext || ByContext.Next() != ByName {
		t.Error("sort order should cycle between the two options")
	}
}

func TestEmbeddedCatalogOnlyHasUsableModels(t *testing.T) {
	catalog, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	if len(catalog.Profiles) < 10 {
		t.Fatalf("embedded catalog has only %d profiles", len(catalog.Profiles))
	}
	for id, profile := range catalog.Profiles {
		if !profile.Chat() {
			t.Errorf("%s should not be in the embedded catalog: %+v", id, profile)
		}
	}
	if _, present := catalog.Profiles[defaultEmbeddedModel]; !present {
		t.Errorf("embedded catalog is missing %s", defaultEmbeddedModel)
	}
}

func TestLoadFallsBackToEmbedded(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "models.json")
	catalog, fromDisk, err := Load(missing)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fromDisk {
		t.Error("a missing file must not report an on-disk load")
	}
	if len(catalog.Profiles) == 0 {
		t.Error("fallback catalog is empty")
	}
}

// defaultEmbeddedModel is the model linuxai ships with, so it must always be
// selectable in the picker.
const defaultEmbeddedModel = "openai/gpt-oss-20b"

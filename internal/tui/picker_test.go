package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLiveListNarrowsToAvailableModels(t *testing.T) {
	m := newSettingsModel(t)
	opened, _ := m.openPicker()
	m = opened.(model)

	before := len(m.picker.view)
	if before == 0 {
		t.Fatal("catalog should be browsable before the live list arrives")
	}

	updated, _ := m.Update(liveModelsMsg{ids: []string{"openai/gpt-oss-20b"}})
	got := updated.(model)
	if !got.picker.filter.AvailableOnly {
		t.Error("a successful live lookup should narrow the list to available models")
	}
	if len(got.picker.view) != 1 || got.picker.view[0].ID != "openai/gpt-oss-20b" {
		t.Errorf("view = %d entries, want only the live model", len(got.picker.view))
	}

	// A deliberate toggle must survive a later refresh.
	toggled, _ := got.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	got = toggled.(model)
	refreshed, _ := got.Update(liveModelsMsg{ids: []string{"openai/gpt-oss-20b"}})
	if got := refreshed.(model); got.picker.filter.AvailableOnly {
		t.Error("refreshing must not undo an explicit availability toggle")
	}
}

func TestUnlistedModelsAreHiddenUntilRequested(t *testing.T) {
	m := newSettingsModel(t)
	opened, _ := m.openPicker()
	m = opened.(model)

	live := []string{"openai/gpt-oss-20b", "vendor/not-in-catalog"}
	updated, _ := m.Update(liveModelsMsg{ids: live})
	m = updated.(model)

	if m.picker.filter.IncludeUnprofiled {
		t.Error("unlisted models must not be shown by default")
	}
	for _, entry := range m.picker.view {
		if !entry.HasProfile {
			t.Fatalf("%s has no profile and should be hidden", entry.ID)
		}
	}

	toggled, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = toggled.(model)
	if !m.picker.filter.IncludeUnprofiled {
		t.Fatal("Ctrl+N should reveal unlisted models")
	}
	found := false
	for _, entry := range m.picker.view {
		if entry.ID == "vendor/not-in-catalog" {
			found = true
		}
	}
	if !found {
		t.Error("the unlisted model should appear once revealed")
	}
}

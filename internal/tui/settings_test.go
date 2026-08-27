package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"linuxai/internal/config"
	"linuxai/internal/models"
)

func newSettingsModel(t *testing.T) model {
	t.Helper()
	store := newTestStore(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	m, err := newModel(store, "", false, false, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	opened, _ := m.openSettings()
	return opened.(model)
}

func TestSettingsSaveWritesEnvFile(t *testing.T) {
	m := newSettingsModel(t)
	m.settings.fields[fieldAPIKey].SetValue("nvapi-test")
	m.settings.fields[fieldModel].SetValue("vendor/model")
	m.settings.fields[fieldSearXNG].SetValue("http://localhost:8080")

	saved, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	got := saved.(model)
	if !strings.HasPrefix(got.settings.status, "Saved to ") {
		t.Fatalf("status = %q", got.settings.status)
	}

	values, err := config.ReadEnvFile(got.settings.path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if values[config.KeyAPIKey] != "nvapi-test" || values[config.KeyModel] != "vendor/model" {
		t.Errorf("saved values = %v", values)
	}
	if !got.webAvailable {
		t.Error("saving a SearXNG URL should enable the web toggle")
	}
}

func TestSettingsMasksTheAPIKey(t *testing.T) {
	m := newSettingsModel(t)
	m.settings.fields[fieldAPIKey].SetValue("nvapi-secret")
	if rendered := m.settings.fields[fieldAPIKey].View(); strings.Contains(rendered, "nvapi-secret") {
		t.Errorf("API key field rendered in the clear: %q", rendered)
	}
}

func TestSettingsEscapeReturnsToMenu(t *testing.T) {
	m := newSettingsModel(t)
	back, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := back.(model); got.screen != menuScreen {
		t.Errorf("screen = %v, want menu", got.screen)
	}
}

func TestSettingsArrowNavigationReachesTheActions(t *testing.T) {
	m := newSettingsModel(t)
	if !m.settings.fields[fieldAPIKey].Focused() {
		t.Fatal("the first field should start focused")
	}

	var current tea.Model = m
	for step := 0; step < rowSearch; step++ {
		current, _ = current.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	got := current.(model)
	if got.settings.cursor != rowSearch {
		t.Fatalf("cursor = %d, want %d", got.settings.cursor, rowSearch)
	}
	for index := range got.settings.fields {
		if got.settings.fields[index].Focused() {
			t.Errorf("field %d should be blurred while an action is selected", index)
		}
	}

	opened, _ := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := opened.(model); got.screen != modelSearchScreen {
		t.Errorf("Enter on the search action should open the picker, got screen %v", got.screen)
	}

	wrapped, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := wrapped.(model); got.settings.cursor != rowSave {
		t.Errorf("Up from the first row should wrap to Save, got %d", got.settings.cursor)
	}
}

func TestSettingsTypingEditsTheFocusedField(t *testing.T) {
	m := newSettingsModel(t)
	m.settings.cursor = fieldModel
	m.blurSettings()
	m.settings.fields[fieldModel].Focus()
	m.settings.fields[fieldModel].SetValue("")

	var current tea.Model = m
	for _, r := range "abc" {
		current, _ = current.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := current.(model).settings.fields[fieldModel].Value(); got != "abc" {
		t.Errorf("model field = %q, want abc", got)
	}
}

func TestAcceptingAModelCardFillsTheModelField(t *testing.T) {
	m := newSettingsModel(t)
	m.screen = modelCardScreen
	m.picker.card = models.Entry{ID: "vendor/chosen", HasProfile: true}

	accepted, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := accepted.(model)
	if got.screen != settingsScreen {
		t.Fatalf("screen = %v, want settings", got.screen)
	}
	if value := got.settings.fields[fieldModel].Value(); value != "vendor/chosen" {
		t.Errorf("model field = %q, want vendor/chosen", value)
	}
}

func TestModelCardEscapeReturnsToTheList(t *testing.T) {
	m := newSettingsModel(t)
	m.screen = modelCardScreen
	m.picker.card = models.Entry{ID: "vendor/chosen"}

	back, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := back.(model); got.screen != modelSearchScreen {
		t.Errorf("screen = %v, want model search", got.screen)
	}
}

func TestPickerTogglesFiltersAndSort(t *testing.T) {
	m := newSettingsModel(t)
	opened, _ := m.openPicker()
	m = opened.(model)
	if m.screen != modelSearchScreen {
		t.Fatalf("screen = %v, want model search", m.screen)
	}
	if len(m.picker.entries) == 0 {
		t.Fatal("picker should load the embedded catalog")
	}

	toggled, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = toggled.(model)
	if !m.picker.filter.RequireTools {
		t.Error("Ctrl+T should require tool calling")
	}
	toggled, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = toggled.(model)
	if !m.picker.filter.RequireImages {
		t.Error("Ctrl+G should require image input")
	}
	toggled, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = toggled.(model)
	if m.picker.order != models.ByContext {
		t.Error("Ctrl+O should switch the sort order")
	}
	for _, entry := range m.picker.view {
		if !entry.Profile.ToolCalling || !entry.Profile.ImageInputs {
			t.Fatalf("%s does not satisfy the active filters", entry.ID)
		}
	}
}

func TestPickerFallsBackWhenTheLiveListFails(t *testing.T) {
	m := newSettingsModel(t)
	opened, _ := m.openPicker()
	m = opened.(model)
	m.picker.filter.AvailableOnly = true

	updated, _ := m.Update(liveModelsMsg{err: os.ErrDeadlineExceeded})
	got := updated.(model)
	if got.picker.filter.AvailableOnly {
		t.Error("a failed live lookup should stop filtering on availability")
	}
	if len(got.picker.view) == 0 {
		t.Error("the catalog should still be browsable without the live list")
	}
	if !strings.Contains(got.picker.status, "Live list unavailable") {
		t.Errorf("status = %q", got.picker.status)
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int]string{0: "—", 512: "512", 128000: "128k", 1048576: "1.0M"}
	for tokens, want := range cases {
		if got := formatTokens(tokens); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", tokens, got, want)
		}
	}
}

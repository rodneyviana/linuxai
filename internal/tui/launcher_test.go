package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"linuxai/internal/history"
)

func newTestStore(t *testing.T) *history.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := history.NewStore()
	if err != nil {
		t.Fatalf("history.NewStore: %v", err)
	}
	return store
}

func TestNewModelStartsOnExpectedScreen(t *testing.T) {
	store := newTestStore(t)
	id, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}

	fresh, err := newModel(store, id, true, false, false)
	if err != nil {
		t.Fatalf("newModel fresh: %v", err)
	}
	if fresh.screen != promptScreen || !fresh.prompt.Focused() {
		t.Errorf("fresh model screen/focus = %v/%v, want prompt/true", fresh.screen, fresh.prompt.Focused())
	}

	active, err := newModel(store, id, false, false, false)
	if err != nil {
		t.Fatalf("newModel active: %v", err)
	}
	if active.screen != menuScreen {
		t.Errorf("active model screen = %v, want menu", active.screen)
	}
}

func TestPromptSubmissionReturnsSelection(t *testing.T) {
	store := newTestStore(t)
	id, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	m, err := newModel(store, id, true, true, true)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	m.prompt.SetValue("  how do I find a file?  ")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	got := updated.(model)
	if cmd == nil {
		t.Fatal("Ctrl+S should return a quit command")
	}
	if got.result.ThreadID != id || got.result.Prompt != "how do I find a file?" || !got.result.Web {
		t.Errorf("result = %+v", got.result)
	}
}

func TestWebToggleIgnoredWhenUnavailable(t *testing.T) {
	store := newTestStore(t)
	id, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	m, err := newModel(store, id, true, false, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	got := updated.(model)
	if got.web {
		t.Error("Ctrl+W enabled web search without a configured SearXNG server")
	}
	if view := got.View(); !strings.Contains(view, "Search web unavailable") {
		t.Errorf("unavailable prompt view does not explain web configuration: %q", view)
	}
}

func TestNewChatMenuItemCreatesThreadAndOpensPrompt(t *testing.T) {
	store := newTestStore(t)
	oldID, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	m, err := newModel(store, oldID, false, false, false)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	m.menuCursor = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if got.currentID == oldID || got.screen != promptScreen {
		t.Errorf("new chat current/screen = %q/%v, want new id/prompt", got.currentID, got.screen)
	}
	if !store.ThreadExists(got.currentID) {
		t.Errorf("new thread %q does not exist", got.currentID)
	}
}

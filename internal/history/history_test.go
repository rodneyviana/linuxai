package history

import (
	"os"
	"testing"
	"time"
)

// newTestStore points a Store at a temp HOME so tests never touch the
// real ~/.local/share/linuxai.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestNewThreadSetsCurrent(t *testing.T) {
	store := newTestStore(t)

	id, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if id == "" {
		t.Fatal("NewThread returned empty id")
	}
	if !store.ThreadExists(id) {
		t.Errorf("ThreadExists(%q) = false, want true", id)
	}

	current, err := store.CurrentThreadID()
	if err != nil {
		t.Fatalf("CurrentThreadID: %v", err)
	}
	if current != id {
		t.Errorf("CurrentThreadID() = %q, want %q", current, id)
	}
}

func TestCurrentThreadIDCreatesWhenMissing(t *testing.T) {
	store := newTestStore(t)

	id, err := store.CurrentThreadID()
	if err != nil {
		t.Fatalf("CurrentThreadID: %v", err)
	}
	if id == "" {
		t.Fatal("expected a freshly created thread id")
	}
	if !store.ThreadExists(id) {
		t.Errorf("expected thread %q to exist after auto-creation", id)
	}
}

func TestAppendAndLoad(t *testing.T) {
	store := newTestStore(t)
	id, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}

	if err := store.Append(id, Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	if err := store.Append(id, Message{Role: "assistant", Content: "hi there"}); err != nil {
		t.Fatalf("Append assistant: %v", err)
	}

	messages, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Errorf("messages[0] = %+v, want user/hello", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "hi there" {
		t.Errorf("messages[1] = %+v, want assistant/hi there", messages[1])
	}
	for _, m := range messages {
		if m.TS == 0 {
			t.Errorf("message %+v has zero timestamp", m)
		}
	}
}

func TestLoadNonexistentThreadReturnsEmpty(t *testing.T) {
	store := newTestStore(t)

	messages, err := store.Load("does-not-exist")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if messages != nil {
		t.Errorf("messages = %+v, want nil", messages)
	}
}

func TestSetCurrentAndResume(t *testing.T) {
	store := newTestStore(t)

	first, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	second, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if second == first {
		t.Fatal("expected distinct thread ids")
	}

	// NewThread should have repointed current at the second thread.
	current, err := store.CurrentThreadID()
	if err != nil {
		t.Fatalf("CurrentThreadID: %v", err)
	}
	if current != second {
		t.Errorf("current = %q, want %q", current, second)
	}

	// Resuming the first thread should repoint current back to it.
	if err := store.SetCurrent(first); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	current, err = store.CurrentThreadID()
	if err != nil {
		t.Fatalf("CurrentThreadID: %v", err)
	}
	if current != first {
		t.Errorf("current = %q, want %q", current, first)
	}
}

func TestListOrdersMostRecentFirstWithTitles(t *testing.T) {
	store := newTestStore(t)

	older, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if err := store.Append(older, Message{Role: "user", Content: "older question"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Force distinct mtimes: without this, threads created in the same
	// instant could sort arbitrarily.
	olderPath := store.threadPath(older)
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(olderPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	newer, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if err := store.Append(newer, Message{Role: "user", Content: "newer question"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	threads, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("len(threads) = %d, want 2", len(threads))
	}
	if threads[0].ID != newer {
		t.Errorf("threads[0].ID = %q, want newer thread %q", threads[0].ID, newer)
	}
	if threads[0].Title != "newer question" {
		t.Errorf("threads[0].Title = %q, want %q", threads[0].Title, "newer question")
	}
	if threads[1].Title != "older question" {
		t.Errorf("threads[1].Title = %q, want %q", threads[1].Title, "older question")
	}
}

func TestSearchFindsMatchesCaseInsensitively(t *testing.T) {
	store := newTestStore(t)

	id, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if err := store.Append(id, Message{Role: "user", Content: "How do I check disk USAGE?"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Append(id, Message{Role: "assistant", Content: "Use df -h."}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	results, err := store.Search("disk usage")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != id {
		t.Errorf("results[0].ID = %q, want %q", results[0].ID, id)
	}

	if results, err := store.Search("nonexistent term"); err != nil || len(results) != 0 {
		t.Errorf("Search(nonexistent) = %+v, %v; want empty, nil", results, err)
	}
}

func TestReplayBudgetKeepsMostRecent(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, // ~10 tokens
		{Role: "assistant", Content: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{Role: "user", Content: "short"},
	}

	// Budget too small for everything: should keep only the most recent.
	got := ReplayBudget(messages, 3)
	if len(got) == 0 || got[len(got)-1].Content != "short" {
		t.Errorf("ReplayBudget with small budget = %+v, want to end with the most recent message", got)
	}

	// Generous budget: should keep everything.
	got = ReplayBudget(messages, 100000)
	if len(got) != len(messages) {
		t.Errorf("len(ReplayBudget with large budget) = %d, want %d", len(got), len(messages))
	}

	// Zero/negative budget means no trimming.
	got = ReplayBudget(messages, 0)
	if len(got) != len(messages) {
		t.Errorf("ReplayBudget(messages, 0) = %+v, want unchanged", got)
	}
}

func TestNewThreadIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		id, err := NewThreadID()
		if err != nil {
			t.Fatalf("NewThreadID: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate thread id generated: %s", id)
		}
		seen[id] = true
	}
}

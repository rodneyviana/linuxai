package main

import (
	"strings"
	"testing"

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

func TestResolveThreadNewCreatesFreshThread(t *testing.T) {
	store := newTestStore(t)

	existing, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}

	id, err := resolveThread(store, &cliArgs{New: true})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if id == existing {
		t.Error("--new should create a different thread than the existing current one")
	}
	if !store.ThreadExists(id) {
		t.Errorf("resolved thread %q does not exist", id)
	}
}

func TestResolveThreadBareContinuesCurrent(t *testing.T) {
	store := newTestStore(t)

	current, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}

	id, err := resolveThread(store, &cliArgs{})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if id != current {
		t.Errorf("bare invocation resolved to %q, want current thread %q", id, current)
	}
}

func TestResolveThreadResumeSwitchesCurrent(t *testing.T) {
	store := newTestStore(t)

	first, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if _, err := store.NewThread(); err != nil { // becomes current
		t.Fatalf("NewThread: %v", err)
	}

	id, err := resolveThread(store, &cliArgs{ResumeGiven: true, Resume: first})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if id != first {
		t.Errorf("resolveThread with --resume = %q, want %q", id, first)
	}

	current, err := store.CurrentThreadID()
	if err != nil {
		t.Fatalf("CurrentThreadID: %v", err)
	}
	if current != first {
		t.Errorf("current thread after --resume = %q, want %q", current, first)
	}
}

func TestResolveThreadResumeNonexistentErrors(t *testing.T) {
	store := newTestStore(t)

	_, err := resolveThread(store, &cliArgs{ResumeGiven: true, Resume: "no-such-thread"})
	if err == nil {
		t.Fatal("expected an error when resuming a thread that does not exist")
	}
}

func TestReadPromptPrefersArgOverStdin(t *testing.T) {
	got, err := readPrompt("already have a prompt")
	if err != nil {
		t.Fatalf("readPrompt: %v", err)
	}
	if got != "already have a prompt" {
		t.Errorf("readPrompt = %q, want the arg prompt unchanged", got)
	}
}

func TestLoadImageEmptySourceIsNoop(t *testing.T) {
	dataURL, ref, err := loadImage("")
	if err != nil {
		t.Fatalf("loadImage: %v", err)
	}
	if dataURL != "" || ref != "" {
		t.Errorf("loadImage(\"\") = (%q, %q), want empty/empty", dataURL, ref)
	}
}

func TestLoadImageMissingFile(t *testing.T) {
	_, _, err := loadImage("/nonexistent/path/to/image.png")
	if err == nil {
		t.Fatal("expected an error for a missing image file")
	}
	if !strings.Contains(err.Error(), "opening image") {
		t.Errorf("error = %v, want it to mention opening the image", err)
	}
}

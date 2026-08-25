package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"linuxai/internal/history"
)

func TestPrintHelp(t *testing.T) {
	var output bytes.Buffer
	printHelp(&output)
	for _, text := range []string{"Usage:", "-n, --new", "-r, --resume ID", "-h, --help"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("help output does not contain %q:\n%s", text, output.String())
		}
	}
}

func TestBuildMessagesPrependsSystemInstructions(t *testing.T) {
	prior := []history.Message{{Role: "user", Content: "earlier question"}, {Role: "assistant", Content: "earlier answer"}}
	messages := buildMessages("stay on topic", prior, "new question", "data:image/jpeg;base64,AAAA")
	if len(messages) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(messages))
	}
	if messages[0].Role != "system" || messages[0].Content != "stay on topic" {
		t.Errorf("messages[0] = %+v, want system instructions", messages[0])
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || last.Content != "new question" || last.ImageDataURL == "" {
		t.Errorf("last message = %+v, want new multimodal user prompt", last)
	}
}

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

	id, created, err := resolveThread(store, &cliArgs{New: true})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if !created {
		t.Error("--new should report that it created a thread")
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
	if err := store.Append(current, history.Message{Role: "user", Content: "active question"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	id, created, err := resolveThread(store, &cliArgs{})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if created {
		t.Error("an active current thread should be continued")
	}
	if id != current {
		t.Errorf("bare invocation resolved to %q, want current thread %q", id, current)
	}
}

func TestResolveThreadEmptyCurrentOpensFreshPrompt(t *testing.T) {
	store := newTestStore(t)

	current, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	id, fresh, err := resolveThread(store, &cliArgs{})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if id != current || !fresh {
		t.Errorf("resolveThread = %q, %v; want existing empty thread %q as fresh", id, fresh, current)
	}
}

func TestResolveThreadIdleCreatesFreshThread(t *testing.T) {
	store := newTestStore(t)

	current, err := store.NewThread()
	if err != nil {
		t.Fatalf("NewThread: %v", err)
	}
	if err := store.Append(current, history.Message{Role: "user", Content: "old question"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	modified, err := store.ThreadModified(current)
	if err != nil {
		t.Fatalf("ThreadModified: %v", err)
	}

	id, created, err := resolveThreadAt(store, &cliArgs{}, modified.Add(threadIdleTimeout+time.Second))
	if err != nil {
		t.Fatalf("resolveThreadAt: %v", err)
	}
	if !created {
		t.Error("an idle current thread should create a new thread")
	}
	if id == current {
		t.Errorf("idle invocation resolved to old thread %q", current)
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

	id, created, err := resolveThread(store, &cliArgs{ResumeGiven: true, Resume: first})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	if created {
		t.Error("--resume should not report that it created a thread")
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

	_, _, err := resolveThread(store, &cliArgs{ResumeGiven: true, Resume: "no-such-thread"})
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

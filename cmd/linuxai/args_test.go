package main

import (
	"strings"
	"testing"
)

func TestParseArgsBarePrompt(t *testing.T) {
	a, err := parseArgs(strings.Fields("how do I list hidden files in bash"))
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if a.Prompt != "how do I list hidden files in bash" {
		t.Errorf("Prompt = %q, want the joined bare words", a.Prompt)
	}
	if a.New || a.List || a.Web || a.ResumeGiven || a.SearchGiven || a.Image != "" {
		t.Errorf("unexpected flags set: %+v", a)
	}
}

func TestParseArgsFlagsAnywhereInArgv(t *testing.T) {
	a, err := parseArgs([]string{"--web", "how", "do", "I", "--new", "check", "disk", "usage"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !a.Web || !a.New {
		t.Errorf("expected --web and --new to be recognized regardless of position, got %+v", a)
	}
	if a.Prompt != "how do I check disk usage" {
		t.Errorf("Prompt = %q, want flags stripped and remaining words joined in order", a.Prompt)
	}
}

func TestParseArgsResume(t *testing.T) {
	a, err := parseArgs([]string{"--resume", "20260704-143347-b9c9bd", "one", "more", "thing"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !a.ResumeGiven || a.Resume != "20260704-143347-b9c9bd" {
		t.Errorf("Resume = %q, ResumeGiven = %v, want the thread id consumed as a value", a.Resume, a.ResumeGiven)
	}
	if a.Prompt != "one more thing" {
		t.Errorf("Prompt = %q, want %q", a.Prompt, "one more thing")
	}
}

func TestParseArgsResumeMissingValue(t *testing.T) {
	if _, err := parseArgs([]string{"--resume"}); err == nil {
		t.Error("expected an error when --resume has no following id")
	}
}

func TestParseArgsSearch(t *testing.T) {
	a, err := parseArgs([]string{"--search", "inode"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !a.SearchGiven || a.Search != "inode" {
		t.Errorf("Search = %q, SearchGiven = %v, want %q/true", a.Search, a.SearchGiven, "inode")
	}
}

func TestParseArgsSearchMissingValue(t *testing.T) {
	if _, err := parseArgs([]string{"--search"}); err == nil {
		t.Error("expected an error when --search has no following term")
	}
}

func TestParseArgsImage(t *testing.T) {
	a, err := parseArgs([]string{"--image", "/tmp/shot.png", "what", "is", "this"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if a.Image != "/tmp/shot.png" {
		t.Errorf("Image = %q, want %q", a.Image, "/tmp/shot.png")
	}
	if a.Prompt != "what is this" {
		t.Errorf("Prompt = %q, want %q", a.Prompt, "what is this")
	}
}

func TestParseArgsImageMissingValue(t *testing.T) {
	if _, err := parseArgs([]string{"--image"}); err == nil {
		t.Error("expected an error when --image has no following path")
	}
}

func TestParseArgsClipboardSentinel(t *testing.T) {
	a, err := parseArgs([]string{"--clipboard", "describe", "this"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if a.Image != "-" {
		t.Errorf("Image = %q, want the clipboard sentinel %q", a.Image, "-")
	}
}

func TestParseArgsWebPrefix(t *testing.T) {
	a, err := parseArgs([]string{"/web", "what", "is", "new"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !a.Web {
		t.Error("expected a leading '/web ' prompt prefix to set Web = true")
	}
	if a.Prompt != "what is new" {
		t.Errorf("Prompt = %q, want the prefix stripped", a.Prompt)
	}
}

func TestParseArgsNoWebPrefixWithoutTrailingSpace(t *testing.T) {
	// "/webinar" should not be mistaken for the /web trigger.
	a, err := parseArgs([]string{"/webinar", "schedule"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if a.Web {
		t.Error("a word merely starting with /web should not trigger web grounding")
	}
	if a.Prompt != "/webinar schedule" {
		t.Errorf("Prompt = %q, want unchanged", a.Prompt)
	}
}

func TestParseArgsVersion(t *testing.T) {
	a, err := parseArgs([]string{"--version"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !a.Version {
		t.Error("expected --version to be recognized")
	}
	if a.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", a.Prompt)
	}
}

func TestParseArgsEmpty(t *testing.T) {
	a, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if a.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", a.Prompt)
	}
}

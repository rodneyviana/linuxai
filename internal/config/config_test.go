package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"NVIDIA_API_KEY", "LINUXAI_BASE_URL", "LINUXAI_MODEL", "LINUXAI_SEARXNG_URL"} {
		t.Setenv(key, "")
		// t.Setenv leaves the var present-but-empty; Load treats "" as
		// unset via os.Getenv, which is what we want here.
	}
}

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("NVIDIA_API_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "test-key")
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want default %q", cfg.BaseURL, DefaultBaseURL)
	}
	if cfg.Model != DefaultModel {
		t.Errorf("Model = %q, want default %q", cfg.Model, DefaultModel)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("NVIDIA_API_KEY", "test-key")
	t.Setenv("LINUXAI_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("LINUXAI_MODEL", "custom-model")
	t.Setenv("LINUXAI_SEARXNG_URL", "http://localhost:8888")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("BaseURL = %q, want override", cfg.BaseURL)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("Model = %q, want override", cfg.Model)
	}
	if cfg.SearXNGURL != "http://localhost:8888" {
		t.Errorf("SearXNGURL = %q, want override", cfg.SearXNGURL)
	}
}

func TestLoadMissingKeyErrorsAgainstDefaultBackend(t *testing.T) {
	clearConfigEnv(t)

	if _, err := Load(); err == nil {
		t.Error("expected error when NVIDIA_API_KEY is unset and using the default (hosted) backend")
	}
}

func TestLoadMissingKeyOKForOllama(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LINUXAI_BASE_URL", "http://localhost:11434/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load should not require a key for a non-default base URL: %v", err)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
}

func TestLoadInstructionsDefaultsWhenMissingOrBlank(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, err := LoadInstructions()
	if err != nil {
		t.Fatalf("LoadInstructions missing: %v", err)
	}
	if got != DefaultInstructions {
		t.Errorf("missing instructions = %q, want default %q", got, DefaultInstructions)
	}
	for _, phrase := range []string{"do not apologize", "operating systems", "suggest a computing-related way to reframe"} {
		if !strings.Contains(strings.ToLower(got), phrase) {
			t.Errorf("default instructions do not contain %q: %q", phrase, got)
		}
	}

	path, err := InstructionsPath()
	if err != nil {
		t.Fatalf("InstructionsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(" \n\t"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err = LoadInstructions()
	if err != nil {
		t.Fatalf("LoadInstructions blank: %v", err)
	}
	if got != DefaultInstructions {
		t.Errorf("blank instructions = %q, want default %q", got, DefaultInstructions)
	}
}

func TestLoadInstructionsUsesConfigFile(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	path := filepath.Join(configHome, "linuxai", "instructions.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("  Answer in haiku.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadInstructions()
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if got != "Answer in haiku." {
		t.Errorf("instructions = %q, want trimmed file content", got)
	}
}

func TestValidateWeb(t *testing.T) {
	if err := (&Config{}).ValidateWeb(); err == nil || !strings.Contains(err.Error(), "LINUXAI_SEARXNG_URL") {
		t.Errorf("unconfigured ValidateWeb error = %v", err)
	}
	if err := (&Config{SearXNGURL: "http://localhost:8080"}).ValidateWeb(); err != nil {
		t.Errorf("configured ValidateWeb: %v", err)
	}
}

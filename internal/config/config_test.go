package config

import "testing"

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
	if cfg.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want default %q", cfg.BaseURL, defaultBaseURL)
	}
	if cfg.Model != defaultModel {
		t.Errorf("Model = %q, want default %q", cfg.Model, defaultModel)
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

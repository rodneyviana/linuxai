package config

import (
	"fmt"
	"os"
)

const (
	defaultBaseURL = "https://integrate.api.nvidia.com/v1"
	defaultModel   = "qwen/qwen3.5-122b-a10b"
)

// Config holds the runtime configuration for linuxai, sourced from the
// process environment (after LoadDotEnv has had a chance to fill gaps).
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	SearXNGURL string
}

// Load reads configuration from the environment. Call LoadDotEnv first if
// .env support is desired.
func Load() (*Config, error) {
	cfg := &Config{
		APIKey:     os.Getenv("NVIDIA_API_KEY"),
		BaseURL:    getEnvDefault("LINUXAI_BASE_URL", defaultBaseURL),
		Model:      getEnvDefault("LINUXAI_MODEL", defaultModel),
		SearXNGURL: os.Getenv("LINUXAI_SEARXNG_URL"),
	}

	if cfg.APIKey == "" && cfg.BaseURL == defaultBaseURL {
		return nil, fmt.Errorf("NVIDIA_API_KEY is not set (set it in the environment or in .env)")
	}

	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

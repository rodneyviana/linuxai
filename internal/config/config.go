package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultBaseURL = "https://integrate.api.nvidia.com/v1"
	DefaultModel   = "openai/gpt-oss-20b"

	// Environment variable names that the settings dialog can edit.
	KeyAPIKey  = "NVIDIA_API_KEY"
	KeyBaseURL = "LINUXAI_BASE_URL"
	KeyModel   = "LINUXAI_MODEL"
	KeySearXNG = "LINUXAI_SEARXNG_URL"

	DefaultInstructions = "Only answer questions about operating systems, especially Linux if no OS is specified, and programming. Do not be verbose unless required. If a question is outside this scope, do not apologize or give only a generic refusal. Briefly explain that you can help with operating systems, Linux, command-line tools, system administration, software development, debugging, and programming, give one or two relevant examples, and suggest a computing-related way to reframe the question when natural."
)

// Config holds the runtime configuration for linuxai, sourced from the
// process environment (after LoadDotEnv has had a chance to fill gaps).
type Config struct {
	APIKey       string
	BaseURL      string
	Model        string
	SearXNGURL   string
	Instructions string
}

// Load reads configuration from the environment. Call LoadDotEnv first if
// .env support is desired.
func Load() (*Config, error) {
	instructions, err := LoadInstructions()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		APIKey:       os.Getenv(KeyAPIKey),
		BaseURL:      getEnvDefault(KeyBaseURL, DefaultBaseURL),
		Model:        getEnvDefault(KeyModel, DefaultModel),
		SearXNGURL:   strings.TrimSpace(os.Getenv(KeySearXNG)),
		Instructions: instructions,
	}

	if cfg.APIKey == "" && cfg.BaseURL == DefaultBaseURL {
		return nil, fmt.Errorf("NVIDIA_API_KEY is not set (set it in the environment or in .env)")
	}

	return cfg, nil
}

func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving config directory: %w", err)
	}
	return filepath.Join(dir, "linuxai"), nil
}

func InstructionsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "instructions.txt"), nil
}

// EnvPath is the user-level .env the settings dialog reads and writes.
func EnvPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".env"), nil
}

// ModelsPath is where an updated model catalog is cached.
func ModelsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

func LoadInstructions() (string, error) {
	path, err := InstructionsPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultInstructions, nil
		}
		return "", fmt.Errorf("reading instructions: %w", err)
	}
	instructions := strings.TrimSpace(string(data))
	if instructions == "" {
		return DefaultInstructions, nil
	}
	return instructions, nil
}

func (c *Config) ValidateWeb() error {
	if c.SearXNGURL == "" {
		return fmt.Errorf("web search is not configured; set LINUXAI_SEARXNG_URL in the environment or config .env")
	}
	return nil
}

func WebConfigured() bool {
	return strings.TrimSpace(os.Getenv("LINUXAI_SEARXNG_URL")) != ""
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

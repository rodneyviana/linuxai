// Package config loads linuxai configuration from the process environment,
// optionally filled in from a .env file.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadEnvFile parses a .env-style file and calls os.Setenv for any key
// that is not already set in the process environment. Missing files are
// not an error.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = unquote(value)

		if _, present := os.LookupEnv(key); !present {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

// unquote strips one layer of matching single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// LoadDotEnv loads ./.env and ~/.config/linuxai/.env, in that order, with
// ./.env taking precedence for any key it sets. Real process environment
// variables always win over either file.
func LoadDotEnv() error {
	if err := loadEnvFile(".env"); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err == nil {
		if err := loadEnvFile(filepath.Join(home, ".config", "linuxai", ".env")); err != nil {
			return err
		}
	}
	return nil
}

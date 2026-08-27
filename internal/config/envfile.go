package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadEnvFile parses a .env file into a map. A missing file yields an empty
// map rather than an error, so the settings dialog can start from scratch.
func ReadEnvFile(path string) (map[string]string, error) {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := splitEnvLine(line)
		if ok {
			values[key] = value
		}
	}
	return values, nil
}

// WriteEnvFile updates path with the given key/value pairs. Existing comments,
// ordering, and keys that are not being changed are preserved; new keys are
// appended. The file is written with owner-only permissions because it holds
// the API key.
func WriteEnvFile(path string, updates map[string]string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	remaining := make(map[string]string, len(updates))
	for key, value := range updates {
		remaining[key] = value
	}

	var lines []string
	if len(existing) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(existing), "\n"), "\n")
	}
	for index, line := range lines {
		key, _, ok := splitEnvLine(line)
		if !ok {
			continue
		}
		value, found := remaining[key]
		if !found {
			continue
		}
		prefix := ""
		if strings.HasPrefix(strings.TrimSpace(line), "export ") {
			prefix = "export "
		}
		lines[index] = prefix + key + "=" + quoteEnvValue(value)
		delete(remaining, key)
	}

	for _, key := range []string{KeyAPIKey, KeyBaseURL, KeyModel, KeySearXNG} {
		value, found := remaining[key]
		if !found {
			continue
		}
		lines = append(lines, key+"="+quoteEnvValue(value))
		delete(remaining, key)
	}
	for key, value := range remaining {
		lines = append(lines, key+"="+quoteEnvValue(value))
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// ApplyEnv sets the values in the current process so a save takes effect
// without restarting.
func ApplyEnv(values map[string]string) {
	for key, value := range values {
		os.Setenv(key, value)
	}
}

// splitEnvLine parses one active assignment. Blank lines and comments return
// ok=false so callers leave them untouched.
func splitEnvLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	key, value, found := strings.Cut(trimmed, "=")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(key), unquote(strings.TrimSpace(value)), true
}

// quoteEnvValue wraps the value in double quotes when it contains characters
// the loader would otherwise misread. The loader strips one layer of quotes
// and does not process escapes, so neither does this.
func quoteEnvValue(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t#") {
		return `"` + strings.ReplaceAll(value, `"`, "") + `"`
	}
	return value
}

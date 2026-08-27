package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEnvFileMissingIsEmpty(t *testing.T) {
	values, err := ReadEnvFile(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("values = %v, want empty", values)
	}
}

func TestReadEnvFileParsesActiveLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# comment\n\nNVIDIA_API_KEY=abc123\nexport LINUXAI_MODEL=\"vendor/model\"\n#LINUXAI_BASE_URL=ignored\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	values, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if values[KeyAPIKey] != "abc123" {
		t.Errorf("api key = %q", values[KeyAPIKey])
	}
	if values[KeyModel] != "vendor/model" {
		t.Errorf("model = %q", values[KeyModel])
	}
	if _, present := values[KeyBaseURL]; present {
		t.Error("commented-out keys must not be read")
	}
}

func TestWriteEnvFilePreservesCommentsAndUpdatesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# linuxai config\nNVIDIA_API_KEY=old-key\n\n# a note\nexport LINUXAI_MODEL=old/model\nOTHER_KEY=keep\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	updates := map[string]string{
		KeyAPIKey:  "new-key",
		KeyModel:   "new/model",
		KeyBaseURL: "http://localhost:11434/v1",
		KeySearXNG: "",
	}
	if err := WriteEnvFile(path, updates); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	written := string(data)
	for _, want := range []string{"# linuxai config", "# a note", "OTHER_KEY=keep", "export LINUXAI_MODEL=new/model", "NVIDIA_API_KEY=new-key", "LINUXAI_BASE_URL=http://localhost:11434/v1"} {
		if !strings.Contains(written, want) {
			t.Errorf("written file is missing %q:\n%s", want, written)
		}
	}
	if strings.Contains(written, "old-key") || strings.Contains(written, "old/model") {
		t.Errorf("old values survived:\n%s", written)
	}

	values, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	for key, want := range updates {
		if values[key] != want {
			t.Errorf("%s = %q, want %q", key, values[key], want)
		}
	}
}

func TestWriteEnvFileCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".env")
	if err := WriteEnvFile(path, map[string]string{KeyAPIKey: "secret value"}); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}

	values, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if values[KeyAPIKey] != "secret value" {
		t.Errorf("round trip of a spaced value = %q", values[KeyAPIKey])
	}
}

func TestApplyEnv(t *testing.T) {
	t.Setenv(KeySearXNG, "")
	ApplyEnv(map[string]string{KeySearXNG: "http://localhost:8080"})
	if os.Getenv(KeySearXNG) != "http://localhost:8080" {
		t.Errorf("ApplyEnv did not set the process environment")
	}
	if !WebConfigured() {
		t.Error("WebConfigured should follow the applied value")
	}
}

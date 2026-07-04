package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestLoadEnvFileParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeFile(t, path, `
# a comment line
FOO=bar

export BAZ=qux
QUOTED="hello world"
SINGLE='it''s here'
SPACED   =   trimmed
`)

	for _, key := range []string{"FOO", "BAZ", "QUOTED", "SINGLE", "SPACED"} {
		os.Unsetenv(key)
	}
	t.Cleanup(func() {
		for _, key := range []string{"FOO", "BAZ", "QUOTED", "SINGLE", "SPACED"} {
			os.Unsetenv(key)
		}
	})

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}

	cases := map[string]string{
		"FOO":    "bar",
		"BAZ":    "qux",
		"QUOTED": "hello world",
		"SINGLE": "it''s here",
		"SPACED": "trimmed",
	}
	for key, want := range cases {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadEnvFileDoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	writeFile(t, path, "EXISTING=from_file\n")

	t.Setenv("EXISTING", "from_process")

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if got := os.Getenv("EXISTING"); got != "from_process" {
		t.Errorf("EXISTING = %q, want %q (process env must win)", got, "from_process")
	}
}

func TestLoadEnvFileMissingIsNotError(t *testing.T) {
	if err := loadEnvFile("/nonexistent/path/.env"); err != nil {
		t.Errorf("missing file should not error, got %v", err)
	}
}

func TestUnquote(t *testing.T) {
	cases := map[string]string{
		`"hello"`:   "hello",
		`'hello'`:   "hello",
		`hello`:     "hello",
		`"mixed'`:   `"mixed'`,
		`""`:        "",
		`'`:         "'",
		`unquoted"`: `unquoted"`,
	}
	for in, want := range cases {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadDotEnvPrecedence(t *testing.T) {
	// ./.env should win over ~/.config/linuxai/.env for keys both set.
	workDir := t.TempDir()
	homeDir := t.TempDir()

	writeFile(t, filepath.Join(workDir, ".env"), "SHARED=from_local\nLOCAL_ONLY=local\n")
	configDir := filepath.Join(homeDir, ".config", "linuxai")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(configDir, ".env"), "SHARED=from_home\nHOME_ONLY=home\n")

	for _, key := range []string{"SHARED", "LOCAL_ONLY", "HOME_ONLY"} {
		os.Unsetenv(key)
	}
	t.Cleanup(func() {
		for _, key := range []string{"SHARED", "LOCAL_ONLY", "HOME_ONLY"} {
			os.Unsetenv(key)
		}
	})

	t.Setenv("HOME", homeDir)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origWD) })

	if err := LoadDotEnv(); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("SHARED"); got != "from_local" {
		t.Errorf("SHARED = %q, want %q (./.env should win)", got, "from_local")
	}
	if got := os.Getenv("LOCAL_ONLY"); got != "local" {
		t.Errorf("LOCAL_ONLY = %q, want %q", got, "local")
	}
	if got := os.Getenv("HOME_ONLY"); got != "home" {
		t.Errorf("HOME_ONLY = %q, want %q", got, "home")
	}
}

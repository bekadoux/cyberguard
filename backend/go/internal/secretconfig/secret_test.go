package secretconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func values(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := env[key]; return value, ok }
}

func TestValueSourcesAndNewlines(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", " secret \n", "password"} {
		got, err := Value(values(map[string]string{"TOKEN": value}), "TOKEN")
		if err != nil || got != value {
			t.Fatal("direct environment value changed")
		}
	}
	if got, err := Value(values(nil), "TOKEN"); err != nil || got != "" {
		t.Fatal("unset value must remain available to caller validation")
	}
	for _, ending := range []string{"", "\n", "\r\n"} {
		path := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(path, []byte("  sensitive-password  "+ending), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := Value(values(map[string]string{"TOKEN_FILE": path}), "TOKEN")
		if err != nil || got != "  sensitive-password  " {
			t.Fatal("file whitespace was changed beyond the terminal newline")
		}
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("password\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := Value(values(map[string]string{"TOKEN_FILE": path}), "TOKEN"); err != nil || got != "password\n" {
		t.Fatal("more than one terminal newline was removed")
	}
}

func TestValueFailsClosedWithoutLeaking(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "sensitive-path")
	for _, env := range []map[string]string{
		{"TOKEN": "sensitive-password", "TOKEN_FILE": path},
		{"TOKEN": "", "TOKEN_FILE": path},
		{"TOKEN_FILE": ""}, {"TOKEN_FILE": "  "},
		{"TOKEN_FILE": path}, {"TOKEN_FILE": directory},
	} {
		got, err := Value(values(env), "TOKEN")
		if err == nil || got != "" || !strings.Contains(err.Error(), "TOKEN") || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), directory) {
			t.Fatal("invalid source did not fail safely")
		}
	}
	for _, content := range []string{"", "\n", "\r\n", "sensitive\x00value", strings.Repeat("s", maxFileBytes+1)} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := Value(values(map[string]string{"TOKEN_FILE": path}), "TOKEN"); err == nil || got != "" || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("invalid file contents did not fail safely")
		}
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	if os.Geteuid() != 0 {
		if _, err := Value(values(map[string]string{"TOKEN_FILE": path}), "TOKEN"); err == nil {
			t.Fatal("unreadable file accepted")
		}
	}
}

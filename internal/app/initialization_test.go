package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitUsesEmbeddedTemplates(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--cwd", root, "--init"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	for _, name := range []string{"config.json", "llm.json", "mcp.json", "tools.json", "prompts/default.md", "modes/coder.md", "coderenga.db"} {
		if _, err := os.Stat(filepath.Join(root, "coderenga.d", filepath.FromSlash(name))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	stderr.Reset()
	if code := Run([]string{"--cwd", root, "--init"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root}); code != 1 || !strings.Contains(stderr.String(), "coderenga.d already exists") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestUninitializedErrorSuggestsInit(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "hello"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root})
	if code != 1 || !strings.Contains(stderr.String(), `Run "coderenga --init"`) || !strings.Contains(stderr.String(), "coderenga.d/llm.json") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestOldConfigurationErrorSuggestsMigration(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"profiles":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "hello"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root})
	if code != 1 || !strings.Contains(stderr.String(), "old configuration format detected") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

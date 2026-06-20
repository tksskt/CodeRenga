package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSplitConfiguration(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", `{"version":1,"defaultMode":"coder","defaultProfile":"x","state":{"database":"state.db"}}`)
	write("llm.json", `{"version":1,"profiles":{"x":{"baseURL":"http://x/v1","model":"m","maxTokens":99}}}`)
	write("mcp.json", `{"version":1,"servers":{"docs":{"transport":"http_sse","url":"http://mcp","enabled":true}}}`)
	write("tools.json", `{"version":1,"policies":{"builtin.read_file":"block"},"plugins":{}}`)
	c, loaded, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 4 || c.Storage.Path != filepath.Join(dir, "state.db") || c.Profiles["x"].Model != "m" || c.MCP.Servers["docs"].URL != "http://mcp" || c.ToolPolicies["builtin.read_file"] != "block" {
		t.Fatalf("bad split config: %#v loaded=%v", c, loaded)
	}
}

func TestLoadReportsNotInitialized(t *testing.T) {
	if _, _, err := Load(t.TempDir(), t.TempDir(), ""); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadDetectsOldFormatBeforeMissingLLM(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"profiles":{"local":{"model":"old"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root, root, ""); !errors.Is(err, ErrOldFormat) {
		t.Fatalf("err=%v", err)
	}
}

func TestExplicitConfigErrorIncludesPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.json")
	if _, _, err := Load(t.TempDir(), t.TempDir(), p); err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("err=%v", err)
	}
}

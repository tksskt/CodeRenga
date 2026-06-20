package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "coderenga.d")
	for _, sub := range []string{"prompts", "modes"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"local","state":{"database":"coderenga.db"}}`,
		"llm.json":           `{"version":1,"profiles":{"local":{"baseURL":"http://127.0.0.1:1/v1","model":"test"}}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

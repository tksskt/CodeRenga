package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileAndModelOverridesUseLLMConfiguration(t *testing.T) {
	root := writeProfileTestConfig(t)
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, Profile: "remote", Model: "override-model", NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Profile != "remote" || rt.Model != "override-model" || rt.Config.Profiles["remote"].BaseURL != "http://remote/v1" {
		t.Fatalf("profile=%s model=%s config=%#v", rt.Profile, rt.Model, rt.Config.Profiles["remote"])
	}
}

func TestUnknownProfileNamesLLMConfigFile(t *testing.T) {
	root := writeProfileTestConfig(t)
	_, err := New(context.Background(), Options{BinaryDir: root, CWD: root, Profile: "missing", NoPersist: true})
	if err == nil || !strings.Contains(err.Error(), `profile "missing" was not found in coderenga.d/llm.json`) {
		t.Fatalf("err=%v", err)
	}
}

func writeProfileTestConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"local","state":{"database":"coderenga.db"}}`,
		"llm.json":           `{"version":1,"profiles":{"local":{"baseURL":"http://local/v1","model":"local-model"},"remote":{"baseURL":"http://remote/v1","model":"remote-model"}}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

package initfs

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	templatefs "github.com/tks/coderenga/templates"
	_ "modernc.org/sqlite"
)

func TestInitializeCreatesEmbeddedTemplatesAndDatabase(t *testing.T) {
	root := t.TempDir()
	if err := Initialize(root, templatefs.Files); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "llm.json", "mcp.json", "tools.json", "prompts/default.md", "prompts/compact.md", "modes/coder.md", "modes/architect.md", "modes/debug.md", "modes/reviewer.md", "modes/documenter.md", "coderenga.db"} {
		if _, err := os.Stat(filepath.Join(root, "coderenga.d", filepath.FromSlash(name))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "coderenga.d", "coderenga.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration count=%d err=%v", count, err)
	}
}

func TestInitializeRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "coderenga.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Initialize(root, templatefs.Files)
	if err == nil || !strings.Contains(err.Error(), "coderenga.d already exists") {
		t.Fatalf("err=%v", err)
	}
}
func TestInitializeCreatesPublicContractPromptTemplates(t *testing.T) {
	root := t.TempDir()
	if err := Initialize(root, templatefs.Files); err != nil {
		t.Fatal(err)
	}
	checks := map[string][]string{
		"prompts/default.md": {"Public contract preservation", "If the specification says `line`, keep `line`"},
		"modes/coder.md":     {"Public contract discipline", "do not output `line_number`"},
	}
	for rel, wants := range checks {
		body, err := os.ReadFile(filepath.Join(root, "coderenga.d", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("failed to read initialized %s: %v", rel, err)
		}
		text := string(body)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("initialized %s missing %q", rel, want)
			}
		}
	}
}

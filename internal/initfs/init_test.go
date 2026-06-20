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
	for _, name := range []string{"config.json", "llm.json", "mcp.json", "tools.json", "prompts/default.md", "prompts/compact.md", "modes/coder.md", "modes/architect.md", "modes/debug.md", "modes/reviewer.md", "coderenga.db"} {
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

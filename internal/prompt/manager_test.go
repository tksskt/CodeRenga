package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/config"
)

func TestBuildOrdersSystemModeThenProjectInstructions(t *testing.T) {
	root := t.TempDir()
	system := filepath.Join(root, "default.md")
	modes := filepath.Join(root, "modes")
	if err := os.Mkdir(modes, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{system: "SYSTEM", filepath.Join(modes, "coder.md"): "MODE", filepath.Join(root, "AGENTS.md"): "PROJECT"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	cfg.Prompt.SystemFiles = []string{system}
	cfg.Prompt.GlobalModeDir = modes
	cfg.Prompt.ModeDir = ""
	cfg.Prompt.ProjectInstructionFiles = []string{"AGENTS.md"}
	m, err := Load(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	built := m.Build("coder")
	if !(strings.Index(built, "SYSTEM") < strings.Index(built, "MODE") && strings.Index(built, "MODE") < strings.Index(built, "PROJECT")) {
		t.Fatalf("prompt order=%q", built)
	}
}

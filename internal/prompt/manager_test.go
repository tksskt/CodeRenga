package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/config"
)

func TestParseMode(t *testing.T) {
	t.Run("yaml front matter", func(t *testing.T) {
		s := `---
name: code
description: coding mode
write: true
shell: allow
mcp: true
plan_first: true
tool_allow: builtin.read_file,builtin.write_file
tool_deny: shell.run
---
You are a coding agent.`
		m := parseMode(s, "fallback")
		if m.Name != "code" {
			t.Fatalf("name = %q", m.Name)
		}
		if m.Description != "coding mode" {
			t.Fatalf("description = %q", m.Description)
		}
		if m.Write != "true" {
			t.Fatalf("write = %q", m.Write)
		}
		if m.Shell != "allow" {
			t.Fatalf("shell = %q", m.Shell)
		}
		if !m.MCP {
			t.Fatal("mcp should be true")
		}
		if !m.PlanFirst {
			t.Fatal("plan_first should be true")
		}
		if len(m.ToolAllow) != 2 || m.ToolAllow[0] != "builtin.read_file" {
			t.Fatalf("tool_allow = %v", m.ToolAllow)
		}
		if len(m.ToolDeny) != 1 || m.ToolDeny[0] != "shell.run" {
			t.Fatalf("tool_deny = %v", m.ToolDeny)
		}
		if m.Prompt != "You are a coding agent." {
			t.Fatalf("prompt = %q", m.Prompt)
		}
	})
	t.Run("no front matter", func(t *testing.T) {
		s := "Just a prompt."
		m := parseMode(s, "fallback")
		if m.Name != "fallback" {
			t.Fatalf("name = %q", m.Name)
		}
		if m.Prompt != s {
			t.Fatalf("prompt = %q", m.Prompt)
		}
	})
}

func TestParseCSV(t *testing.T) {
	out := parseCSV("a, b , c")
	if len(out) != 3 || out[0] != "a" || out[1] != "b" || out[2] != "c" {
		t.Fatalf("parseCSV = %v", out)
	}
	if len(parseCSV("")) != 0 {
		t.Fatal("empty should be empty")
	}
}

func TestManagerReload(t *testing.T) {
	tmp := t.TempDir()
	sysFile := filepath.Join(tmp, "system.md")
	projFile := filepath.Join(tmp, "project.md")
	modeDir := filepath.Join(tmp, "modes")
	os.MkdirAll(modeDir, 0o755)
	os.WriteFile(sysFile, []byte("SYSTEM"), 0o644)
	os.WriteFile(projFile, []byte("PROJECT"), 0o644)
	os.WriteFile(filepath.Join(modeDir, "code.md"), []byte("---\nname: code\n---\nPROMPT"), 0o644)
	cfg := config.Config{
		Prompt: config.PromptConfig{
			SystemFiles:             []string{sysFile},
			ProjectInstructionFiles: []string{"project.md"},
			ModeDir:                 modeDir,
		},
	}
	m, err := Load(cfg, tmp)
	if err != nil {
		t.Fatal(err)
	}
	built := m.Build("code")
	sysIdx := strings.Index(built, "SYSTEM")
	promptIdx := strings.Index(built, "PROMPT")
	projIdx := strings.Index(built, "PROJECT")
	if sysIdx < 0 || promptIdx < 0 || projIdx < 0 {
		t.Fatalf("missing section in built = %q", built)
	}
	if sysIdx >= promptIdx || promptIdx >= projIdx {
		t.Fatalf("sections out of order in built = %q", built)
	}
	modes := m.Modes()
	if len(modes) != 1 || modes[0].Name != "code" {
		t.Fatalf("modes = %v", modes)
	}
	_, err = m.Mode("unknown")
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestManagerMissingSystemPrompt(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{
		Prompt: config.PromptConfig{
			SystemFiles:             []string{filepath.Join(tmp, "missing.md")},
			MissingSystemPrompt:     "ignore",
			ProjectInstructionFiles: []string{},
		},
	}
	m, err := Load(cfg, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if m.Build("") != "" {
		t.Fatalf("expected empty, got %q", m.Build(""))
	}
}

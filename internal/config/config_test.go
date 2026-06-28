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
	write("tools.json", `{
		"version":1,
		"tool_policy":{"builtin.read_file":"block"},
		"shell_policy":{"unknown":"confirm","allow":[{"cmd":"git","args":["status"],"match":"argv_prefix"}],"confirm":[],"block":[]},
		"plugins":{}
	}`)
	c, loaded, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 4 || c.Storage.Path != filepath.Join(dir, "state.db") || c.Profiles["x"].Model != "m" || c.MCP.Servers["docs"].URL != "http://mcp" || c.ToolPolicies["builtin.read_file"] != "block" {
		t.Fatalf("bad split config: %#v loaded=%v", c, loaded)
	}
	if c.ShellPolicy.Unknown != "confirm" {
		t.Fatalf("expected ShellPolicy.Unknown=confirm, got %q", c.ShellPolicy.Unknown)
	}
	if len(c.ShellPolicy.Allow) != 1 || c.ShellPolicy.Allow[0].Cmd != "git" {
		t.Fatalf("expected 1 allow rule with cmd=git, got %#v", c.ShellPolicy.Allow)
	}
	if len(c.ShellPolicy.Confirm) != 0 {
		t.Fatalf("expected 0 confirm rules, got %d", len(c.ShellPolicy.Confirm))
	}
	if len(c.ShellPolicy.Block) != 0 {
		t.Fatalf("expected 0 block rules, got %d", len(c.ShellPolicy.Block))
	}
}

func TestLoadCompactFromConfigJSON(t *testing.T) {
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
	write("config.json", `{"version":1,"defaultMode":"coder","defaultProfile":"x","state":{"database":"state.db"},"compact":{"level":"hard","context_tokens":8192,"trigger_turns":0,"prompt_file":"prompts/custom_compact.md","levels":{"hard":{"target_tokens":777}}}}`)
	write("llm.json", `{"version":1,"profiles":{"x":{"baseURL":"http://x/v1","model":"m"}}}`)
	write("mcp.json", `{"version":1,"servers":{}}`)
	write("tools.json", `{"version":1,"plugins":{}}`)
	c, _, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Compact.Enabled || c.Compact.Level != "hard" || c.Compact.ContextTokens != 8192 || c.Compact.TriggerTurns != 0 {
		t.Fatalf("compact config was not loaded from config.json: %#v", c.Compact)
	}
	if c.Compact.PromptFile != filepath.Join(dir, "prompts", "custom_compact.md") {
		t.Fatalf("compact prompt_file was not resolved relative to config dir: %q", c.Compact.PromptFile)
	}
	if c.Compact.Levels["hard"].TargetTokens != 777 || c.Compact.Levels["normal"].TargetTokens == 0 {
		t.Fatalf("compact levels were not loaded from config.json: %#v", c.Compact.Levels)
	}
}
func writeConfigSet(t *testing.T, dir, configJSON string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", configJSON)
	write("llm.json", `{"version":1,"profiles":{"x":{"baseURL":"http://x/v1","model":"m"}}}`)
	write("mcp.json", `{"version":1,"servers":{}}`)
	write("tools.json", `{"version":1,"plugins":{}}`)
}

func TestLoadRejectsUnknownCompactLevel(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	writeConfigSet(t, dir, `{"version":1,"defaultMode":"coder","defaultProfile":"x","state":{"database":"state.db"},"compact":{"level":"custom"}}`)
	_, _, err := Load(root, root, "")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "config.json")) || !strings.Contains(err.Error(), `compact.level "custom"`) {
		t.Fatalf("expected compact.level config error with path and level, got %v", err)
	}
}

func TestLoadRejectsUnknownCompactLevelKey(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	writeConfigSet(t, dir, `{"version":1,"defaultMode":"coder","defaultProfile":"x","state":{"database":"state.db"},"compact":{"levels":{"custom":{"target_tokens":99}}}}`)
	_, _, err := Load(root, root, "")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "config.json")) || !strings.Contains(err.Error(), `compact.levels key "custom"`) {
		t.Fatalf("expected compact.levels config error with path and key, got %v", err)
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

func TestLegacyPoliciesStillLoad(t *testing.T) {
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
	write("mcp.json", `{"version":1,"servers":{}}`)
	// Use legacy "policies" field only (no canonical tool_policy)
	write("tools.json", `{"version":1,"policies":{"builtin.read_file":"block","git.status":"allow"},"plugins":{}}`)
	c, _, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ToolPolicies["builtin.read_file"] != "block" {
		t.Fatalf("expected builtin.read_file=block, got %q", c.ToolPolicies["builtin.read_file"])
	}
	if c.ToolPolicies["git.status"] != "allow" {
		t.Fatalf("expected git.status=allow, got %q", c.ToolPolicies["git.status"])
	}
}

func TestLegacyShellPolicyStillLoads(t *testing.T) {
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
	write("mcp.json", `{"version":1,"servers":{}}`)
	// Use legacy "shellPolicy" field with unknown/block arrays (no canonical shell_policy)
	write("tools.json", `{
		"version":1,
		"shellPolicy":{"unknown":"confirm","allow":[],"confirm":[],"block":[{"cmd":"rm","args":["-rf"],"match":"argv_prefix"}]},
		"plugins":{}
	}`)
	c, _, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ShellPolicy.Unknown != "confirm" {
		t.Fatalf("expected ShellPolicy.Unknown=confirm, got %q", c.ShellPolicy.Unknown)
	}
	if len(c.ShellPolicy.Block) != 1 || c.ShellPolicy.Block[0].Cmd != "rm" {
		t.Fatalf("expected 1 block rule with cmd=rm, got %#v", c.ShellPolicy.Block)
	}
	if len(c.ShellPolicy.Allow) != 0 {
		t.Fatalf("expected 0 allow rules, got %d", len(c.ShellPolicy.Allow))
	}
}

func TestShellPolicyLoadsWithoutUnknown(t *testing.T) {
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
	write("llm.json", `{"version":1,"profiles":{"x":{"baseURL":"http://x/v1","model":"m"}}}`)
	write("mcp.json", `{"version":1,"servers":{}}`)
	write("tools.json", `{"version":1,"shell_policy":{"block":[{"cmd":"rm","match":"argv_prefix"}]},"plugins":{}}`)
	c, _, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ShellPolicy.Unknown != "confirm" {
		t.Fatalf("unknown default was not preserved: %q", c.ShellPolicy.Unknown)
	}
	if len(c.ShellPolicy.Block) != 1 || c.ShellPolicy.Block[0].Cmd != "rm" {
		t.Fatalf("block-only shell_policy was not loaded: %#v", c.ShellPolicy.Block)
	}
}
func TestCanonicalWinsOverLegacy(t *testing.T) {
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
	write("mcp.json", `{"version":1,"servers":{}}`)
	// Both canonical and legacy fields present; canonical must win
	write("tools.json", `{
		"version":1,
		"tool_policy":{"builtin.read_file":"block","git.status":"allow"},
		"policies":{"builtin.read_file":"allow","git.diff":"block"},
		"shell_policy":{"unknown":"allow","allow":[{"cmd":"git","args":["status"],"match":"argv_prefix"}],"confirm":[],"block":[]},
		"shellPolicy":{"unknown":"block","allow":[],"confirm":[],"block":[{"cmd":"cat","args":[],"match":"argv_prefix"}]},
		"plugins":{}
	}`)
	c, _, err := Load(root, root, "")
	if err != nil {
		t.Fatal(err)
	}

	// Canonical tool_policy wins over legacy policies for overlapping keys
	if c.ToolPolicies["builtin.read_file"] != "block" {
		t.Fatalf("expected builtin.read_file=block (canonical), got %q", c.ToolPolicies["builtin.read_file"])
	}
	if c.ToolPolicies["git.status"] != "allow" {
		t.Fatalf("expected git.status=allow (canonical), got %q", c.ToolPolicies["git.status"])
	}

	// Canonical shell_policy wins over legacy shellPolicy: Unknown and rule counts must reflect canonical
	if c.ShellPolicy.Unknown != "allow" {
		t.Fatalf("expected ShellPolicy.Unknown=allow (canonical), got %q", c.ShellPolicy.Unknown)
	}
	if len(c.ShellPolicy.Allow) != 1 || c.ShellPolicy.Allow[0].Cmd != "git" {
		t.Fatalf("expected 1 allow rule with cmd=git (canonical), got %#v", c.ShellPolicy.Allow)
	}
	if len(c.ShellPolicy.Block) != 0 {
		t.Fatalf("expected 0 block rules (canonical), got %d", len(c.ShellPolicy.Block))
	}
}

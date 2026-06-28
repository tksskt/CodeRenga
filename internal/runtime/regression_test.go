package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/llm"
	"github.com/tks/coderenga/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentFingerprintsUseExplicitConfigPath(t *testing.T) {
	root := t.TempDir()
	defaultDir := filepath.Join(root, "coderenga.d")
	explicitDir := filepath.Join(root, "alt")
	writeMinimalConfigSet(t, defaultDir, "default-model")
	writeMinimalConfigSet(t, explicitDir, "explicit-model")

	explicitConfig := filepath.Join(explicitDir, "config.json")
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, ConfigPath: explicitConfig, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	_, explicitFiles, err := config.Load(root, root, explicitConfig)
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := fingerprintConfigFiles(explicitFiles)
	gotConfig, _ := rt.currentFingerprints()
	if gotConfig != wantConfig {
		t.Fatalf("current config fingerprint did not use explicit config path: got %s want %s", gotConfig, wantConfig)
	}
}

func TestInitialUserMessageDoesNotTriggerAutoCompact(t *testing.T) {
	store, err := storage.Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s", "p", "coder", "local"); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{
		Store:     store,
		SessionID: "s",
		Profile:   "local",
		Config: config.Config{
			Profiles: map[string]config.Profile{"local": {MaxTokens: 1}},
			Compact:  config.CompactConfig{Enabled: true, TriggerContextRatio: 0.01, TriggerTurns: 1},
		},
	}
	if _, err := rt.addMessageNoCompact(ctx, "user", "current instruction that exceeds token limit"); err != nil {
		t.Fatal(err)
	}
	count, err := store.UncompactedCount(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("current instruction was compacted before processing; uncompacted count=%d", count)
	}
}

func TestConfigFingerprintIgnoresSecretValues(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "llm.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"local":{"model":"m1","apiKey":"secret-a"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	first := fingerprintConfigFiles([]string{path})
	if err := os.WriteFile(path, []byte(`{"profiles":{"local":{"model":"m1","apiKey":"secret-b"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	second := fingerprintConfigFiles([]string{path})
	if first != second {
		t.Fatal("secret value change should not change config fingerprint")
	}
	if err := os.WriteFile(path, []byte(`{"profiles":{"local":{"model":"m2","apiKey":"secret-b"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	third := fingerprintConfigFiles([]string{path})
	if second == third {
		t.Fatal("non-secret config change should change config fingerprint")
	}
}

func TestCurrentContextTokenLimitDoesNotUseOutputMaxTokensOrSummaryTarget(t *testing.T) {
	rt := &Runtime{
		Profile: "local",
		Config: config.Config{
			Profiles: map[string]config.Profile{"local": {MaxTokens: 8}},
			Compact:  config.CompactConfig{Levels: map[string]config.CompactLevel{"normal": {TargetTokens: 2048}}},
		},
	}
	if got := rt.currentContextTokenLimit(); got != 4096 {
		t.Fatalf("context token limit used output maxTokens or summary target: got %d", got)
	}
	rt.Config.Compact.ContextTokens = 8192
	if got := rt.currentContextTokenLimit(); got != 8192 {
		t.Fatalf("context token limit ignored explicit context_tokens: got %d", got)
	}
}

func TestInitialPluginDuplicateKeepsFirstRegistration(t *testing.T) {
	root := t.TempDir()
	writeMinimalConfigSet(t, filepath.Join(root, "coderenga.d"), "model")
	toolsJSON := filepath.Join(root, "coderenga.d", "tools.json")
	if err := os.WriteFile(toolsJSON, []byte(`{"version":1,"plugins":{"dupe":{"description":"first","command":"noop","policy":"confirm"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "coderenga.d", "plugins", "dupe")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "tool.json"), []byte(`{"name":"dupe","description":"second","command":"noop","policy":"confirm"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	tool, ok := rt.Registry.Info("plugin.dupe")
	if !ok {
		t.Fatal("plugin.dupe was not loaded")
	}
	if tool.Description() != "first" {
		t.Fatalf("initial duplicate plugin load overwrote first registration: %q", tool.Description())
	}
	if len(rt.Diagnostics) == 0 || !strings.Contains(rt.Diagnostics[0], "duplicate tool") {
		t.Fatalf("duplicate plugin load was not recorded in diagnostics: %#v", rt.Diagnostics)
	}
	var toolsOut bytes.Buffer
	rt.printTools(&toolsOut, "plugin.")
	if !strings.Contains(toolsOut.String(), "plugin.dupe") || !strings.Contains(toolsOut.String(), "reason=") {
		t.Fatalf("/tools did not expose duplicate reason: %q", toolsOut.String())
	}
	var infoOut bytes.Buffer
	if err := rt.toolCommand(context.Background(), []string{"/tool", "info", "plugin.dupe"}, &infoOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(infoOut.String(), "reason:") || !strings.Contains(infoOut.String(), "duplicate tool") {
		t.Fatalf("/tool info did not expose duplicate reason: %q", infoOut.String())
	}
	rt.loadPlugins(true)
	tool, ok = rt.Registry.Info("plugin.dupe")
	if !ok || tool.Description() != "first" {
		t.Fatalf("reload duplicate plugin load overwrote first registration: ok=%t desc=%q", ok, tool.Description())
	}
	if !strings.Contains(rt.ToolDiagnostics["plugin.dupe"], "duplicate tool in reload batch") {
		t.Fatalf("reload duplicate plugin was not diagnosed: %#v", rt.ToolDiagnostics)
	}
}

func TestPluginReloadRemovesStaleToolsAndClearsDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeMinimalConfigSet(t, filepath.Join(root, "coderenga.d"), "model")
	toolsJSON := filepath.Join(root, "coderenga.d", "tools.json")
	if err := os.WriteFile(toolsJSON, []byte(`{"version":1,"plugins":{"dupe":{"description":"first","command":"noop","policy":"confirm"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "coderenga.d", "plugins", "dupe")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "tool.json"), []byte(`{"name":"dupe","description":"second","command":"noop","policy":"confirm"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if !strings.Contains(rt.ToolDiagnostics["plugin.dupe"], "duplicate tool") {
		t.Fatalf("expected duplicate diagnostic, got %#v", rt.ToolDiagnostics)
	}
	if err := os.RemoveAll(pluginDir); err != nil {
		t.Fatal(err)
	}
	rt.loadPlugins(true)
	if reason := rt.ToolDiagnostics["plugin.dupe"]; reason != "" {
		t.Fatalf("stale plugin diagnostic remained after duplicate was resolved: %q", reason)
	}
	if tool, ok := rt.Registry.Info("plugin.dupe"); !ok || tool.Description() != "first" {
		t.Fatalf("plugin.dupe was not reloaded after duplicate resolution: ok=%t tool=%#v", ok, tool)
	}
	if err := os.WriteFile(toolsJSON, []byte(`{"version":1,"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.loadPlugins(true)
	if _, ok := rt.Registry.Info("plugin.dupe"); ok {
		t.Fatalf("stale plugin.dupe remained after manifest removal; names=%v", rt.Registry.Names())
	}
	if reason := rt.ToolDiagnostics["plugin.dupe"]; reason != "" {
		t.Fatalf("stale plugin diagnostic remained after manifest removal: %q", reason)
	}
}

func TestAutoCompactUsesConfiguredLevel(t *testing.T) {
	root := t.TempDir()
	promptPath := filepath.Join(root, "compact.md")
	if err := os.WriteFile(promptPath, []byte("compact base prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	var systemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Messages) > 0 {
			systemPrompt = body.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"summary"}}]}`))
	}))
	defer server.Close()
	store, err := storage.Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s", root, "coder", "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, "s", "user", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, "s", "assistant", "world"); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{
		Store:     store,
		SessionID: "s",
		Profile:   "local",
		LLM:       llm.New(),
		Config: config.Config{
			Profiles: map[string]config.Profile{"local": {BaseURL: server.URL, Model: "m"}},
			Compact: config.CompactConfig{
				Enabled:      true,
				PromptFile:   promptPath,
				Profile:      "local",
				Level:        "hard",
				TriggerTurns: 1,
				Levels:       map[string]config.CompactLevel{"normal": {TargetTokens: 333}, "hard": {TargetTokens: 777}},
			},
		},
	}
	if err := rt.maybeAutoCompact(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemPrompt, "777 tokens") || strings.Contains(systemPrompt, "333 tokens") {
		t.Fatalf("auto compact did not use configured level: %q", systemPrompt)
	}
}
func TestCompactLevelTargetTokensAreSentToSummaryPrompt(t *testing.T) {
	root := t.TempDir()
	promptPath := filepath.Join(root, "compact.md")
	if err := os.WriteFile(promptPath, []byte("compact base prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	var systemPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Messages) > 0 {
			systemPrompt = body.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"summary"}}]}`))
	}))
	defer server.Close()
	store, err := storage.Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateSession(ctx, "s", root, "coder", "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, "s", "user", "hello"); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{
		Store:     store,
		SessionID: "s",
		Profile:   "local",
		LLM:       llm.New(),
		Config: config.Config{
			Profiles: map[string]config.Profile{"local": {BaseURL: server.URL, Model: "m"}},
			Compact: config.CompactConfig{
				PromptFile: promptPath,
				Profile:    "local",
				Levels:     map[string]config.CompactLevel{"light": {TargetTokens: 1200}},
			},
		},
	}
	if err := rt.compact(ctx, "light", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(systemPrompt, "1200 tokens") {
		t.Fatalf("compact target tokens were not sent to summary prompt: %q", systemPrompt)
	}
}

func TestCompactRejectsUnknownLevel(t *testing.T) {
	rt := &Runtime{}
	err := rt.compact(context.Background(), "custom", &bytes.Buffer{})
	if err == nil || err.Error() != "invalid compact level" {
		t.Fatalf("err=%v", err)
	}
}
func writeMinimalConfigSet(t *testing.T, dir, model string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"test","state":{"database":"coderenga.db"}}`,
		"llm.json":           `{"version":1,"profiles":{"test":{"baseURL":"http://127.0.0.1.invalid","model":"` + model + `"}}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: allow\nshell: policy\nmcp: true\n---\nmode",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

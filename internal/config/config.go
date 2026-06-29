package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrNotInitialized = errors.New("configuration is not initialized")
var ErrOldFormat = errors.New("old configuration format detected")

type Config struct {
	DefaultProfile string
	DefaultMode    string
	Profiles       map[string]Profile
	Prompt         PromptConfig
	Storage        StorageConfig
	Compact        CompactConfig
	ShellPolicy    ShellPolicy
	MCP            MCPConfig
	ToolPolicies   map[string]string
}
type Profile struct {
	BaseURL           string         `json:"baseURL"`
	APIKey            string         `json:"apiKey"`
	Model             string         `json:"model"`
	Temperature       float64        `json:"temperature"`
	MaxTokens         int            `json:"maxTokens"`
	ToolProtocol      string         `json:"toolProtocol,omitempty"`
	ToolChoice        string         `json:"toolChoice,omitempty"`
	ParallelToolCalls *bool          `json:"parallelToolCalls,omitempty"`
	ExtraBody         map[string]any `json:"extraBody,omitempty"`
}
type PromptConfig struct {
	SystemFiles             []string
	ProjectInstructionFiles []string
	ModeDir                 string
	GlobalModeDir           string
	MissingSystemPrompt     string
}
type StorageConfig struct {
	Enabled   bool
	Driver    string
	Path      string
	NoPersist bool
}
type CompactLevel struct {
	TargetTokens int `json:"target_tokens"`
}
type CompactConfig struct {
	Enabled             bool                    `json:"enabled"`
	ContextTokens       int                     `json:"context_tokens"`
	Level               string                  `json:"level"`
	TriggerContextRatio float64                 `json:"trigger_context_ratio"`
	TriggerTurns        int                     `json:"trigger_turns"`
	KeepRecentTurns     int                     `json:"keep_recent_turns"`
	PromptFile          string                  `json:"prompt_file"`
	Profile             string                  `json:"profile"`
	Levels              map[string]CompactLevel `json:"levels"`
}
type ShellRule struct {
	Cmd     string   `json:"cmd"`
	Args    []string `json:"args"`
	Pattern string   `json:"pattern"`
	Match   string   `json:"match"`
}
type ShellPolicy struct {
	Unknown string      `json:"unknown"`
	Allow   []ShellRule `json:"allow"`
	Confirm []ShellRule `json:"confirm"`
	Block   []ShellRule `json:"block"`
}
type MCPConfig struct {
	Enabled bool
	Servers map[string]MCPServer
}
type MCPServer struct {
	Transport string   `json:"transport"`
	Command   string   `json:"command"`
	URL       string   `json:"url"`
	Args      []string `json:"args"`
	Enabled   bool     `json:"enabled"`
}

func Defaults() Config {
	return Config{DefaultProfile: "local", DefaultMode: "coder", Profiles: map[string]Profile{}, Prompt: PromptConfig{ProjectInstructionFiles: []string{".coderenga/instructions.md", "CODERENGA.md", "AGENTS.md"}, MissingSystemPrompt: "error"}, Storage: StorageConfig{Enabled: true, Driver: "sqlite"}, Compact: CompactConfig{Enabled: true, ContextTokens: 4096, Level: "normal", TriggerContextRatio: .75, TriggerTurns: 30, KeepRecentTurns: 10, Profile: "local", Levels: map[string]CompactLevel{"light": {6000}, "normal": {3000}, "hard": {1200}}}, ShellPolicy: ShellPolicy{Unknown: "confirm"}, MCP: MCPConfig{Enabled: true, Servers: map[string]MCPServer{}}, ToolPolicies: map[string]string{}}
}

func Load(binaryDir, cwd, explicit string) (Config, []string, error) {
	c := Defaults()
	configPath := explicit
	if configPath == "" {
		configPath = filepath.Join(binaryDir, "coderenga.d", "config.json")
	}
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return c, nil, err
	}
	baseDir := filepath.Dir(configPath)
	llmPath := filepath.Join(baseDir, "llm.json")
	if _, err := os.Stat(configPath); err != nil {
		if explicit != "" {
			return c, nil, fmt.Errorf("failed to load config file: %s\n%w", configPath, err)
		}
		return c, nil, ErrNotInitialized
	}
	var app struct {
		Version        int    `json:"version"`
		DefaultMode    string `json:"defaultMode"`
		DefaultProfile string `json:"defaultProfile"`
		State          struct {
			Database string `json:"database"`
		} `json:"state"`
	}
	raw, err := readJSON(configPath, &app)
	if err != nil {
		return c, nil, fmt.Errorf("failed to load config file: %s\n%w", configPath, err)
	}
	for _, key := range []string{"profiles", "mcp", "prompt", "shell_policy", "storage"} {
		if _, ok := raw[key]; ok {
			return c, nil, ErrOldFormat
		}
	}
	if _, err := os.Stat(llmPath); err != nil {
		if explicit != "" {
			return c, nil, fmt.Errorf("failed to load config file: %s\n%w", llmPath, err)
		}
		return c, nil, ErrNotInitialized
	}
	if app.DefaultMode != "" {
		c.DefaultMode = app.DefaultMode
	}
	if app.DefaultProfile != "" {
		c.DefaultProfile = app.DefaultProfile
	}
	if compactRaw, ok := raw["compact"]; ok {
		var compactErr error
		c.Compact, compactErr = mergeCompactConfigJSON(c.Compact, compactRaw)
		if compactErr != nil {
			return c, nil, fmt.Errorf("failed to load config file: %s\n%w", configPath, compactErr)
		}
	}
	database := app.State.Database
	if database == "" {
		database = "coderenga.db"
	}
	c.Storage.Path = filepath.Join(baseDir, database)
	c.Prompt.SystemFiles = []string{filepath.Join(baseDir, "prompts", "default.md")}
	c.Prompt.GlobalModeDir = filepath.Join(baseDir, "modes")
	c.Prompt.ModeDir = filepath.Join(cwd, ".coderenga", "modes")
	if err := validateCompactConfig(c.Compact); err != nil {
		return c, nil, fmt.Errorf("failed to load config file: %s\n%w", configPath, err)
	}
	if c.Compact.PromptFile == "" {
		c.Compact.PromptFile = filepath.Join(baseDir, "prompts", "compact.md")
	} else if !filepath.IsAbs(c.Compact.PromptFile) {
		c.Compact.PromptFile = filepath.Join(baseDir, c.Compact.PromptFile)
	}

	var llm struct {
		Version        int                `json:"version"`
		DefaultProfile string             `json:"defaultProfile"`
		Profiles       map[string]Profile `json:"profiles"`
	}
	if _, err = readJSON(llmPath, &llm); err != nil {
		return c, nil, fmt.Errorf("failed to load config file: %s\n%w", llmPath, err)
	}
	if llm.DefaultProfile != "" && app.DefaultProfile == "" {
		c.DefaultProfile = llm.DefaultProfile
	}
	c.Profiles = llm.Profiles
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}

	mcpPath := filepath.Join(baseDir, "mcp.json")
	var mcpFile struct {
		Version int                  `json:"version"`
		Servers map[string]MCPServer `json:"servers"`
	}
	if _, err = readOptionalJSON(mcpPath, &mcpFile); err != nil {
		return c, nil, fmt.Errorf("failed to load config file: %s\n%w", mcpPath, err)
	}
	if mcpFile.Servers != nil {
		c.MCP.Servers = mcpFile.Servers
	}
	toolsPath := filepath.Join(baseDir, "tools.json")
	var toolsFile struct {
		// Canonical fields
		ToolPolicy  map[string]string `json:"tool_policy"`
		ShellPolicy ShellPolicy       `json:"shell_policy"`
		// Legacy fields (backward compatibility)
		Policies          map[string]string `json:"policies"`
		LegacyShellPolicy ShellPolicy       `json:"shellPolicy"`
	}
	toolsRaw, err := readOptionalJSON(toolsPath, &toolsFile)
	if err != nil {
		return c, nil, fmt.Errorf("failed to load config file: %s\n%w", toolsPath, err)
	}
	// Load ToolPolicies: canonical tool_policy wins over legacy policies
	if toolsFile.ToolPolicy != nil {
		c.ToolPolicies = toolsFile.ToolPolicy
	} else if toolsFile.Policies != nil {
		c.ToolPolicies = toolsFile.Policies
	}
	// Load ShellPolicy: canonical shell_policy wins over legacy shellPolicy.
	// Partial shell_policy keeps defaults, so unknown defaults to confirm.
	if _, ok := toolsRaw["shell_policy"]; ok {
		c.ShellPolicy = mergeShellPolicy(c.ShellPolicy, toolsFile.ShellPolicy)
	} else if _, ok := toolsRaw["shellPolicy"]; ok {
		c.ShellPolicy = mergeShellPolicy(c.ShellPolicy, toolsFile.LegacyShellPolicy)
	}
	return c, []string{configPath, llmPath, mcpPath, toolsPath}, nil
}

func validateCompactConfig(compact CompactConfig) error {
	if !validCompactLevel(compact.Level) {
		return fmt.Errorf("invalid compact.level %q", compact.Level)
	}
	for name := range compact.Levels {
		if !validCompactLevel(name) {
			return fmt.Errorf("invalid compact.levels key %q", name)
		}
	}
	return nil
}

func validCompactLevel(level string) bool {
	switch level {
	case "light", "normal", "hard":
		return true
	default:
		return false
	}
}

func mergeCompactConfigJSON(base CompactConfig, raw json.RawMessage) (CompactConfig, error) {
	var override CompactConfig
	if err := json.Unmarshal(raw, &override); err != nil {
		return base, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return base, err
	}
	if _, ok := fields["enabled"]; ok {
		base.Enabled = override.Enabled
	}
	if _, ok := fields["context_tokens"]; ok {
		base.ContextTokens = override.ContextTokens
	}
	if _, ok := fields["level"]; ok {
		base.Level = override.Level
	}
	if _, ok := fields["trigger_context_ratio"]; ok {
		base.TriggerContextRatio = override.TriggerContextRatio
	}
	if _, ok := fields["trigger_turns"]; ok {
		base.TriggerTurns = override.TriggerTurns
	}
	if _, ok := fields["keep_recent_turns"]; ok {
		base.KeepRecentTurns = override.KeepRecentTurns
	}
	if _, ok := fields["prompt_file"]; ok {
		base.PromptFile = override.PromptFile
	}
	if _, ok := fields["profile"]; ok {
		base.Profile = override.Profile
	}
	if _, ok := fields["levels"]; ok {
		if base.Levels == nil {
			base.Levels = map[string]CompactLevel{}
		}
		for name, level := range override.Levels {
			base.Levels[name] = level
		}
	}
	return base, nil
}

func mergeShellPolicy(base, override ShellPolicy) ShellPolicy {
	if override.Unknown != "" {
		base.Unknown = override.Unknown
	}
	if override.Allow != nil {
		base.Allow = override.Allow
	}
	if override.Confirm != nil {
		base.Confirm = override.Confirm
	}
	if override.Block != nil {
		base.Block = override.Block
	}
	return base
}
func readJSON(path string, dst any) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, dst); err != nil {
		return nil, err
	}
	return raw, nil
}
func readOptionalJSON(path string, dst any) (map[string]json.RawMessage, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return readJSON(path, dst)
}

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
	BaseURL     string  `json:"baseURL"`
	APIKey      string  `json:"apiKey"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens"`
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
type CompactLevel struct{ TargetTokens int }
type CompactConfig struct {
	Enabled             bool
	TriggerContextRatio float64
	TriggerTurns        int
	KeepRecentTurns     int
	PromptFile          string
	Profile             string
	Levels              map[string]CompactLevel
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
	return Config{DefaultProfile: "local", DefaultMode: "coder", Profiles: map[string]Profile{}, Prompt: PromptConfig{ProjectInstructionFiles: []string{".coderenga/instructions.md", "CODERENGA.md", "AGENTS.md"}, MissingSystemPrompt: "error"}, Storage: StorageConfig{Enabled: true, Driver: "sqlite"}, Compact: CompactConfig{Enabled: true, TriggerContextRatio: .75, TriggerTurns: 30, KeepRecentTurns: 10, Profile: "local", Levels: map[string]CompactLevel{"light": {6000}, "normal": {3000}, "hard": {1200}}}, ShellPolicy: ShellPolicy{Unknown: "confirm"}, MCP: MCPConfig{Enabled: true, Servers: map[string]MCPServer{}}, ToolPolicies: map[string]string{}}
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
	for _, key := range []string{"profiles", "mcp", "prompt", "shell_policy", "storage", "compact"} {
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
	database := app.State.Database
	if database == "" {
		database = "coderenga.db"
	}
	c.Storage.Path = filepath.Join(baseDir, database)
	c.Prompt.SystemFiles = []string{filepath.Join(baseDir, "prompts", "default.md")}
	c.Prompt.GlobalModeDir = filepath.Join(baseDir, "modes")
	c.Prompt.ModeDir = filepath.Join(cwd, ".coderenga", "modes")
	c.Compact.PromptFile = filepath.Join(baseDir, "prompts", "compact.md")

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
		Version     int               `json:"version"`
		Policies    map[string]string `json:"policies"`
		ShellPolicy ShellPolicy       `json:"shellPolicy"`
	}
	if _, err = readOptionalJSON(toolsPath, &toolsFile); err != nil {
		return c, nil, fmt.Errorf("failed to load config file: %s\n%w", toolsPath, err)
	}
	if toolsFile.Policies != nil {
		c.ToolPolicies = toolsFile.Policies
	}
	if toolsFile.ShellPolicy.Unknown != "" {
		c.ShellPolicy = toolsFile.ShellPolicy
	}
	return c, []string{configPath, llmPath, mcpPath, toolsPath}, nil
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

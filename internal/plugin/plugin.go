package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/tools"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type EnvPolicy struct {
	Mode  string   `json:"mode"`
	Allow []string `json:"allow"`
}
type Sandbox struct {
	Required   bool      `json:"required"`
	Filesystem string    `json:"filesystem"`
	Network    string    `json:"network"`
	Env        EnvPolicy `json:"env"`
}
type Manifest struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Command        string          `json:"command"`
	InputMode      string          `json:"input_mode"`
	Policy         string          `json:"policy"`
	ArgsSchema     json.RawMessage `json:"args_schema"`
	TimeoutSec     int             `json:"timeout_sec"`
	AvailableModes []string        `json:"available_modes"`
	Sandbox        Sandbox         `json:"sandbox"`
}
type Tool struct {
	Manifest             Manifest
	BaseDir              string
	HardSandboxAvailable bool
}

func (t Tool) Name() string {
	n := t.Manifest.Name
	if !strings.HasPrefix(n, "plugin.") {
		n = "plugin." + n
	}
	return n
}
func (t Tool) Description() string { return t.Manifest.Description }
func (t Tool) Policy() tools.Level {
	level := tools.ParseLevel(t.Manifest.Policy)
	if !t.Manifest.Sandbox.Required && level == tools.Allow {
		return tools.Confirm
	}
	return level
}
func (t Tool) Execute(ctx context.Context, r tools.Request) (tools.Result, error) {
	if t.Manifest.Sandbox.Required && !t.HardSandboxAvailable {
		return tools.Result{OK: false, Error: "required hard sandbox backend is unavailable"}, nil
	}
	if t.Manifest.InputMode != "" && t.Manifest.InputMode != "json_stdin" {
		return tools.Result{}, fmt.Errorf("unsupported plugin input_mode %q", t.Manifest.InputMode)
	}
	command := t.Manifest.Command
	if !filepath.IsAbs(command) {
		command = filepath.Join(t.BaseDir, command)
	}
	timeout := time.Duration(t.Manifest.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command)
	cmd.Dir = r.Context.CWD
	cmd.Env = allowedEnv(t.Manifest.Sandbox.Env.Allow, r.Context)
	args := map[string]any{}
	for k, v := range r.Arguments {
		if !strings.HasPrefix(k, "_coderenga_") {
			args[k] = v
		}
	}
	payload, _ := json.Marshal(map[string]any{"arguments": args, "context": r.Context})
	cmd.Stdin = bytes.NewReader(payload)
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	if err := cmd.Run(); err != nil {
		return tools.Result{OK: false, Error: err.Error(), Metadata: map[string]any{"stderr": clip(errout.String())}}, nil
	}
	if out.Len() > 4<<20 {
		return tools.Result{}, fmt.Errorf("plugin output exceeds limit")
	}
	var result tools.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return tools.Result{}, fmt.Errorf("plugin returned invalid JSON: %w", err)
	}
	return result, nil
}
func clip(s string) string {
	if len(s) > 4096 {
		return s[:4096]
	}
	return s
}
func allowedEnv(keys []string, c tools.Context) []string {
	values := map[string]string{"CR_CWD": c.CWD, "CR_SESSION_ID": c.SessionID}
	var out []string
	for _, k := range keys {
		if v, ok := values[k]; ok {
			out = append(out, k+"="+v)
		} else if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}
func LoadToolsJSON(path string) ([]Tool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v struct {
		Plugins map[string]Manifest `json:"plugins"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(v.Plugins))
	for name, m := range v.Plugins {
		if m.Name == "" {
			m.Name = name
		}
		out = append(out, Tool{Manifest: m, BaseDir: filepath.Dir(path)})
	}
	return out, nil
}
func LoadDirectory(dir string) ([]Tool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Tool
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p := filepath.Join(dir, entry.Name(), "tool.json")
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(b, &m) == nil {
			out = append(out, Tool{Manifest: m, BaseDir: filepath.Dir(p)})
		}
	}
	return out, nil
}

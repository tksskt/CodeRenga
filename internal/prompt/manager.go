package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tks/coderenga/internal/config"
)

type Mode struct {
	Name, Description, Profile, Prompt string
	Write, Shell                       string
	MCP, PlanFirst                     bool
}
type Manager struct {
	cfg     config.Config
	cwd     string
	files   []string
	system  string
	project string
	modes   map[string]Mode
}

func Load(cfg config.Config, cwd string) (*Manager, error) {
	m := &Manager{cfg: cfg, cwd: cwd}
	if err := m.Reload(); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *Manager) Reload() error {
	m.files = nil
	m.system = ""
	m.project = ""
	m.modes = map[string]Mode{}
	for _, p := range m.cfg.Prompt.SystemFiles {
		b, e := os.ReadFile(p)
		if e != nil {
			if os.IsNotExist(e) && m.cfg.Prompt.MissingSystemPrompt != "error" {
				continue
			}
			return e
		}
		m.files = append(m.files, p)
		m.system += string(b) + "\n"
	}
	for _, p := range m.cfg.Prompt.ProjectInstructionFiles {
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.cwd, p)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return e
		}
		m.files = append(m.files, p)
		m.project += string(b) + "\n"
	}
	for _, d := range []string{m.cfg.Prompt.GlobalModeDir, m.cfg.Prompt.ModeDir} {
		if d == "" {
			continue
		}
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
				continue
			}
			p := filepath.Join(d, e.Name())
			b, er := os.ReadFile(p)
			if er != nil {
				return er
			}
			mode := parseMode(string(b), strings.TrimSuffix(e.Name(), ".md"))
			m.modes[mode.Name] = mode
			m.files = append(m.files, p)
		}
	}
	return nil
}
func parseMode(s, fallback string) Mode {
	m := Mode{Name: fallback, Write: "false", Shell: "allow_readonly"}
	if !strings.HasPrefix(s, "---\n") {
		m.Prompt = s
		return m
	}
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		m.Prompt = s
		return m
	}
	for _, line := range strings.Split(parts[1], "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		k, v := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		switch k {
		case "name":
			m.Name = v
		case "description":
			m.Description = v
		case "profile":
			m.Profile = v
		case "write":
			m.Write = v
		case "shell":
			m.Shell = v
		case "mcp":
			m.MCP = v == "true"
		case "plan_first":
			m.PlanFirst = v == "true"
		}
	}
	m.Prompt = strings.TrimSpace(parts[2])
	return m
}
func (m *Manager) Build(mode string) string {
	v := m.modes[mode]
	return strings.TrimSpace(m.system + "\n" + v.Prompt + "\n" + m.project)
}
func (m *Manager) Files() []string { return append([]string(nil), m.files...) }
func (m *Manager) Modes() []Mode {
	out := make([]Mode, 0, len(m.modes))
	for _, v := range m.modes {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (m *Manager) Mode(name string) (Mode, error) {
	v, ok := m.modes[name]
	if !ok {
		return Mode{}, fmt.Errorf("unknown mode %q", name)
	}
	return v, nil
}

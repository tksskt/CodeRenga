package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/llm"
	"github.com/tks/coderenga/internal/mcp"
	"github.com/tks/coderenga/internal/plugin"
	"github.com/tks/coderenga/internal/prompt"
	"github.com/tks/coderenga/internal/storage"
	"github.com/tks/coderenga/internal/tools"
	"github.com/tks/coderenga/internal/tools/builtin"
	gittool "github.com/tks/coderenga/internal/tools/git"
	shelltool "github.com/tks/coderenga/internal/tools/shell"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Options struct {
	BinaryDir, CWD, ConfigPath, StateDir, Mode, Profile, Model string
	NoPersist, DryRun                                          bool
}
type Runtime struct {
	Config                                          config.Config
	Prompts                                         *prompt.Manager
	Store                                           *storage.Store
	Registry                                        *tools.Registry
	Executor                                        *tools.Executor
	LLM                                             *llm.Client
	CWD, BinaryDir, Mode, Profile, Model, SessionID string
	DryRun                                          bool
	MCP                                             map[string]mcp.Client
	Approve                                         tools.Approver
}

func New(ctx context.Context, o Options) (*Runtime, error) {
	cfg, _, e := config.Load(o.BinaryDir, o.CWD, o.ConfigPath)
	if e != nil {
		return nil, e
	}
	if o.StateDir != "" {
		p, e := filepath.Abs(o.StateDir)
		if e != nil {
			return nil, e
		}
		cfg.Storage.Enabled = true
		cfg.Storage.Path = filepath.Join(p, "coderenga.db")
	}
	pm, e := prompt.Load(cfg, o.CWD)
	if e != nil {
		return nil, e
	}
	mode := first(o.Mode, cfg.DefaultMode)
	profile := first(o.Profile, cfg.DefaultProfile)
	selectedProfile, ok := cfg.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("profile %q was not found in coderenga.d/llm.json.\nCheck the profile name, or edit coderenga.d/llm.json", profile)
	}
	model := o.Model
	if model == "" {
		model = selectedProfile.Model
	}
	dbPath := cfg.Storage.Path
	if !cfg.Storage.Enabled {
		dbPath = ""
	}
	store, e := storage.Open(dbPath, o.NoPersist || cfg.Storage.NoPersist || !cfg.Storage.Enabled)
	if e != nil {
		return nil, e
	}
	rt := &Runtime{Config: cfg, Prompts: pm, Store: store, Registry: tools.NewRegistry(), LLM: llm.New(), CWD: o.CWD, BinaryDir: o.BinaryDir, Mode: mode, Profile: profile, Model: model, SessionID: fmt.Sprintf("%d", time.Now().UnixNano()), DryRun: o.DryRun, MCP: map[string]mcp.Client{}}
	if e = builtin.Register(rt.Registry); e != nil {
		return nil, e
	}
	if e = gittool.Register(rt.Registry); e != nil {
		return nil, e
	}
	if e = rt.Registry.Register(&shelltool.Runner{PolicyConfig: cfg.ShellPolicy, Store: store}); e != nil {
		return nil, e
	}
	rt.loadPlugins()
	rt.loadMCP(ctx)
	rt.Executor = &tools.Executor{Registry: rt.Registry, Store: store, PolicyDecision: func(name string) tools.Level {
		value, configured := cfg.ToolPolicies[name]
		if !configured {
			return tools.Allow
		}
		return tools.ParseLevel(value)
	}, Approver: func(n string, a map[string]any) bool {
		if rt.Approve == nil {
			return false
		}
		return rt.Approve(n, a)
	}, ModeDecision: rt.modeDecision}
	if e = store.CreateSession(ctx, rt.SessionID, o.CWD, mode, profile); e != nil {
		return nil, e
	}
	return rt, nil
}
func first(v, d string) string {
	if v != "" {
		return v
	}
	return d
}
func (rt *Runtime) Close() {
	for _, c := range rt.MCP {
		_ = c.Close()
	}
	_ = rt.Store.Close()
}
func (rt *Runtime) modeDecision(mode, name string) tools.Level {
	m, e := rt.Prompts.Mode(mode)
	if e != nil {
		return tools.Unknown
	}
	if strings.HasPrefix(name, "builtin.write") || name == "builtin.apply_patch" {
		if m.Write == "false" {
			return tools.Block
		}
		return tools.Confirm
	}
	if name == "shell.run" && m.Shell == "allow_readonly" {
		return tools.Confirm
	}
	if strings.HasPrefix(name, "mcp.") && !m.MCP {
		return tools.Block
	}
	return tools.Allow
}
func (rt *Runtime) loadPlugins() {
	items, _ := plugin.LoadToolsJSON(filepath.Join(rt.BinaryDir, "coderenga.d", "tools.json"))
	more, _ := plugin.LoadDirectory(filepath.Join(rt.BinaryDir, "coderenga.d", "plugins"))
	items = append(items, more...)
	for _, t := range items {
		_ = rt.Registry.Replace(t)
	}
}
func (rt *Runtime) loadMCP(ctx context.Context) {
	if !rt.Config.MCP.Enabled {
		return
	}
	for name, cfg := range rt.Config.MCP.Servers {
		if !cfg.Enabled {
			continue
		}
		c, e := mcp.Connect(ctx, name, cfg)
		if e != nil {
			continue
		}
		if e = mcp.Initialize(ctx, c); e != nil {
			_ = c.Close()
			continue
		}
		rt.MCP[name] = c
		infos, e := mcp.Discover(ctx, c)
		if e != nil {
			continue
		}
		for _, info := range infos {
			_ = rt.registerMCPTool(ctx, name, info, c)
		}
	}
}
func (rt *Runtime) RunInstruction(ctx context.Context, instruction string, out io.Writer) error {
	if _, err := rt.addMessage(ctx, "user", instruction); err != nil {
		return err
	}
	lastSignature := ""
	lastResults := map[string]string{}
	var callHistory []string
	var dryRunSkipped []tools.Request
	for turn := 0; turn < 8; turn++ {
		msgs, err := rt.context(ctx)
		if err != nil {
			return err
		}
		profile, ok := rt.Config.Profiles[rt.Profile]
		if !ok {
			return fmt.Errorf("unknown profile %q", rt.Profile)
		}
		profile.Model = rt.Model
		answer, err := rt.LLM.Chat(ctx, profile, msgs, true, nil)
		if err != nil {
			return err
		}
		calls, err := tools.ParseCalls(answer)
		if err != nil {
			return err
		}
		if len(calls) > 0 && isSimpleConversation(instruction) {
			final := casualResponse(instruction)
			if _, err = rt.addMessage(ctx, "assistant", final); err != nil {
				return err
			}
			fmt.Fprintln(out, final)
			return nil
		}
		if len(calls) == 0 {
			final := answer
			if len(dryRunSkipped) > 0 {
				final = dryRunFinal(dryRunSkipped)
			}
			if _, err = rt.addMessage(ctx, "assistant", final); err != nil {
				return err
			}
			fmt.Fprintln(out, final)
			return nil
		}
		if _, err = rt.addMessage(ctx, "assistant", answer); err != nil {
			return err
		}
		for _, call := range calls {
			signature := toolCallSignature(call)
			if signature == lastSignature {
				return fmt.Errorf("repeated tool call detected: %s was requested again immediately after its result; previous result: %s", toolCallSummary(call), lastResults[signature])
			}
			lastSignature = signature
			callHistory = append(callHistory, toolCallSummary(call))
			call.Context = tools.Context{CWD: rt.CWD, Mode: rt.Mode, SessionID: rt.SessionID, DryRun: rt.DryRun}
			res, err := rt.Executor.Execute(ctx, call)
			if err != nil {
				return err
			}
			lastResults[signature] = toolResultSummary(res)
			body, _ := json.Marshal(res)
			toolResult := fmt.Sprintf("Tool result for %s:\n%s", call.Name, body)
			if _, err = rt.addMessage(ctx, "user", toolResult); err != nil {
				return err
			}
			if rt.DryRun && isSideEffectTool(call.Name) {
				dryRunSkipped = append(dryRunSkipped, call)
				fmt.Fprintf(out, "[dry-run] %s\n", call.Name)
				printToolArguments(out, call.Arguments)
			}
		}
	}
	return fmt.Errorf("tool loop exceeded 8 turns; calls: %s", strings.Join(callHistory, " -> "))
}

func printToolArguments(out io.Writer, arguments map[string]any) {
	keys := make([]string, 0, len(arguments))
	for key := range arguments {
		if !strings.HasPrefix(key, "_coderenga_") {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "path" {
			return true
		}
		if keys[j] == "path" {
			return false
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		value := arguments[key]
		if key == "content" || key == "patch" {
			fmt.Fprintf(out, "%s:\n%v\n", key, value)
		} else {
			fmt.Fprintf(out, "%s: %v\n", key, value)
		}
	}
}

func toolCallSignature(call tools.Request) string {
	body, _ := json.Marshal(struct {
		Name      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
	}{call.Name, call.Arguments})
	return string(body)
}

func toolCallSummary(call tools.Request) string {
	body, _ := json.Marshal(call.Arguments)
	return call.Name + " " + limitText(string(body), 256)
}

func toolResultSummary(result tools.Result) string {
	if result.Error != "" {
		return limitText(result.Error, 256)
	}
	return limitText(result.Content, 256)
}

func limitText(value string, limit int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if len(value) > limit {
		return value[:limit] + "..."
	}
	return value
}

func dryRunFinal(calls []tools.Request) string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	return fmt.Sprintf("dry-run: %s was not executed. The planned operation was displayed above; no file was written and no command was run because --dry-run is enabled.", strings.Join(names, ", "))
}

func isSimpleConversation(instruction string) bool {
	value := strings.ToLower(strings.TrimSpace(instruction))
	value = strings.Trim(value, " !?.,。！？")
	switch value {
	case "hello", "hi", "hey", "こんにちは", "こんばんは", "おはよう", "thanks", "thank you", "ありがとう":
		return true
	default:
		return false
	}
}

func casualResponse(instruction string) string {
	value := strings.ToLower(strings.TrimSpace(instruction))
	if strings.Contains(value, "こんにちは") || strings.Contains(value, "こんばんは") || strings.Contains(value, "おはよう") || strings.Contains(value, "ありがとう") {
		return "こんにちは。コーディング作業について、何をお手伝いしましょうか？"
	}
	return "Hello! How can I help with your coding task?"
}

func isSideEffectTool(name string) bool {
	return name == "builtin.write_file" || name == "builtin.apply_patch" || name == "shell.run" || strings.HasPrefix(name, "plugin.") || strings.HasPrefix(name, "mcp.")
}
func (rt *Runtime) context(ctx context.Context) ([]llm.Message, error) {
	out := []llm.Message{{Role: "system", Content: rt.systemPrompt()}}
	sum, e := rt.Store.ActiveSummary(ctx, rt.SessionID)
	if e != nil {
		return nil, e
	}
	if sum != "" {
		out = append(out, llm.Message{Role: "system", Content: "Active summary:\n" + sum})
	}
	recent, e := rt.Store.RecentMessages(ctx, rt.SessionID, max(10, rt.Config.Compact.KeepRecentTurns))
	if e != nil {
		return nil, e
	}
	for _, m := range recent {
		out = append(out, llm.Message{Role: m.Role, Content: m.Content})
	}
	return out, nil
}
func (rt *Runtime) Handle(ctx context.Context, line string, out io.Writer) (bool, error) {
	p := strings.Fields(line)
	if len(p) == 0 {
		return false, nil
	}
	switch p[0] {
	case "/exit", "/quit":
		return true, nil
	case "/help":
		fmt.Fprintln(out, "/mode /modes /profile /model /status /prompts /reload-prompts /db status /session list|resume|search /compact light|normal|hard /mcp list|tools /tools /tool info|enable|disable|reload /tool-policy /exit")
	case "/status":
		fmt.Fprintf(out, "cwd: %s\nmode: %s\nprofile: %s\nmodel: %s\nsession: %s\n", rt.CWD, rt.Mode, rt.Profile, rt.Model, rt.SessionID)
	case "/modes":
		for _, m := range rt.Prompts.Modes() {
			fmt.Fprintf(out, "%s\t%s\n", m.Name, m.Description)
		}
	case "/mode":
		if len(p) < 2 {
			return false, fmt.Errorf("usage: /mode <name>")
		}
		m, e := rt.Prompts.Mode(p[1])
		if e != nil {
			return false, e
		}
		rt.Mode = m.Name
		if m.Profile != "" {
			rt.Profile = m.Profile
			rt.Model = rt.Config.Profiles[m.Profile].Model
		}
	case "/profile":
		if len(p) < 2 {
			return false, fmt.Errorf("usage: /profile <name>")
		}
		v, ok := rt.Config.Profiles[p[1]]
		if !ok {
			return false, fmt.Errorf("unknown profile %q", p[1])
		}
		rt.Profile = p[1]
		rt.Model = v.Model
	case "/model":
		if len(p) < 2 {
			return false, fmt.Errorf("usage: /model <name>")
		}
		rt.Model = strings.Join(p[1:], " ")
	case "/prompts":
		for _, v := range rt.Prompts.Files() {
			fmt.Fprintln(out, v)
		}
	case "/reload-prompts":
		if e := rt.Prompts.Reload(); e != nil {
			return false, e
		}
		fmt.Fprintln(out, "prompts reloaded")
	case "/db":
		fmt.Fprintln(out, rt.Store.Status(ctx))
	case "/session":
		return false, rt.sessionCommand(ctx, p, out)
	case "/compact":
		level := "normal"
		if len(p) > 1 {
			level = p[1]
		}
		return false, rt.compact(ctx, level, out)
	case "/mcp":
		if len(p) > 1 && p[1] == "list" {
			keys := make([]string, 0, len(rt.MCP))
			for k := range rt.MCP {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintln(out, k)
			}
		} else {
			rt.printTools(out, "mcp.")
		}
	case "/tools":
		prefix := ""
		if len(p) > 1 {
			prefix = p[1] + "."
		}
		rt.printTools(out, prefix)
	case "/tool":
		return false, rt.toolCommand(p, out)
	case "/tool-policy":
		fmt.Fprintln(out, "block > confirm > unknown > allow")
	default:
		return false, rt.RunInstruction(ctx, line, out)
	}
	return false, nil
}
func (rt *Runtime) printTools(out io.Writer, prefix string) {
	for _, n := range rt.Registry.Names() {
		if prefix == "" || strings.HasPrefix(n, prefix) {
			fmt.Fprintf(out, "%s\tenabled=%t\n", n, rt.Registry.Enabled(n))
		}
	}
}
func (rt *Runtime) toolCommand(p []string, out io.Writer) error {
	if len(p) < 2 {
		return fmt.Errorf("usage: /tool info|enable|disable|reload")
	}
	if p[1] == "reload" {
		rt.loadPlugins()
		fmt.Fprintln(out, "plugins reloaded")
		return nil
	}
	if len(p) < 3 {
		return fmt.Errorf("tool name required")
	}
	switch p[1] {
	case "info":
		t, ok := rt.Registry.Info(p[2])
		if !ok {
			return fmt.Errorf("unknown tool")
		}
		fmt.Fprintf(out, "%s\nenabled: %t\npolicy: %s\n%s\n", t.Name(), rt.Registry.Enabled(p[2]), t.Policy(), t.Description())
	case "enable":
		return rt.Registry.SetEnabled(p[2], true)
	case "disable":
		return rt.Registry.SetEnabled(p[2], false)
	default:
		return fmt.Errorf("unknown /tool action")
	}
	return nil
}
func (rt *Runtime) sessionCommand(ctx context.Context, p []string, out io.Writer) error {
	if len(p) > 2 && p[1] == "resume" {
		s, e := rt.Store.SessionByID(ctx, p[2])
		if e != nil {
			return e
		}
		if filepath.Clean(s.ProjectPath) != filepath.Clean(rt.CWD) {
			return fmt.Errorf("session belongs to a different project")
		}
		rt.SessionID = s.ID
		rt.Mode = s.Mode
		rt.Profile = s.Profile
		if v, ok := rt.Config.Profiles[s.Profile]; ok {
			rt.Model = v.Model
		}
		fmt.Fprintln(out, "resumed", s.ID)
		return nil
	}
	query := ""
	if len(p) > 2 && p[1] == "search" {
		query = strings.Join(p[2:], " ")
	}
	ss, e := rt.Store.Sessions(ctx, query)
	if e != nil {
		return e
	}
	for _, s := range ss {
		fmt.Fprintf(out, "%s\t%s\t%s\n", s.ID, s.Status, s.Title)
	}
	return nil
}
func (rt *Runtime) compact(ctx context.Context, level string, out io.Writer) error {
	if level != "light" && level != "normal" && level != "hard" {
		return fmt.Errorf("invalid compact level")
	}
	msgs, e := rt.Store.RecentMessages(ctx, rt.SessionID, 10000)
	if e != nil {
		return e
	}
	if len(msgs) == 0 {
		return nil
	}
	var b strings.Builder
	var to int64
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
		to = m.ID
	}
	profile := rt.Config.Profiles[rt.Config.Compact.Profile]
	if profile.Model == "" {
		profile = rt.Config.Profiles[rt.Profile]
	}
	promptText, e := os.ReadFile(rt.Config.Compact.PromptFile)
	if e != nil {
		return e
	}
	summary, e := rt.LLM.Chat(ctx, profile, []llm.Message{{Role: "system", Content: string(promptText)}, {Role: "user", Content: b.String()}}, false, nil)
	if e != nil {
		return e
	}
	if e = rt.Store.Compact(ctx, rt.SessionID, level, summary, to); e != nil {
		return e
	}
	fmt.Fprintln(out, "conversation compacted:", level)
	return nil
}

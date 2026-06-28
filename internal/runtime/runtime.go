package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	NoPersist, DryRun, NonInteractive                          bool
	MaxTurns                                                   int
}
type Runtime struct {
	Config                                                      config.Config
	Prompts                                                     *prompt.Manager
	Store                                                       *storage.Store
	Registry                                                    *tools.Registry
	Executor                                                    *tools.Executor
	LLM                                                         *llm.Client
	CWD, BinaryDir, ConfigPath, Mode, Profile, Model, SessionID string
	DryRun                                                      bool
	MCP                                                         map[string]mcp.Client
	Approve                                                     tools.Approver
	Transcript                                                  []TranscriptEntry
	ToolCalls                                                   []ToolCallRecord
	Diagnostics                                                 []string
	ToolDiagnostics                                             map[string]string
	MaxTurns                                                    int
}

func New(ctx context.Context, o Options) (*Runtime, error) {
	cfg, configFiles, e := config.Load(o.BinaryDir, o.CWD, o.ConfigPath)
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
	rt := &Runtime{Config: cfg, Prompts: pm, Store: store, Registry: tools.NewRegistry(), LLM: llm.New(), CWD: o.CWD, BinaryDir: o.BinaryDir, ConfigPath: o.ConfigPath, Mode: mode, Profile: profile, Model: model, SessionID: fmt.Sprintf("%d", time.Now().UnixNano()), DryRun: o.DryRun, MCP: map[string]mcp.Client{}, ToolDiagnostics: map[string]string{}, MaxTurns: o.MaxTurns}
	initialized := false
	defer func() {
		if !initialized {
			rt.Close()
		}
	}()
	if e = builtin.Register(rt.Registry); e != nil {
		return nil, e
	}
	if e = gittool.Register(rt.Registry); e != nil {
		return nil, e
	}
	if e = rt.Registry.Register(&shelltool.Runner{PolicyConfig: cfg.ShellPolicy, Store: store}); e != nil {
		return nil, e
	}
	rt.loadPlugins(false)
	rt.loadMCP(ctx, false)
	rt.Executor = &tools.Executor{Registry: rt.Registry, Store: store, NonInteractive: o.NonInteractive, PolicyDecision: func(name string) tools.Level {
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
	configFingerprint := fingerprintConfigFiles(configFiles)
	promptFingerprint := fingerprintFiles(pm.Files())
	if e = store.CreateSessionWithFingerprints(ctx, rt.SessionID, o.CWD, mode, profile, configFingerprint, promptFingerprint); e != nil {
		return nil, e
	}
	initialized = true
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
func (rt *Runtime) modeDecision(mode string, tool tools.Tool) tools.Level {
	m, err := rt.Prompts.Mode(mode)
	if err != nil {
		return tools.Unknown
	}
	name := tool.Name()
	if matchesAnyToolPattern(name, m.ToolDeny) {
		return tools.Block
	}
	if len(m.ToolAllow) > 0 && !matchesAnyToolPattern(name, m.ToolAllow) {
		return tools.Block
	}
	if am, ok := tool.(interface{ AvailableModes() []string }); ok {
		modes := am.AvailableModes()
		if len(modes) > 0 && !modeInList(mode, modes) {
			return tools.Block
		}
	}
	if mutator, ok := tool.(tools.FileMutator); ok && mutator.ModifiesFiles() {
		switch strings.ToLower(strings.TrimSpace(m.Write)) {
		case "allow", "true":
			return tools.Allow
		case "confirm":
			return tools.Confirm
		case "false", "block", "deny":
			return tools.Block
		default:
			return tools.Unknown
		}
	}
	if name == "shell.run" && m.Shell == "allow_readonly" {
		return tools.Confirm
	}
	if strings.HasPrefix(name, "mcp.") && !m.MCP {
		return tools.Block
	}
	return tools.Allow
}
func matchesAnyToolPattern(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchesToolPattern(name, p) {
			return true
		}
	}
	return false
}
func matchesToolPattern(name, pattern string) bool {
	if name == pattern {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-2]
		return strings.HasPrefix(name, prefix+".") || name == prefix
	}
	return false
}
func modeInList(mode string, modes []string) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}
func (rt *Runtime) recordToolDiagnostic(name, reason string) {
	if rt.ToolDiagnostics == nil {
		rt.ToolDiagnostics = map[string]string{}
	}
	rt.ToolDiagnostics[name] = reason
	rt.Diagnostics = append(rt.Diagnostics, name+": "+reason)
}
func (rt *Runtime) loadPlugins(reload bool) {
	if reload {
		for _, name := range rt.Registry.RemovePrefix("plugin.") {
			delete(rt.ToolDiagnostics, name)
		}
	}
	items, _ := plugin.LoadToolsJSON(filepath.Join(rt.BinaryDir, "coderenga.d", "tools.json"))
	more, _ := plugin.LoadDirectory(filepath.Join(rt.BinaryDir, "coderenga.d", "plugins"))
	items = append(items, more...)
	seen := map[string]bool{}
	for _, t := range items {
		name := t.Name()
		if seen[name] {
			rt.recordToolDiagnostic(name, "tool registration skipped: duplicate tool in reload batch")
			continue
		}
		seen[name] = true
		if reload {
			if err := rt.Registry.Replace(t); err != nil {
				rt.recordToolDiagnostic(name, "tool reload skipped: "+err.Error())
			}
			continue
		}
		if err := rt.Registry.RegisterDynamic(t); err != nil {
			rt.recordToolDiagnostic(name, "tool registration skipped: "+err.Error())
		}
	}
}
func (rt *Runtime) loadMCP(ctx context.Context, reload bool) {
	if !rt.Config.MCP.Enabled {
		return
	}
	if reload {
		for name, client := range rt.MCP {
			_ = client.Close()
			delete(rt.MCP, name)
		}
		for _, name := range rt.Registry.RemovePrefix("mcp.") {
			delete(rt.ToolDiagnostics, name)
		}
	}
	seen := map[string]bool{}
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
			toolName := mcp.Bridge{Server: name, Info: info, Client: c}.Name()
			if seen[toolName] {
				rt.recordToolDiagnostic(toolName, "mcp tool registration skipped: duplicate tool in reload batch")
				continue
			}
			seen[toolName] = true
			if err := rt.registerMCPTool(ctx, name, info, c, reload); err != nil {
				rt.recordToolDiagnostic(toolName, "mcp tool registration skipped: "+err.Error())
			}
		}
	}
}
func (rt *Runtime) RunInstruction(ctx context.Context, instruction string, out io.Writer) (err error) {
	defer func() {
		if err == nil {
			_ = rt.maybeAutoCompact(ctx)
		}
	}()
	if _, err := rt.addMessageNoCompact(ctx, "user", instruction); err != nil {
		return err
	}
	lastSignature := ""
	lastResults := map[string]string{}
	var callHistory []string
	var dryRunSkipped []tools.Request
	malformedRepairUsed := false
	taskStartRepairUsed := false
	loopRepairUsed := false
	maxTurns := rt.maxTurns()
turnLoop:
	for turn := 0; turn < maxTurns; turn++ {
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
		rt.recordTranscript(turn, "llm_output", "", "", answer, "", "")
		calls, err := tools.ParseCalls(answer)
		if err != nil {
			var malformed tools.MalformedToolCallError
			if errors.As(err, &malformed) {
				if malformedRepairUsed {
					rt.recordTranscript(turn, "parse_error", "", "", answer, "malformed_tool_call_after_repair", "")
					return fmt.Errorf("malformed tool call after repair: %w", err)
				}
				rt.recordTranscript(turn, "parse_error", "", "", answer, "malformed_tool_call", "")
				malformedRepairUsed = true
				if _, err = rt.addMessage(ctx, "assistant", answer); err != nil {
					return err
				}
				if _, err = rt.addMessage(ctx, "user", toolCallRepairMessage(malformed, answer)); err != nil {
					return err
				}
				rt.recordTranscript(turn, "recovery", "", "", malformed.Reason, "malformed_tool_call", "")
				continue
			}
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
			if shouldRecoverTaskStart(instruction, answer) {
				if taskStartRepairUsed {
					rt.recordTranscript(turn, "recovery_failed", "", "", answer, "task_start_stall", "")
					return fmt.Errorf("task-start recovery failed: model did not start the concrete task after recovery; last answer: %s", limitText(answer, 512))
				}
				rt.recordTranscript(turn, "recovery", "", "", answer, "task_start_stall", "")
				taskStartRepairUsed = true
				if _, err = rt.addMessage(ctx, "assistant", answer); err != nil {
					return err
				}
				if _, err = rt.addMessage(ctx, "user", taskStartRepairMessage(instruction, answer)); err != nil {
					return err
				}
				continue
			}
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
				if loopRepairUsed {
					rt.recordTranscript(turn, "loop_error", call.Name, signature, lastResults[signature], "repeated_tool_call", "")
					return fmt.Errorf("repeated tool call detected after recovery: %s was requested again immediately after its result; previous result: %s", toolCallSummary(call), lastResults[signature])
				}
				loopRepairUsed = true
				if _, err = rt.addMessage(ctx, "user", repeatedToolCallRecoveryMessage(call, lastResults[signature])); err != nil {
					return err
				}
				rt.recordTranscript(turn, "recovery", call.Name, signature, lastResults[signature], "repeated_tool_call", "")
				continue turnLoop
			}
			lastSignature = signature
			callHistory = append(callHistory, toolCallSummary(call))
			rt.recordToolStatus(call.Name, ToolCallGenerated)
			rt.recordTranscript(turn, "tool_call", call.Name, signature, "", "", "")
			call.Context = tools.Context{CWD: rt.CWD, Mode: rt.Mode, SessionID: rt.SessionID, DryRun: rt.DryRun}
			rt.recordToolStatus(call.Name, ToolCallRunning)
			res, err := rt.Executor.Execute(ctx, call)
			if err != nil {
				return err
			}
			lastResults[signature] = toolResultSummary(res)
			status := ToolCallDone
			if !res.OK {
				status = ToolCallFailed
				if strings.Contains(res.Error, "blocked by policy") {
					status = ToolCallBlocked
				}
			}
			rt.recordToolStatus(call.Name, status)
			rt.recordTranscript(turn, "tool_result", call.Name, signature, toolResultSummary(res), res.Error, "")
			body, _ := json.Marshal(res)
			toolResult := fmt.Sprintf("Tool result for %s:\n%s", call.Name, body)
			if _, err = rt.addMessage(ctx, "user", toolResult); err != nil {
				return err
			}
			if rt.DryRun && rt.isSideEffectTool(call.Name) {
				dryRunSkipped = append(dryRunSkipped, call)
				fmt.Fprintf(out, "[dry-run] %s\n", call.Name)
				printToolArguments(out, call.Arguments)
			}
		}
	}
	return fmt.Errorf("tool loop exceeded %d turns; calls: %s", maxTurns, strings.Join(callHistory, " -> "))
}

func (rt *Runtime) maxTurns() int {
	if rt.MaxTurns > 0 {
		return rt.MaxTurns
	}
	return 8
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

func toolCallRepairMessage(err tools.MalformedToolCallError, answer string) string {
	return fmt.Sprintf("The previous assistant message was intended to call a tool, but CodeRenga could not parse it. Reason: %s. Previous output: %s\n\nReturn exactly one JSON object with only the keys \"tool\" and \"arguments\", or answer in plain text if no tool is needed. Do not use XML tags, Markdown fences, or prose around a tool call. Example: {\"tool\":\"builtin.read_file\",\"arguments\":{\"path\":\"README.md\"}}", err.Reason, limitText(answer, 512))
}

func repeatedToolCallRecoveryMessage(call tools.Request, previous string) string {
	return fmt.Sprintf("The previous tool call was already executed and returned this result: %s. Do not repeat the same tool call. Continue with a different tool, an edit tool, or a concrete final answer. Repeated call: %s", limitText(previous, 512), toolCallSummary(call))
}

func taskStartRepairMessage(instruction, answer string) string {
	return fmt.Sprintf("The user gave a concrete repository task, but the previous answer did not start it. User instruction: %s. Previous answer: %s\n\nStart the task now. If repository context is needed, return exactly one JSON tool call using builtin.read_file, builtin.list_files, or builtin.search_text. If no tool is needed, provide a concrete final answer. Do not ask what to do next.", limitText(instruction, 512), limitText(answer, 512))
}

func shouldRecoverTaskStart(instruction, answer string) bool {
	if isSimpleConversation(instruction) || !looksLikeConcreteTask(instruction) {
		return false
	}
	return looksLikeTaskStartStall(answer)
}

func looksLikeConcreteTask(value string) bool {
	v := strings.ToLower(value)
	markers := []string{
		"implement", "fix", "edit", "update", "modify", "add test", "add tests", "review", "readme", "design doc", "document",
		"実装", "修正", "更新", "変更", "追加", "レビュー", "設計書", "テスト", "ドキュメント",
	}
	for _, marker := range markers {
		if strings.Contains(v, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func looksLikeTaskStartStall(answer string) bool {
	v := strings.ToLower(strings.TrimSpace(answer))
	if v == "" {
		return false
	}
	markers := []string{
		"hello", "hi", "how can i help", "what would you like", "what do you want", "please provide", "please tell me", "could you provide", "どのような", "何を", "教えてください", "お手伝い", "こんにちは",
	}
	for _, marker := range markers {
		if strings.Contains(v, strings.ToLower(marker)) {
			return true
		}
	}
	return false
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

func (rt *Runtime) isSideEffectTool(name string) bool {
	if tool, ok := rt.Registry.Info(name); ok {
		if mutator, ok := tool.(tools.FileMutator); ok && mutator.ModifiesFiles() {
			return true
		}
	}
	return name == "shell.run" || strings.HasPrefix(name, "plugin.") || strings.HasPrefix(name, "mcp.")
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
		for _, diagnostic := range rt.Diagnostics {
			fmt.Fprintln(out, "diagnostic:", diagnostic)
		}
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
		return false, rt.toolCommand(ctx, p, out)
	case "/tool-policy":
		fmt.Fprintln(out, "block > confirm > unknown > allow")
	default:
		return false, rt.RunInstruction(ctx, line, out)
	}
	return false, nil
}
func (rt *Runtime) printTools(out io.Writer, prefix string) {
	printed := map[string]bool{}
	for _, n := range rt.Registry.Names() {
		if prefix == "" || strings.HasPrefix(n, prefix) {
			printed[n] = true
			if reason := rt.ToolDiagnostics[n]; reason != "" {
				fmt.Fprintf(out, "%s	enabled=%t	reason=%s\n", n, rt.Registry.Enabled(n), reason)
			} else {
				fmt.Fprintf(out, "%s	enabled=%t\n", n, rt.Registry.Enabled(n))
			}
		}
	}
	for _, n := range sortedDiagnosticNames(rt.ToolDiagnostics) {
		if printed[n] || (prefix != "" && !strings.HasPrefix(n, prefix)) {
			continue
		}
		fmt.Fprintf(out, "%s	enabled=false	reason=%s\n", n, rt.ToolDiagnostics[n])
	}
}

func sortedDiagnosticNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func (rt *Runtime) toolCommand(ctx context.Context, p []string, out io.Writer) error {
	if len(p) < 2 {
		return fmt.Errorf("usage: /tool info|enable|disable|reload")
	}
	if p[1] == "reload" {
		rt.loadPlugins(true)
		rt.loadMCP(ctx, true)
		fmt.Fprintln(out, "tools reloaded")
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
		if reason := rt.ToolDiagnostics[p[2]]; reason != "" {
			fmt.Fprintln(out, "reason:", reason)
		}
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
		currentConfig, currentPrompt := rt.currentFingerprints()
		if s.ConfigFingerprint != "" && s.ConfigFingerprint != currentConfig {
			fmt.Fprintln(out, "warning: configuration fingerprint differs from the resumed session")
		}
		if s.PromptFingerprint != "" && s.PromptFingerprint != currentPrompt {
			fmt.Fprintln(out, "warning: prompt fingerprint differs from the resumed session")
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
func (rt *Runtime) currentFingerprints() (string, string) {
	_, configFiles, _ := config.Load(rt.BinaryDir, rt.CWD, rt.ConfigPath)
	return fingerprintConfigFiles(configFiles), fingerprintFiles(rt.Prompts.Files())
}

func fingerprintFiles(files []string) string {
	return fingerprintFilesWithSanitizer(files, nil)
}

func fingerprintConfigFiles(files []string) string {
	return fingerprintFilesWithSanitizer(files, sanitizeFingerprintJSON)
}

func fingerprintFilesWithSanitizer(files []string, sanitize func([]byte) []byte) string {
	h := sha256.New()
	for _, file := range files {
		if file == "" {
			continue
		}
		h.Write([]byte(file))
		if b, err := os.ReadFile(file); err == nil {
			if sanitize != nil {
				b = sanitize(b)
			}
			h.Write(b)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func sanitizeFingerprintJSON(input []byte) []byte {
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return input
	}
	value = sanitizeFingerprintValue(value)
	b, err := json.Marshal(value)
	if err != nil {
		return input
	}
	return b
}

func sanitizeFingerprintValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if secretFingerprintKey(key) {
				out[key] = "<present>"
				continue
			}
			out[key] = sanitizeFingerprintValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = sanitizeFingerprintValue(child)
		}
		return out
	default:
		return v
	}
}

func secretFingerprintKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	switch normalized {
	case "apikey", "token", "secret", "password", "credential", "credentials", "clientsecret":
		return true
	default:
		return false
	}
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
	systemPrompt := string(promptText)
	if target := rt.compactTargetTokens(level); target > 0 {
		systemPrompt += fmt.Sprintf("\n\nTarget summary length: about %d tokens.", target)
	}
	summary, e := rt.LLM.Chat(ctx, profile, []llm.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: b.String()}}, false, nil)
	if e != nil {
		return e
	}
	if e = rt.Store.Compact(ctx, rt.SessionID, level, summary, to); e != nil {
		return e
	}
	fmt.Fprintln(out, "conversation compacted:", level)
	return nil
}

func (rt *Runtime) compactTargetTokens(level string) int {
	if rt.Config.Compact.Levels == nil {
		return 0
	}
	return rt.Config.Compact.Levels[level].TargetTokens
}

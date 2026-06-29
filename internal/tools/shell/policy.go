package shell

import (
	"bytes"
	"context"
	"fmt"
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/storage"
	"github.com/tks/coderenga/internal/tools"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const outputCapBytes = 65536

type Runner struct {
	PolicyConfig config.ShellPolicy
	Store        *storage.Store
}

func (r *Runner) Name() string        { return "shell.run" }
func (r *Runner) Description() string { return "Run a segmented shell command." }
func (r *Runner) Policy() tools.Level { return tools.Allow }
func (r *Runner) Schema() map[string]any {
	return tools.ObjectSchema(map[string]any{
		"command":    tools.StringProperty("Command text to parse and run."),
		"argv":       tools.StringArrayProperty("Command and arguments for direct execution."),
		"shell_mode": tools.BoolProperty("Use the platform shell for shell syntax."),
	}, nil)
}

// readArgv extracts argv from req.Arguments["argv"], accepting both []string
// and []any of strings. Returns nil, false for empty or non-string entries.
func readArgv(args map[string]any) ([]string, bool) {
	raw, ok := args["argv"]
	if !ok {
		return nil, false
	}

	switch v := raw.(type) {
	case []string:
		if len(v) == 0 {
			return nil, false
		}
		return v, true
	case []any:
		if len(v) == 0 {
			return nil, false
		}
		result := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			result = append(result, s)
		}
		return result, true
	default:
		return nil, false
	}
}

func (r *Runner) Decision(req tools.Request) tools.Level {
	// argv path: evaluate as single segment directly
	if argv, ok := readArgv(req.Arguments); ok {
		return Evaluate(r.PolicyConfig, [][]string{argv})
	}

	command, _ := req.Arguments["command"].(string)
	shellMode, _ := req.Arguments["shell_mode"].(bool)

	if shellMode {
		segs, e := SplitShellMode(command)
		if e != nil {
			return tools.Block
		}
		// shell_mode never relaxes Block: Max(EvaluateCompound(policy, segments), Confirm)
		return tools.Max(EvaluateCompound(r.PolicyConfig, segs), tools.Confirm)
	}

	segs, e := Split(command)
	if e != nil {
		return tools.Block
	}
	return EvaluateCompound(r.PolicyConfig, segs)
}
func Split(command string) ([][]string, error) {
	return splitCommand(command, false)
}

func SplitShellMode(command string) ([][]string, error) {
	return splitCommand(command, true)
}

func splitCommand(command string, allowShellSyntax bool) ([][]string, error) {
	var segs [][]string
	var argv []string
	var token strings.Builder
	quote := rune(0)
	escaped := false
	flushToken := func() {
		if token.Len() > 0 {
			argv = append(argv, token.String())
			token.Reset()
		}
	}
	flushSeg := func() {
		flushToken()
		if len(argv) > 0 {
			segs = append(segs, argv)
			argv = nil
		}
	}
	rr := []rune(command)
	for i := 0; i < len(rr); i++ {
		ch := rr[i]
		if escaped {
			token.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				token.WriteRune(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '$' && i+1 < len(rr) && rr[i+1] == '(' {
			if !allowShellSyntax {
				return nil, fmt.Errorf("command substitution is not allowed")
			}
			flushSeg()
			depth := 1
			start := i + 2
			found := false
			for j := start; j < len(rr); j++ {
				switch rr[j] {
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						inner, err := splitCommand(string(rr[start:j]), true)
						if err != nil {
							return nil, err
						}
						segs = append(segs, inner...)
						i = j
						found = true
					}
				}
				if found {
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("unterminated command substitution")
			}
			continue
		}
		if !allowShellSyntax && (ch == '>' || ch == '<') {
			return nil, fmt.Errorf("redirection is unsupported")
		}
		if ch == ';' || ch == '\n' || ch == '|' || (ch == '&' && i+1 < len(rr) && rr[i+1] == '&') {
			flushSeg()
			if (ch == '|' || ch == '&') && i+1 < len(rr) && rr[i+1] == ch {
				i++
			}
			continue
		}
		if unicode.IsSpace(ch) {
			flushToken()
			continue
		}
		token.WriteRune(ch)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flushSeg()
	if len(segs) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return segs, nil
}

func Match(rule config.ShellRule, a []string) bool {
	if len(a) == 0 || rule.Cmd == "" || !strings.EqualFold(a[0], rule.Cmd) {
		return false
	}
	if rule.Match == "exact" {
		return len(a) == 1
	}
	if len(a) < 1+len(rule.Args) {
		return false
	}
	for i, v := range rule.Args {
		if a[i+1] != v {
			return false
		}
	}
	return true
}

func Evaluate(p config.ShellPolicy, segs [][]string) tools.Level {
	overall := tools.Allow
	for _, a := range segs {
		level := tools.ParseLevel(p.Unknown)
		for _, x := range p.Allow {
			if Match(x, a) {
				level = tools.Allow
			}
		}
		for _, x := range p.Confirm {
			if Match(x, a) {
				level = tools.Max(level, tools.Confirm)
			}
		}
		for _, x := range p.Block {
			if Match(x, a) {
				level = tools.Block
			}
		}
		overall = tools.Max(overall, level)
	}
	return overall
}

func (r *Runner) Execute(ctx context.Context, req tools.Request) (tools.Result, error) {
	// --- argv path: direct exec, no shell ---
	if argv, ok := readArgv(req.Arguments); ok {
		// Policy check
		if r.Decision(req) == tools.Block {
			return tools.Result{OK: false, Error: "shell command blocked by policy"}, nil
		}

		approved, _ := req.Arguments["_coderenga_approved"].(bool)
		if !approved {
			return tools.Result{OK: false, Error: "shell command was not approved"}, nil
		}

		return r.executeDirect(ctx, req, argv, approved)
	}

	// --- command string path ---
	command, ok := req.Arguments["command"].(string)
	if !ok || command == "" {
		return tools.Result{}, fmt.Errorf("command or argv is required")
	}

	shellMode, _ := req.Arguments["shell_mode"].(bool)

	var segs [][]string
	var err error
	if shellMode {
		segs, err = SplitShellMode(command)
	} else {
		segs, err = Split(command)
	}
	if err != nil {
		return tools.Result{OK: false, Error: "failed to parse command: " + err.Error()}, nil
	}

	// Policy check
	if r.Decision(req) == tools.Block {
		return tools.Result{OK: false, Error: "shell command blocked by policy"}, nil
	}

	approved, _ := req.Arguments["_coderenga_approved"].(bool)
	if !approved {
		return tools.Result{OK: false, Error: "shell command was not approved"}, nil
	}

	// Single segment without shell_mode → direct exec (no /bin/sh or powershell)
	if len(segs) == 1 && !shellMode {
		return r.executeDirect(ctx, req, segs[0], approved)
	}

	// Multiple segments or shell syntax without shell_mode → block
	if !shellMode {
		return tools.Result{OK: false, Error: "complex commands (pipes, redirections, multiple statements) require shell_mode=true; blocked for safety"}, nil
	}

	// shell_mode=true → execute through shell
	return r.executeShell(ctx, req, command, approved)
}

func (r *Runner) executeDirect(ctx context.Context, req tools.Request, argv []string, approved bool) (tools.Result, error) {
	if req.Context.DryRun {
		return tools.Result{OK: true, Content: "dry-run: would run " + strings.Join(argv, " ")}, nil
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Context.CWD
	cmd.Env = []string{"PATH=" + getenv("PATH")}
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	start := time.Now()
	e := cmd.Run()
	exit := 0
	if e != nil {
		exit = -1
		if x, ok := e.(*exec.ExitError); ok {
			exit = x.ExitCode()
		}
	}

	if r.Store != nil {
		_ = r.Store.ShellRun(ctx, req.Context.SessionID, strings.Join(argv, " "), req.Context.CWD, exit, capOutput(out.String()), capOutput(errout.String()), r.Decision(req).String(), approved, time.Since(start))
	}

	res := tools.Result{OK: e == nil, Content: capOutput(out.String()), Metadata: map[string]any{"stderr": capOutput(errout.String()), "exit_code": exit}}
	if e != nil {
		res.Error = e.Error()
	}
	return res, nil
}

func (r *Runner) executeShell(ctx context.Context, req tools.Request, command string, approved bool) (tools.Result, error) {
	if req.Context.DryRun {
		return tools.Result{OK: true, Content: "dry-run: would run " + command}, nil
	}

	shell, args := "/bin/sh", []string{"-c", command}
	if runtime.GOOS == "windows" {
		shell, args = "powershell.exe", []string{"-NoProfile", "-Command", command}
	}
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = req.Context.CWD
	cmd.Env = []string{"PATH=" + getenv("PATH")}
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	start := time.Now()
	e := cmd.Run()
	exit := 0
	if e != nil {
		exit = -1
		if x, ok := e.(*exec.ExitError); ok {
			exit = x.ExitCode()
		}
	}

	if r.Store != nil {
		_ = r.Store.ShellRun(ctx, req.Context.SessionID, command, req.Context.CWD, exit, capOutput(out.String()), capOutput(errout.String()), r.Decision(req).String(), approved, time.Since(start))
	}

	res := tools.Result{OK: e == nil, Content: capOutput(out.String()), Metadata: map[string]any{"stderr": capOutput(errout.String()), "exit_code": exit}}
	if e != nil {
		res.Error = e.Error()
	}
	return res, nil
}

func capOutput(value string) string {
	if len(value) <= outputCapBytes {
		return value
	}
	return value[:outputCapBytes] + "\n[output truncated]"
}

var getenv = func(string) string { return "" }

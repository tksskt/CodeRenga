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

type Runner struct {
	PolicyConfig config.ShellPolicy
	Store        *storage.Store
}

func (r *Runner) Name() string        { return "shell.run" }
func (r *Runner) Description() string { return "Run a segmented shell command." }
func (r *Runner) Policy() tools.Level { return tools.Allow }
func (r *Runner) Decision(req tools.Request) tools.Level {
	command, _ := req.Arguments["command"].(string)
	segs, e := Split(command)
	if e != nil {
		return tools.Block
	}
	return EvaluateCompound(r.PolicyConfig, segs)
}
func Split(command string) ([][]string, error) {
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
			return nil, fmt.Errorf("command substitution is not allowed")
		}
		if ch == '>' || ch == '<' {
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
	command, ok := req.Arguments["command"].(string)
	if !ok || command == "" {
		return tools.Result{}, fmt.Errorf("command is required")
	}
	if r.Decision(req) == tools.Block {
		return tools.Result{OK: false, Error: "shell command blocked"}, nil
	}
	approved, _ := req.Arguments["_coderenga_approved"].(bool)
	if !approved {
		return tools.Result{OK: false, Error: "shell command was not approved"}, nil
	}
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
		_ = r.Store.ShellRun(ctx, req.Context.SessionID, command, req.Context.CWD, exit, out.String(), errout.String(), r.Decision(req).String(), approved, time.Since(start))
	}
	res := tools.Result{OK: e == nil, Content: out.String(), Metadata: map[string]any{"stderr": errout.String(), "exit_code": exit}}
	if e != nil {
		res.Error = e.Error()
	}
	return res, nil
}

var getenv = func(string) string { return "" }

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/storage"
	"sort"
	"strings"
	"time"
)

type Approver func(string, map[string]any) bool
type RequestPolicy interface{ Decision(Request) Level }
type Executor struct {
	Registry       *Registry
	Store          *storage.Store
	Approver       Approver
	ModeDecision   func(string, string) Level
	PolicyDecision func(string) Level
}

func (e *Executor) Execute(ctx context.Context, req Request) (Result, error) {
	t, ok := e.Registry.Get(req.Name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", req.Name)
	}
	levels := []Level{t.Policy()}
	if e.PolicyDecision != nil {
		levels = append(levels, e.PolicyDecision(req.Name))
	}
	if e.ModeDecision != nil {
		levels = append(levels, e.ModeDecision(req.Context.Mode, req.Name))
	}
	if dynamic, ok := t.(RequestPolicy); ok {
		levels = append(levels, dynamic.Decision(req))
	}
	decision := Max(levels...)
	if req.Context.DryRun && hasSideEffects(req.Name) {
		res := dryRunResult(req)
		e.record(ctx, req, res, decision, false, 0)
		return res, nil
	}
	approved := decision == Allow
	if decision == Block {
		res := Result{OK: false, Error: "tool blocked by policy"}
		e.record(ctx, req, res, decision, false, 0)
		return res, nil
	}
	if decision == Confirm || decision == Unknown {
		if e.Approver == nil || !e.Approver(req.Name, req.Arguments) {
			res := Result{OK: false, Error: "tool execution was not approved"}
			e.record(ctx, req, res, decision, false, 0)
			return res, nil
		}
		approved = true
	}
	if req.Arguments == nil {
		req.Arguments = map[string]any{}
	}
	req.Arguments["_coderenga_approved"] = approved
	start := time.Now()
	res, err := t.Execute(ctx, req)
	duration := time.Since(start)
	if err != nil {
		res = Result{OK: false, Error: err.Error()}
	}
	e.record(ctx, req, res, decision, approved, duration)
	return res, nil
}

func (e *Executor) record(ctx context.Context, req Request, res Result, decision Level, approved bool, duration time.Duration) {
	if e.Store == nil {
		return
	}
	args := map[string]any{}
	for k, v := range req.Arguments {
		if !strings.HasPrefix(k, "_coderenga_") {
			args[k] = v
		}
	}
	b, _ := json.Marshal(redact(args))
	_ = e.Store.Audit(ctx, req.Context.SessionID, "tool_finished", map[string]any{"tool": req.Name, "policy": decision.String(), "approved": approved, "ok": res.OK, "dry_run": req.Context.DryRun})
	_ = e.Store.ToolRunDetailed(ctx, req.Context.SessionID, req.Name, strings.Split(req.Name, ".")[0], string(b), res.Content, map[bool]string{true: "ok", false: "failed"}[res.OK], decision.String(), approved, duration)
}

func hasSideEffects(name string) bool {
	return name == "builtin.write_file" || name == "builtin.apply_patch" || name == "shell.run" || strings.HasPrefix(name, "plugin.") || strings.HasPrefix(name, "mcp.")
}

func dryRunResult(req Request) Result {
	var b strings.Builder
	fmt.Fprintf(&b, "dry-run: %s was not executed.\n", req.Name)
	keys := make([]string, 0, len(req.Arguments))
	for key := range req.Arguments {
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
		value := req.Arguments[key]
		if key == "content" || key == "patch" {
			fmt.Fprintf(&b, "%s:\n%v\n", key, value)
		} else {
			fmt.Fprintf(&b, "%s: %v\n", key, value)
		}
	}
	switch req.Name {
	case "builtin.write_file", "builtin.apply_patch":
		b.WriteString("No file was written because --dry-run is enabled.")
	case "shell.run":
		b.WriteString("No command was executed because --dry-run is enabled.")
	default:
		b.WriteString("No side effect was performed because --dry-run is enabled.")
	}
	return Result{OK: true, Content: strings.TrimSpace(b.String()), Metadata: map[string]any{"dry_run": true, "executed": false}}
}

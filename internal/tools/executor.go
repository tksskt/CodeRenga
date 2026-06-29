package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/policy"
	"github.com/tks/coderenga/internal/storage"
	"sort"
	"strings"
	"time"
)

type Approver func(string, map[string]any) bool
type RequestPolicy interface{ Decision(Request) Level }

type ConfirmationRequiredError struct{ Tool string }

func (e ConfirmationRequiredError) Error() string {
	return fmt.Sprintf("tool %s requires approval, but --non-interactive is set. Re-run without --non-interactive to approve it interactively, change the mode/tools.json/shell policy, or run with %s.", e.Tool, autoApproveHint(e.Tool))
}

type Executor struct {
	Registry       *Registry
	Store          *storage.Store
	Approver       Approver
	ModeDecision   func(string, Tool) Level
	PolicyDecision func(string) Level
	NonInteractive bool
	AutoApprove    map[string]bool
}

func (e *Executor) autoApproves(tool string) bool {
	if len(e.AutoApprove) == 0 {
		return false
	}
	if e.AutoApprove["all"] {
		return true
	}
	for _, category := range ToolCategories(tool) {
		if e.AutoApprove[category] {
			return true
		}
	}
	return false
}

func ToolCategories(tool string) []string {
	switch {
	case tool == "builtin.read_file" || tool == "builtin.list_files" || tool == "builtin.search_text":
		return []string{"read"}
	case tool == "builtin.write_file" || tool == "builtin.apply_patch":
		return []string{"write"}
	case tool == "shell.run":
		return []string{"shell", "exec"}
	case strings.HasPrefix(tool, "git."):
		return []string{"git", "read"}
	case strings.HasPrefix(tool, "plugin.") || strings.HasPrefix(tool, "mcp."):
		return []string{"dangerous"}
	default:
		return []string{"dangerous"}
	}
}

func NormalizeAutoApprove(values []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			category := strings.ToLower(strings.TrimSpace(part))
			if category == "" {
				continue
			}
			switch category {
			case "all", "read", "write", "shell", "exec", "git", "dangerous":
				out[category] = true
			default:
				return nil, fmt.Errorf("unknown auto-approve category %q", category)
			}
		}
	}
	return out, nil
}

func autoApproveHint(tool string) string {
	category := "dangerous"
	if categories := ToolCategories(tool); len(categories) > 0 {
		category = categories[0]
	}
	if tool == "shell.run" {
		category = "shell"
	}
	return fmt.Sprintf("--auto-approve %s", category)
}
func (e *Executor) Execute(ctx context.Context, req Request) (Result, error) {
	t, ok := e.Registry.Get(req.Name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", req.Name)
	}
	inspectors := []policy.Inspector{
		policy.InspectorFunc(func(policy.Request) policy.Result {
			level := t.Policy()
			return policy.Result{Decision: policyFromLevel(level), Reason: "tool manifest policy: " + level.String()}
		}),
	}
	if e.PolicyDecision != nil {
		inspectors = append(inspectors, policy.InspectorFunc(func(policy.Request) policy.Result {
			level := e.PolicyDecision(req.Name)
			return policy.Result{Decision: policyFromLevel(level), Reason: "tool_policy: " + level.String()}
		}))
	}
	if e.ModeDecision != nil {
		inspectors = append(inspectors, policy.InspectorFunc(func(policy.Request) policy.Result {
			level := e.ModeDecision(req.Context.Mode, t)
			return policy.Result{Decision: policyFromLevel(level), Reason: "mode policy: " + level.String()}
		}))
	}
	if dynamic, ok := t.(RequestPolicy); ok {
		inspectors = append(inspectors, policy.InspectorFunc(func(policy.Request) policy.Result {
			level := dynamic.Decision(req)
			return policy.Result{Decision: policyFromLevel(level), Reason: "request policy: " + level.String()}
		}))
	}
	inspectors = append(inspectors, policy.InspectorFunc(func(policy.Request) policy.Result { return safetyResult(req) }))
	policyDecision, reasons := policy.Engine{Inspectors: inspectors}.Decide(policy.Request{Tool: req.Name, Arguments: req.Arguments, Mode: req.Context.Mode, CWD: req.Context.CWD})
	decision := levelFromPolicy(policyDecision)
	policyReasons := policyReasonStrings(reasons)
	if decision == Block {
		res := Result{OK: false, Error: "tool blocked by policy"}
		e.record(ctx, req, res, decision, false, 0, policyReasons)
		return res, nil
	}
	if req.Context.DryRun && hasSideEffects(t) {
		res := dryRunResult(req)
		e.record(ctx, req, res, decision, false, 0, policyReasons)
		return res, nil
	}
	approved := decision == Allow
	if decision == Confirm || decision == Unknown {
		if e.NonInteractive {
			if e.autoApproves(req.Name) {
				approved = true
			} else {
				res := Result{OK: false, Error: fmt.Sprintf("tool %s requires approval, but --non-interactive is set. Run with %s to allow this tool category in non-interactive mode.", req.Name, autoApproveHint(req.Name))}
				e.record(ctx, req, res, decision, false, 0, policyReasons)
				return res, ConfirmationRequiredError{Tool: req.Name}
			}
		} else if e.Approver == nil || !e.Approver(req.Name, req.Arguments) {
			res := Result{OK: false, Error: "tool execution was not approved"}
			e.record(ctx, req, res, decision, false, 0, policyReasons)
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
	e.record(ctx, req, res, decision, approved, duration, policyReasons)
	return res, nil
}

func (e *Executor) record(ctx context.Context, req Request, res Result, decision Level, approved bool, duration time.Duration, policyReasons []string) {
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
	_ = e.Store.Audit(ctx, req.Context.SessionID, "tool_finished", map[string]any{"tool": req.Name, "policy": decision.String(), "policy_reasons": policyReasons, "approved": approved, "ok": res.OK, "dry_run": req.Context.DryRun})
	_ = e.Store.ToolRunDetailed(ctx, req.Context.SessionID, req.Name, strings.Split(req.Name, ".")[0], string(b), res.Content, map[bool]string{true: "ok", false: "failed"}[res.OK], decision.String(), approved, duration)
}

func policyReasonStrings(reasons []policy.Result) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if reason.Reason == "" {
			continue
		}
		out = append(out, reason.Decision.String()+": "+reason.Reason)
	}
	return out
}
func policyFromLevel(level Level) policy.Decision {
	switch level {
	case Allow:
		return policy.AutoApprove
	case Confirm:
		return policy.AskUser
	case Block:
		return policy.Reject
	default:
		return policy.Unknown
	}
}

func levelFromPolicy(decision policy.Decision) Level {
	switch decision {
	case policy.AutoApprove:
		return Allow
	case policy.AskUser:
		return Confirm
	case policy.Reject:
		return Block
	default:
		return Unknown
	}
}

func safetyResult(req Request) policy.Result {
	path, _ := req.Arguments["path"].(string)
	shellMode, _ := req.Arguments["shell_mode"].(bool)
	return policy.SafetyCheck(policy.SafetySubject{Tool: req.Name, CWD: req.Context.CWD, Path: path, ShellMode: shellMode, SandboxReady: true})
}

func hasSideEffects(tool Tool) bool {
	if mutator, ok := tool.(FileMutator); ok && mutator.ModifiesFiles() {
		return true
	}
	name := tool.Name()
	return name == "shell.run" || strings.HasPrefix(name, "plugin.") || strings.HasPrefix(name, "mcp.")
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

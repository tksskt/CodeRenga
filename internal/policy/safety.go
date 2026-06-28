package policy

import (
	"path/filepath"
	"strings"
)

type SafetySubject struct {
	Tool         string
	CWD          string
	Path         string
	ShellMode    bool
	SandboxReady bool
}

func SafetyCheck(subject SafetySubject) Result {
	if subject.Path != "" && subject.CWD != "" {
		target := subject.Path
		if !filepath.IsAbs(target) {
			target = filepath.Join(subject.CWD, target)
		}
		rel, err := filepath.Rel(filepath.Clean(subject.CWD), filepath.Clean(target))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Result{Decision: Reject, Reason: "path escapes writable root"}
		}
	}
	if subject.Tool == "shell.run" && subject.ShellMode {
		return Result{Decision: AskUser, Reason: "shell_mode requires user approval"}
	}
	if !subject.SandboxReady && (subject.Tool == "shell.run" || strings.HasPrefix(subject.Tool, "plugin.") || strings.HasPrefix(subject.Tool, "mcp.")) {
		return Result{Decision: AskUser, Reason: "sandbox is unavailable for side-effect tool"}
	}
	return Result{Decision: AutoApprove, Reason: "safe by default"}
}

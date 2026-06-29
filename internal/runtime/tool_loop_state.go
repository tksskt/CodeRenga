package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tks/coderenga/internal/tools"
)

type loopRuntimeState struct {
	LastSuccessfulTool          string
	LastToolResultSummary       string
	LastSuccessfulShellCommand  string
	LastSuccessfulShellExitCode int
	LastFailedShellCommand      string
	LastFailedShellExitCode     int
	ConsecutiveFailedShell      string
	ConsecutiveFailedShellCount int
	LastVerificationCommand     string
	LastVerificationChangeSeq   int
	FileChangeSeq               int
	PossiblyChangedFiles        map[string]bool
}

func newLoopRuntimeState() *loopRuntimeState {
	return &loopRuntimeState{PossiblyChangedFiles: map[string]bool{}}
}

func (s *loopRuntimeState) afterTool(call tools.Request, res tools.Result) []string {
	if s == nil {
		return nil
	}
	s.LastToolResultSummary = toolResultSummary(res)
	if res.OK {
		s.LastSuccessfulTool = call.Name
	}
	s.recordPossibleFileChange(call, res)
	if call.Name != "shell.run" {
		if res.OK {
			s.ConsecutiveFailedShell = ""
			s.ConsecutiveFailedShellCount = 0
		}
		return nil
	}
	return s.afterShell(call, res)
}

func (s *loopRuntimeState) afterShell(call tools.Request, res tools.Result) []string {
	command := shellCommandText(call)
	exitCode, _ := shellExitCode(res)
	var reminders []string
	if res.OK {
		s.LastSuccessfulShellCommand = command
		s.LastSuccessfulShellExitCode = exitCode
		s.ConsecutiveFailedShell = ""
		s.ConsecutiveFailedShellCount = 0
		if isVerificationCommand(command) {
			s.LastVerificationCommand = command
			s.LastVerificationChangeSeq = s.FileChangeSeq
			reminders = append(reminders, fmt.Sprintf("Runtime reminder: %s already succeeded. Do not run the same verification command again unless files changed; provide the final answer if the implementation is complete.", command))
		}
		return reminders
	}
	s.LastFailedShellCommand = command
	s.LastFailedShellExitCode = exitCode
	if command != "" && command == s.ConsecutiveFailedShell {
		s.ConsecutiveFailedShellCount++
	} else {
		s.ConsecutiveFailedShell = command
		s.ConsecutiveFailedShellCount = 1
	}
	if s.ConsecutiveFailedShellCount >= 2 {
		reminders = append(reminders, "Runtime reminder: Do not retry the same shell command again. Inspect the error and either fix files or provide the final answer.")
	}
	return reminders
}

func (s *loopRuntimeState) shouldSkipShell(call tools.Request) (tools.Result, bool) {
	if s == nil || call.Name != "shell.run" {
		return tools.Result{}, false
	}
	command := shellCommandText(call)
	if command == "" || command != s.LastVerificationCommand || s.FileChangeSeq != s.LastVerificationChangeSeq || !isVerificationCommand(command) {
		return tools.Result{}, false
	}
	content := fmt.Sprintf("runtime skipped duplicate verification command because %q already succeeded and files have not changed", command)
	return tools.Result{OK: true, Content: content, Metadata: map[string]any{"skipped": true, "reason": "duplicate_successful_verification", "command": command}}, true
}

func (s *loopRuntimeState) recordPossibleFileChange(call tools.Request, res tools.Result) {
	if s == nil || !res.OK {
		return
	}
	switch call.Name {
	case "builtin.write_file":
		if path, ok := call.Arguments["path"].(string); ok && path != "" {
			s.addChangedFile(path)
		}
	case "builtin.apply_patch":
		for _, path := range patchTargetFiles(call.Arguments["patch"]) {
			s.addChangedFile(path)
		}
	}
}

func (s *loopRuntimeState) addChangedFile(path string) {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || path == "" {
		return
	}
	s.PossiblyChangedFiles[path] = true
	s.FileChangeSeq++
}

func shellCommandText(call tools.Request) string {
	if argv, ok := call.Arguments["argv"].([]string); ok && len(argv) > 0 {
		return strings.Join(argv, " ")
	}
	if argvAny, ok := call.Arguments["argv"].([]any); ok && len(argvAny) > 0 {
		parts := make([]string, 0, len(argvAny))
		for _, v := range argvAny {
			s, ok := v.(string)
			if !ok {
				return ""
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " ")
	}
	if command, ok := call.Arguments["command"].(string); ok {
		return strings.TrimSpace(command)
	}
	return ""
}

func shellExitCode(res tools.Result) (int, bool) {
	if res.Metadata == nil {
		return 0, false
	}
	switch v := res.Metadata["exit_code"].(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func isVerificationCommand(command string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	switch normalized {
	case "go test ./...", "go test":
		return true
	default:
		return strings.Contains(normalized, " test ") || strings.HasSuffix(normalized, " test") || strings.HasPrefix(normalized, "test ")
	}
}

var patchTargetRE = regexp.MustCompile(`(?m)^\+\+\+ (?:b/)?([^\r\n\t ]+)`)

func patchTargetFiles(raw any) []string {
	patch, ok := raw.(string)
	if !ok || patch == "" {
		return nil
	}
	matches := patchTargetRE.FindAllStringSubmatch(patch, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 && match[1] != "/dev/null" {
			out = append(out, match[1])
		}
	}
	return out
}

func maxTurnReminder(remaining int) string {
	if remaining <= 1 {
		return "Runtime reminder: This is the final allowed turn. Do not call tools unless absolutely unavoidable. If implementation or verification is already complete, answer the user now with the current status."
	}
	return "Runtime reminder: The tool loop is near --max-turns. Keep additional tool calls to the minimum. If implementation or tests are already complete, provide the final answer."
}

func maxTurnExceededError(maxTurns int, callHistory []string, state *loopRuntimeState) error {
	var b strings.Builder
	fmt.Fprintf(&b, "tool loop exceeded %d turns; calls: %s", maxTurns, strings.Join(callHistory, " -> "))
	b.WriteString("\nfinal answer was not generated.")
	if state == nil {
		return fmt.Errorf("%s", b.String())
	}
	if state.LastSuccessfulTool != "" {
		fmt.Fprintf(&b, "\nlast successful tool: %s", state.LastSuccessfulTool)
	} else {
		b.WriteString("\nlast successful tool: none")
	}
	if state.LastToolResultSummary != "" {
		fmt.Fprintf(&b, "\nlast tool result summary: %s", state.LastToolResultSummary)
	} else {
		b.WriteString("\nlast tool result summary: none")
	}
	if state.LastSuccessfulShellCommand != "" {
		fmt.Fprintf(&b, "\nlast successful shell command: %s (exit code %d)", state.LastSuccessfulShellCommand, state.LastSuccessfulShellExitCode)
	} else {
		b.WriteString("\nlast successful shell command: none")
	}
	changed := make([]string, 0, len(state.PossiblyChangedFiles))
	for path := range state.PossiblyChangedFiles {
		changed = append(changed, path)
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		b.WriteString("\npossibly changed files: none recorded")
	} else {
		fmt.Fprintf(&b, "\npossibly changed files: %s", strings.Join(changed, ", "))
	}
	return fmt.Errorf("%s", b.String())
}

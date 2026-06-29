package runtime

import (
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/tools"
)

func TestLoopRuntimeStateRemindsAfterRepeatedShellFailure(t *testing.T) {
	state := newLoopRuntimeState()
	call := tools.Request{Name: "shell.run", Arguments: map[string]any{"command": "go test ./..."}}
	failed := tools.Result{OK: false, Error: "exit status 1", Metadata: map[string]any{"exit_code": 1}}

	if reminders := state.afterTool(call, failed); len(reminders) != 0 {
		t.Fatalf("first failure reminders=%v", reminders)
	}
	reminders := state.afterTool(call, failed)
	if len(reminders) != 1 || !strings.Contains(reminders[0], "Do not retry the same shell command again") {
		t.Fatalf("second failure reminders=%v", reminders)
	}
	if state.LastFailedShellCommand != "go test ./..." || state.LastFailedShellExitCode != 1 {
		t.Fatalf("state=%#v", state)
	}
}

func TestLoopRuntimeStateSkipsDuplicateSuccessfulVerificationUntilFilesChange(t *testing.T) {
	state := newLoopRuntimeState()
	call := tools.Request{Name: "shell.run", Arguments: map[string]any{"command": "go test ./..."}}
	passed := tools.Result{OK: true, Content: "ok", Metadata: map[string]any{"exit_code": 0}}

	reminders := state.afterTool(call, passed)
	if len(reminders) != 1 || !strings.Contains(reminders[0], "already succeeded") {
		t.Fatalf("success reminders=%v", reminders)
	}
	res, skipped := state.shouldSkipShell(call)
	if !skipped || !res.OK || !strings.Contains(res.Content, "already succeeded") {
		t.Fatalf("skipped=%v res=%#v", skipped, res)
	}
	state.afterTool(tools.Request{Name: "builtin.write_file", Arguments: map[string]any{"path": "main.go"}}, tools.Result{OK: true, Content: "wrote main.go"})
	_, skipped = state.shouldSkipShell(call)
	if skipped {
		t.Fatal("verification was skipped after file changes")
	}
}

func TestMaxTurnExceededErrorIncludesLastKnownStatus(t *testing.T) {
	state := newLoopRuntimeState()
	state.afterTool(tools.Request{Name: "builtin.write_file", Arguments: map[string]any{"path": "main.go"}}, tools.Result{OK: true, Content: "wrote main.go"})
	state.afterTool(tools.Request{Name: "shell.run", Arguments: map[string]any{"command": "go test ./..."}}, tools.Result{OK: true, Content: "ok", Metadata: map[string]any{"exit_code": 0}})

	err := maxTurnExceededError(20, []string{"builtin.write_file {}", "shell.run {}"}, state)
	text := err.Error()
	for _, want := range []string{
		"tool loop exceeded 20 turns",
		"final answer was not generated",
		"last successful tool: shell.run",
		"last tool result summary: ok",
		"last successful shell command: go test ./... (exit code 0)",
		"possibly changed files: main.go",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

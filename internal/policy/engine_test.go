package policy

import (
	"path/filepath"
	"testing"
)

func TestEngineRejectWinsOverAllow(t *testing.T) {
	engine := Engine{Inspectors: []Inspector{
		InspectorFunc(func(Request) Result { return Result{Decision: AutoApprove, Reason: "allow"} }),
		InspectorFunc(func(Request) Result { return Result{Decision: Reject, Reason: "mode block"} }),
	}}
	decision, reasons := engine.Decide(Request{Tool: "builtin.write_file"})
	if decision != Reject || len(reasons) != 2 || reasons[1].Reason != "mode block" {
		t.Fatalf("decision=%v reasons=%v", decision, reasons)
	}
}

func TestSafetyCheckRejectsPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	result := SafetyCheck(SafetySubject{Tool: "builtin.write_file", CWD: root, Path: filepath.Join(root, "..", "outside.txt"), SandboxReady: true})
	if result.Decision != Reject || result.Reason == "" {
		t.Fatalf("result=%#v", result)
	}
}

func TestSafetyCheckShellModeRequiresApproval(t *testing.T) {
	result := SafetyCheck(SafetySubject{Tool: "shell.run", ShellMode: true, SandboxReady: true})
	if result.Decision != AskUser {
		t.Fatalf("result=%#v", result)
	}
}

func TestRepetitionInspectorRejectsRepeat(t *testing.T) {
	inspector := &RepetitionInspector{}
	req := Request{Tool: "builtin.read_file", Arguments: map[string]any{"path": "README.md"}}
	if got := inspector.Inspect(req); got.Decision != AutoApprove {
		t.Fatalf("first=%#v", got)
	}
	if got := inspector.Inspect(req); got.Decision != Reject {
		t.Fatalf("repeat=%#v", got)
	}
}

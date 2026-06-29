package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCoderModeWritesWithoutConfirmation(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", false, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"test.txt","content":"hello"}}`,
		"done",
	})
	defer rt.Close()
	approvalRequested := false
	rt.Approve = func(string, map[string]any) bool { approvalRequested = true; return false }
	if err := rt.RunInstruction(context.Background(), "write test.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if approvalRequested {
		t.Fatal("coder mode requested confirmation")
	}
	b, err := os.ReadFile(filepath.Join(root, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("content=%q", b)
	}
}

func TestCoderModeWritesWhenToolPolicyIsUnspecified(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "", false, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"unspecified.txt","content":"hello"}}`,
		"done",
	})
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool {
		t.Fatal("unspecified neutral policy requested confirmation")
		return false
	}
	if err := rt.RunInstruction(context.Background(), "write unspecified.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "unspecified.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestCoderModeAppliesPatchWithoutConfirmation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call, _ := json.Marshal(map[string]any{"tool": "builtin.apply_patch", "arguments": map[string]any{"patch": "--- a/test.txt\n+++ b/test.txt\n@@ -1 +1 @@\n-old\n+new\n"}})
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", false, []string{string(call), "done"})
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool { t.Fatal("coder mode requested confirmation"); return false }
	if err := rt.RunInstruction(context.Background(), "patch test.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new\n" {
		t.Fatalf("content=%q", b)
	}
}

func TestDebugModeRequiresConfirmation(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(fmt.Sprintf("approved_%t", approved), func(t *testing.T) {
			root := t.TempDir()
			rt := newModePolicyRuntime(t, root, "debug", "confirm", "allow", false, []string{
				`{"tool":"builtin.write_file","arguments":{"path":"debug.txt","content":"hello"}}`,
				"finished",
			})
			defer rt.Close()
			requested := false
			rt.Approve = func(string, map[string]any) bool { requested = true; return approved }
			if err := rt.RunInstruction(context.Background(), "write debug.txt", &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if !requested {
				t.Fatal("debug mode did not request confirmation")
			}
			_, err := os.Stat(filepath.Join(root, "debug.txt"))
			if approved && err != nil {
				t.Fatalf("approved write failed: %v", err)
			}
			if !approved && !os.IsNotExist(err) {
				t.Fatalf("declined write occurred: %v", err)
			}
		})
	}
}

func TestReadOnlyModesBlockWritesWithoutConfirmation(t *testing.T) {
	for _, mode := range []string{"architect", "reviewer"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			rt := newModePolicyRuntime(t, root, mode, "false", "allow", false, []string{
				fmt.Sprintf(`{"tool":"builtin.write_file","arguments":{"path":%q,"content":"hello"}}`, mode+".txt"),
				"Editing is disabled; here is a proposal.",
			})
			defer rt.Close()
			rt.Approve = func(string, map[string]any) bool { t.Fatal("read-only mode requested confirmation"); return false }
			if err := rt.RunInstruction(context.Background(), "propose a change", &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, mode+".txt")); !os.IsNotExist(err) {
				t.Fatalf("write occurred: %v", err)
			}
		})
	}
}

func TestToolsJSONBlockWinsInCoderMode(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "block", false, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"blocked.txt","content":"hello"}}`,
		"blocked",
	})
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool { t.Fatal("blocked tool requested confirmation"); return false }
	if err := rt.RunInstruction(context.Background(), "write blocked.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("write occurred: %v", err)
	}
}

func TestNonInteractiveConfirmFailsWithoutPrompt(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "debug", "confirm", "allow", true, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"debug.txt","content":"hello"}}`,
	})
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool { t.Fatal("non-interactive execution prompted"); return true }
	err := rt.RunInstruction(context.Background(), "write debug.txt", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "tool builtin.write_file requires approval, but --non-interactive is set") || !strings.Contains(err.Error(), "--auto-approve write") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "debug.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("write occurred: %v", statErr)
	}
}

func TestCoderDryRunStillDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", false, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"dryrun.txt","content":"hello dry run"}}`,
		"The file was created.",
	})
	defer rt.Close()
	rt.DryRun = true
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "write dryrun.txt", &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dryrun.txt")); !os.IsNotExist(err) {
		t.Fatalf("write occurred: %v", err)
	}
	if strings.Contains(out.String(), "The file was created") || !strings.Contains(out.String(), "was not executed") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestCoderWritePolicyDoesNotAllowShellConfirm(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", true, []string{
		`{"tool":"shell.run","arguments":{"command":"echo hello"}}`,
	})
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "run echo", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "tool shell.run requires approval, but --non-interactive is set") || !strings.Contains(err.Error(), "--auto-approve shell") {
		t.Fatalf("err=%v", err)
	}
}

func TestNonInteractiveShellRequiresExplicitAutoApprove(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", true, []string{
		testShellToolCall(t, root, "blocked.txt"),
	})
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "run shell", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "tool shell.run requires approval, but --non-interactive is set") || !strings.Contains(err.Error(), "--auto-approve shell") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "blocked.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("shell command ran: %v", statErr)
	}
}

func TestNonInteractiveAutoApproveShellRunsShell(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", true, []string{
		testShellToolCall(t, root, "ran.txt"),
		"done",
	})
	defer rt.Close()
	rt.Executor.AutoApprove = map[string]bool{"shell": true}
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "run shell", &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "ran.txt")); err != nil {
		t.Fatalf("shell command did not run: %v", err)
	}
}

func TestNonInteractiveAutoApproveReadWriteDoesNotRunShell(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", true, []string{
		testShellToolCall(t, root, "not-shell.txt"),
	})
	defer rt.Close()
	rt.Executor.AutoApprove = map[string]bool{"read": true, "write": true}
	err := rt.RunInstruction(context.Background(), "run shell", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--auto-approve shell") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "not-shell.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("shell command ran: %v", statErr)
	}
}

func TestHelperProcessWriteFile(t *testing.T) {
	args := os.Args
	for i, arg := range args {
		if arg == "--coderenga-write-file" && i+1 < len(args) {
			if err := os.WriteFile(args[i+1], []byte("ok"), 0o644); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
}

func testShellToolCall(t *testing.T, root, name string) string {
	t.Helper()
	argv := []string{os.Args[0], "-test.run=TestHelperProcessWriteFile", "--", "--coderenga-write-file", filepath.Join(root, name)}
	call, err := json.Marshal(map[string]any{"tool": "shell.run", "arguments": map[string]any{"argv": argv}})
	if err != nil {
		t.Fatal(err)
	}
	return string(call)
}
func TestToolDenyBlocksWriteFile(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", false, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"denied.txt","content":"hello"}}`,
		"blocked by tool_deny",
	}, "tool_deny: builtin.write_file")
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool {
		t.Fatal("tool_deny should block without confirmation")
		return false
	}
	if err := rt.RunInstruction(context.Background(), "write denied.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("write occurred despite tool_deny: %v", err)
	}
}

func TestToolAllowBlocksWriteFile(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", false, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"allowed.txt","content":"hello"}}`,
		"blocked by tool_allow",
	}, "tool_allow: builtin.read_file")
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool {
		t.Fatal("tool_allow should block without confirmation")
		return false
	}
	if err := rt.RunInstruction(context.Background(), "write allowed.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "allowed.txt")); !os.IsNotExist(err) {
		t.Fatalf("write occurred despite tool_allow restriction: %v", err)
	}
}

func TestSystemPromptOmitsModeDeniedTools(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", false, []string{}, "tool_deny: builtin.write_file")
	defer rt.Close()
	prompt := rt.systemPrompt()
	if strings.Contains(prompt, "builtin.write_file") {
		t.Fatalf("denied tool was exposed in system prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "builtin.read_file") {
		t.Fatalf("read tool missing from system prompt: %s", prompt)
	}
}

func TestSystemPromptOmitsToolPolicyBlockedTools(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "block", false, []string{})
	defer rt.Close()
	prompt := rt.systemPrompt()
	if strings.Contains(prompt, "builtin.write_file") {
		t.Fatalf("tool_policy block tool was exposed in system prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "builtin.read_file") {
		t.Fatalf("read tool missing from system prompt: %s", prompt)
	}
}
func newModePolicyRuntime(t *testing.T, root, mode, writeMode, writeToolPolicy string, nonInteractive bool, answers []string, extraFrontmatter ...string) *Runtime {
	t.Helper()
	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		index := requestCount
		requestCount++
		mu.Unlock()
		if index >= len(answers) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answers[index]}}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	t.Cleanup(server.Close)
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolsJSON := `{"version":1,"policies":{"shell.run":"confirm"},"plugins":{}}`
	if writeToolPolicy != "" {
		toolsJSON = fmt.Sprintf(`{"version":1,"policies":{"builtin.write_file":%q,"builtin.apply_patch":%q,"shell.run":"confirm"},"plugins":{}}`, writeToolPolicy, writeToolPolicy)
	}

	extraLines := strings.Join(extraFrontmatter, "\n")
	if extraLines != "" {
		extraLines = "\n" + extraLines
	}
	modeContent := fmt.Sprintf("---\nname: %s\nwrite: %s\nshell: policy\nmcp: true%s\n---\nmode", mode, writeMode, extraLines)

	files := map[string]string{
		"config.json":           `{"version":1,"defaultMode":"` + mode + `","defaultProfile":"test","state":{"database":"coderenga.db"}}`,
		"llm.json":              fmt.Sprintf(`{"version":1,"profiles":{"test":{"baseURL":%q,"model":"m"}}}`, server.URL),
		"tools.json":            toolsJSON,
		"prompts/default.md":    "system",
		"prompts/compact.md":    "compact",
		"modes/" + mode + ".md": modeContent,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true, NonInteractive: nonInteractive})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

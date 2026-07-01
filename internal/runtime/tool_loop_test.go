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

func TestToolLoopReadsFileAndReturnsFinalAnswer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("CodeRenga test README"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, requests := newToolLoopRuntime(t, root, []string{
		`{"tool":"builtin.read_file","arguments":{"path":"README.md"}}`,
		"README summary",
	})
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "read README", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "README summary\n" {
		t.Fatalf("output=%q", out.String())
	}
	foundToolResult := false
	for _, entry := range rt.Transcript {
		if entry.Kind == "tool_result" && entry.Tool == "builtin.read_file" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Fatalf("transcript=%#v", rt.Transcript)
	}
	if len(rt.ToolCalls) == 0 || rt.ToolCalls[len(rt.ToolCalls)-1].Status != ToolCallDone {
		t.Fatalf("tool calls=%#v", rt.ToolCalls)
	}
	if len(*requests) != 2 || !strings.Contains((*requests)[1], "CodeRenga test README") {
		t.Fatalf("requests=%v", *requests)
	}
}

func TestToolLoopDryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	rt, requests := newToolLoopRuntime(t, root, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"test.txt","content":"hello from coderenga"}}`,
		"File test.txt has been updated.",
	})
	defer rt.Close()
	rt.DryRun = true
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "write", &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "test.txt")); !os.IsNotExist(err) {
		t.Fatalf("file created: %v", err)
	}
	if !strings.Contains(out.String(), "[dry-run] builtin.write_file") || !strings.Contains(out.String(), "hello from coderenga") || !strings.Contains(out.String(), "was not executed") || !strings.Contains(out.String(), "no file was written") {
		t.Fatalf("output=%q", out.String())
	}
	if strings.Contains(strings.ToLower(out.String()), "has been updated") {
		t.Fatalf("dry-run exposed contradictory model answer: %q", out.String())
	}
	if len(*requests) != 2 || !strings.Contains((*requests)[1], "was not executed") || !strings.Contains((*requests)[1], "\\\"executed\\\":false") {
		t.Fatalf("dry-run result was not returned to model: %v", *requests)
	}
}

func TestToolLoopWritesAfterApproval(t *testing.T) {
	root := t.TempDir()
	rt, _ := newToolLoopRuntime(t, root, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"test.txt","content":"hello from coderenga"}}`,
		"written",
	})
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool { return true }
	if err := rt.RunInstruction(context.Background(), "write", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello from coderenga" {
		t.Fatalf("content=%q", b)
	}
}

func TestSimpleGreetingDoesNotExecuteUnexpectedToolCall(t *testing.T) {
	root := t.TempDir()
	rt, requests := newToolLoopRuntime(t, root, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"acenga.d","content":"unexpected"}}`,
	})
	defer rt.Close()
	approved := false
	rt.Approve = func(string, map[string]any) bool { approved = true; return true }
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "hello", &out); err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("unexpected tool call requested approval")
	}
	if _, err := os.Stat(filepath.Join(root, "acenga.d")); !os.IsNotExist(err) {
		t.Fatalf("unexpected file: %v", err)
	}
	if len(*requests) != 1 || !strings.Contains(out.String(), "Hello!") {
		t.Fatalf("requests=%v output=%q", *requests, out.String())
	}
}

func TestMalformedToolCallGetsOneRepairTurn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("repair README"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, requests := newToolLoopRuntime(t, root, []string{
		`<tool>{"tool":"builtin.read_file","arguments":{"path":"README.md"}}</tool>`,
		`{"tool":"builtin.read_file","arguments":{"path":"README.md"}}`,
		"repaired summary",
	})
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "read README", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "repaired summary\n" {
		t.Fatalf("output=%q", out.String())
	}
	if len(*requests) != 3 {
		t.Fatalf("requests=%d", len(*requests))
	}
	if !strings.Contains((*requests)[1], "legacy tag formats") || !strings.Contains((*requests)[1], "exactly one JSON object") {
		t.Fatalf("repair prompt was not sent: %s", (*requests)[1])
	}
	if !strings.Contains((*requests)[2], "repair README") {
		t.Fatalf("tool result was not returned after repair: %s", (*requests)[2])
	}
}

func TestMalformedToolCallRepairFailureReturnsError(t *testing.T) {
	root := t.TempDir()
	rt, _ := newToolLoopRuntime(t, root, []string{
		`<tool>{"tool":"builtin.read_file","arguments":{"path":"README.md"}}</tool>`,
		`I will read it now. {"tool":"builtin.read_file","arguments":{"path":"README.md"}}`,
	})
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "read README", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "malformed tool call after repair") || !strings.Contains(err.Error(), "tool calls must not include prose") {
		t.Fatalf("err=%v", err)
	}
}
func TestConcreteTaskStallGetsOneRecoveryTurn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("task README"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, requests := newToolLoopRuntime(t, root, []string{
		"Hello! What would you like me to implement?",
		`{"tool":"builtin.read_file","arguments":{"path":"README.md"}}`,
		"implemented after reading",
	})
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "implement the README update", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "implemented after reading\n" {
		t.Fatalf("output=%q", out.String())
	}
	if len(*requests) != 3 {
		t.Fatalf("requests=%d", len(*requests))
	}
	if !strings.Contains((*requests)[1], "concrete repository task") || !strings.Contains((*requests)[1], "Start the task now") {
		t.Fatalf("task-start recovery prompt was not sent: %s", (*requests)[1])
	}
	if !strings.Contains((*requests)[2], "task README") {
		t.Fatalf("tool result was not returned after recovery: %s", (*requests)[2])
	}
}

func TestSimpleGreetingDoesNotGetTaskStartRecovery(t *testing.T) {
	root := t.TempDir()
	rt, requests := newToolLoopRuntime(t, root, []string{
		"Hello! How can I help with your coding task?",
	})
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "hello", &out); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 {
		t.Fatalf("greeting should not trigger recovery, requests=%d", len(*requests))
	}
	if strings.Contains((*requests)[0], "concrete repository task") {
		t.Fatalf("unexpected task-start recovery prompt: %s", (*requests)[0])
	}
}

func TestConcreteTaskStallRecoveryFailureReturnsError(t *testing.T) {
	root := t.TempDir()
	rt, _ := newToolLoopRuntime(t, root, []string{
		"Hello! What would you like me to implement?",
		"Please provide more details about what you want me to change.",
	})
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "implement the README update", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "task-start recovery failed") || !strings.Contains(err.Error(), "Please provide more details") {
		t.Fatalf("err=%v", err)
	}
}
func TestRepeatedToolCallReportsToolArgumentsAndPreviousResult(t *testing.T) {
	root := t.TempDir()
	call := `{"tool":"builtin.read_file","arguments":{"path":"acenga.d"}}`
	rt, _ := newToolLoopRuntime(t, root, []string{call, call, call})
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "read acenga.d", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "repeated tool call detected after recovery") || !strings.Contains(err.Error(), "builtin.read_file") || !strings.Contains(err.Error(), "acenga.d") || !strings.Contains(err.Error(), "does not exist within cwd") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "GetFileAttributesEx") {
		t.Fatalf("raw Windows path error leaked: %v", err)
	}
}

func TestRepeatedToolCallRecoveryAllowsFinalAnswer(t *testing.T) {
	root := t.TempDir()
	call := `{"tool":"builtin.read_file","arguments":{"path":"missing.txt"}}`
	rt, requests := newToolLoopRuntime(t, root, []string{call, call, "I will stop repeating and report the missing file."})
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "inspect missing file", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "missing file") {
		t.Fatalf("output=%q", out.String())
	}
	if len(*requests) != 3 || !strings.Contains((*requests)[2], "Do not repeat the same tool call") {
		t.Fatalf("requests=%v", *requests)
	}
}

func TestAutoCompactRunsAfterInstructionNotBeforeToolResultIsRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("raw tool result that must reach the next model turn"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, requests := newToolLoopRuntime(t, root, []string{
		`{"tool":"builtin.read_file","arguments":{"path":"README.md"}}`,
		"final answer after raw tool result",
		"compact summary",
	})
	defer rt.Close()
	rt.Config.Compact.Enabled = true
	rt.Config.Compact.TriggerContextRatio = 0.01
	rt.Config.Compact.TriggerTurns = 1
	rt.Config.Compact.ContextTokens = 1
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "read README", &out); err != nil {
		t.Fatal(err)
	}
	if len(*requests) < 2 {
		t.Fatalf("requests=%d", len(*requests))
	}
	if !strings.Contains((*requests)[1], "raw tool result that must reach the next model turn") {
		t.Fatalf("tool result was compacted before the next model turn: %s", (*requests)[1])
	}
}
func TestAddMessageStoresTokenEstimateForContextRatio(t *testing.T) {
	root := t.TempDir()
	rt, _ := newToolLoopRuntime(t, root, []string{})
	defer rt.Close()
	rt.Config.Compact.Enabled = false
	if _, err := rt.addMessage(context.Background(), "user", strings.Repeat("x", 20)); err != nil {
		t.Fatal(err)
	}
	estimate, err := rt.Store.UncompactedTokenEstimate(context.Background(), rt.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if estimate != 5 {
		t.Fatalf("estimate=%d", estimate)
	}
	rt.Config.Compact.ContextTokens = 10
	if ratio := float64(estimate) / float64(rt.currentContextTokenLimit()); ratio < 0.5 {
		t.Fatalf("ratio=%f", ratio)
	}
}
func TestToolLoopLimitReportsCallHistory(t *testing.T) {
	root := t.TempDir()
	answers := make([]string, 16)
	for i := range answers {
		answers[i] = fmt.Sprintf(`{"tool":"builtin.read_file","arguments":{"path":"missing-%d.txt"}}`, i)
	}
	rt, _ := newToolLoopRuntime(t, root, answers)
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "inspect missing files", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "tool loop exceeded 16 turns; calls:") || !strings.Contains(err.Error(), "missing-0.txt") || !strings.Contains(err.Error(), "missing-15.txt") {
		t.Fatalf("err=%v", err)
	}
}

func TestRuntimeMaxTurnsUsesDefaultForZero(t *testing.T) {
	rt := &Runtime{MaxTurns: 0}
	if got := rt.maxTurns(); got != 16 {
		t.Fatalf("maxTurns=%d", got)
	}
}

func TestRuntimeMaxTurnsUsesExplicitPositiveValue(t *testing.T) {
	rt := &Runtime{MaxTurns: 10}
	if got := rt.maxTurns(); got != 10 {
		t.Fatalf("maxTurns=%d", got)
	}
}

func TestToolLoopMaxTurnsCanUseExplicitValue(t *testing.T) {
	root := t.TempDir()
	answers := make([]string, 10)
	for i := range answers {
		answers[i] = fmt.Sprintf(`{"tool":"builtin.read_file","arguments":{"path":"missing-%d.txt"}}`, i)
	}
	rt, _ := newToolLoopRuntime(t, root, answers)
	defer rt.Close()
	rt.MaxTurns = 10
	err := rt.RunInstruction(context.Background(), "inspect missing files", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "tool loop exceeded 10 turns; calls:") || !strings.Contains(err.Error(), "missing-9.txt") {
		t.Fatalf("err=%v", err)
	}
}
func newToolLoopRuntime(t *testing.T, root string, answers []string) (*Runtime, *[]string) {
	t.Helper()
	var mu sync.Mutex
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(body)
		mu.Lock()
		requests = append(requests, string(encoded))
		index := len(requests) - 1
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
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"test","state":{"database":"coderenga.db"}}`,
		"llm.json":           fmt.Sprintf(`{"version":1,"profiles":{"test":{"baseURL":%q,"model":"m"}}}`, server.URL),
		"tools.json":         `{"version":1,"policies":{"builtin.read_file":"allow","builtin.write_file":"confirm"},"plugins":{}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	return rt, &requests
}

func TestLlamaCppNativeConcreteTaskEmptyResponseGetsRecovery(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("native recovery README"), 0o644); err != nil {
		t.Fatal(err)
	}
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		switch len(requests) {
		case 1:
			fmt.Fprintln(w, `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`)
		case 2:
			fmt.Fprintln(w, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call_0","type":"function","function":{"name":"builtin__read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
		default:
			fmt.Fprintln(w, `{"choices":[{"message":{"content":"native recovered"},"finish_reason":"stop"}]}`)
		}
	}))
	defer server.Close()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"test","state":{"database":"coderenga.db"}}`,
		"llm.json":           fmt.Sprintf(`{"version":1,"profiles":{"test":{"baseURL":%q,"model":"m","toolProtocol":"llamacpp_tools","parallelToolCalls":false}}}`, server.URL),
		"tools.json":         `{"version":1,"tool_policy":{"builtin.read_file":"allow"},"plugins":{}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "update README implementation", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "native recovered\n" {
		t.Fatalf("output=%q", out.String())
	}
	if len(requests) != 3 {
		t.Fatalf("requests=%d", len(requests))
	}
	encoded, _ := json.Marshal(requests[1]["messages"])
	if !strings.Contains(string(encoded), "Start the task now") {
		t.Fatalf("native recovery prompt missing: %s", encoded)
	}
	if strings.Contains(string(encoded), `"role":"assistant","content":""`) {
		t.Fatalf("empty assistant message was sent to llama.cpp recovery: %s", encoded)
	}
}
func TestLlamaCppNativeEmptyFinalAfterToolGetsFinalizeReminder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("native final README"), 0o644); err != nil {
		t.Fatal(err)
	}
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		switch len(requests) {
		case 1:
			fmt.Fprintln(w, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call_0","type":"function","function":{"name":"builtin__read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
		case 2:
			fmt.Fprintln(w, `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`)
		default:
			fmt.Fprintln(w, `{"choices":[{"message":{"content":"native finalized"},"finish_reason":"stop"}]}`)
		}
	}))
	defer server.Close()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"test","state":{"database":"coderenga.db"}}`,
		"llm.json":           fmt.Sprintf(`{"version":1,"profiles":{"test":{"baseURL":%q,"model":"m","toolProtocol":"llamacpp_tools","parallelToolCalls":false}}}`, server.URL),
		"tools.json":         `{"version":1,"tool_policy":{"builtin.read_file":"allow"},"plugins":{}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "update README implementation", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "native finalized\n" {
		t.Fatalf("output=%q", out.String())
	}
	if len(requests) != 3 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[2]["tool_choice"] != "required" {
		t.Fatalf("tool_choice=%#v", requests[2]["tool_choice"])
	}
	encoded, _ := json.Marshal(requests[2]["messages"])
	if !strings.Contains(string(encoded), "previous response had no tool calls and no final answer") || !strings.Contains(string(encoded), "README/documentation") || !strings.Contains(string(encoded), "no successful file edit") {
		t.Fatalf("empty-final reminder missing: %s", encoded)
	}
	if strings.Contains(string(encoded), "Start the task now") {
		t.Fatalf("used task-start reminder after tool use: %s", encoded)
	}
	if strings.Contains(string(encoded), `"role":"assistant","content":""`) {
		t.Fatalf("empty assistant message was sent to llama.cpp finalization recovery: %s", encoded)
	}
}
func TestLlamaCppNativeToolLoopReadsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("native README"), 0o644); err != nil {
		t.Fatal(err)
	}
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			fmt.Fprintln(w, `{"choices":[{"message":{"content":null,"tool_calls":[{"type":"function","function":{"name":"builtin__read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprintln(w, `{"choices":[{"message":{"content":"native summary"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json":        `{"version":1,"defaultMode":"coder","defaultProfile":"test","state":{"database":"coderenga.db"}}`,
		"llm.json":           fmt.Sprintf(`{"version":1,"profiles":{"test":{"baseURL":%q,"model":"m","toolProtocol":"llamacpp_tools","parallelToolCalls":false}}}`, server.URL),
		"tools.json":         `{"version":1,"tool_policy":{"builtin.read_file":"allow"},"plugins":{}}`,
		"prompts/default.md": "system",
		"prompts/compact.md": "compact",
		"modes/coder.md":     "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := New(context.Background(), Options{BinaryDir: root, CWD: root, NoPersist: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	var out bytes.Buffer
	if err := rt.RunInstruction(context.Background(), "read README", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "native summary\n" {
		t.Fatalf("output=%q", out.String())
	}
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0]["stream"] != false || requests[0]["parallel_tool_calls"] != false || requests[0]["tools"] == nil {
		t.Fatalf("native request missing tools fields: %#v", requests[0])
	}
	encoded, _ := json.Marshal(requests[1]["messages"])
	if !strings.Contains(string(encoded), "native README") || !strings.Contains(string(encoded), "tool_call_id") || !strings.Contains(string(encoded), "call_0") {
		t.Fatalf("tool result history missing: %s", encoded)
	}
}

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

func TestRepeatedToolCallReportsToolArgumentsAndPreviousResult(t *testing.T) {
	root := t.TempDir()
	call := `{"tool":"builtin.read_file","arguments":{"path":"acenga.d"}}`
	rt, _ := newToolLoopRuntime(t, root, []string{call, call})
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "read acenga.d", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "repeated tool call detected") || !strings.Contains(err.Error(), "builtin.read_file") || !strings.Contains(err.Error(), "acenga.d") || !strings.Contains(err.Error(), "does not exist within cwd") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "GetFileAttributesEx") {
		t.Fatalf("raw Windows path error leaked: %v", err)
	}
}

func TestToolLoopLimitReportsCallHistory(t *testing.T) {
	root := t.TempDir()
	answers := make([]string, 8)
	for i := range answers {
		answers[i] = fmt.Sprintf(`{"tool":"builtin.read_file","arguments":{"path":"missing-%d.txt"}}`, i)
	}
	rt, _ := newToolLoopRuntime(t, root, answers)
	defer rt.Close()
	err := rt.RunInstruction(context.Background(), "inspect missing files", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "tool loop exceeded 8 turns; calls:") || !strings.Contains(err.Error(), "missing-0.txt") || !strings.Contains(err.Error(), "missing-7.txt") {
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

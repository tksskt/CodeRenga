package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tks/coderenga/internal/storage"
)

func TestCoderWorkerWritesWithoutStdinConfirmation(t *testing.T) {
	answers := []string{
		`{"tool":"builtin.write_file","arguments":{"path":"test.txt","content":"hello"}}`,
		"done",
	}
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(count.Add(1) - 1)
		if index >= len(answers) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answers[index]}}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	defer server.Close()
	root := t.TempDir()
	writeTestConfig(t, root)
	dir := filepath.Join(root, "coderenga.d")
	llm := fmt.Sprintf(`{"version":1,"profiles":{"local":{"baseURL":%q,"model":"test"}}}`, server.URL)
	if err := os.WriteFile(filepath.Join(dir, "llm.json"), []byte(llm), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools.json"), []byte(`{"version":1,"policies":{"builtin.write_file":"allow","builtin.apply_patch":"allow","shell.run":"confirm"},"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "modes", "coder.md"), []byte("---\nname: coder\nwrite: allow\nshell: policy\nmcp: true\n---\ncoder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := storage.Bootstrap(filepath.Join(dir, "coderenga.db")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "--mode", "coder", "write test.txt"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "Execute builtin.write_file?") {
		t.Fatalf("prompted: %q", stdout.String())
	}
	b, err := os.ReadFile(filepath.Join(root, "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("content=%q", b)
	}
}

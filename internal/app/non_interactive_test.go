package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/storage"
)

func TestNonInteractiveDebugWriteFailsWithoutPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"{\"tool\":\"builtin.write_file\",\"arguments\":{\"path\":\"debug.txt\",\"content\":\"hello\"}}"}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()
	root := t.TempDir()
	writeTestConfig(t, root)
	dir := filepath.Join(root, "coderenga.d")
	llm := fmt.Sprintf(`{"version":1,"profiles":{"local":{"baseURL":%q,"model":"test"}}}`, server.URL)
	if err := os.WriteFile(filepath.Join(dir, "llm.json"), []byte(llm), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools.json"), []byte(`{"version":1,"policies":{"builtin.write_file":"allow"},"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "modes", "debug.md"), []byte("---\nname: debug\nwrite: confirm\nshell: policy\nmcp: true\n---\ndebug"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := storage.Bootstrap(filepath.Join(dir, "coderenga.db")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "--mode", "debug", "--non-interactive", "write debug.txt"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root})
	if code != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "Execute builtin.write_file?") {
		t.Fatalf("prompted: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "coderenga: operation requires confirmation, but --non-interactive is enabled.") || !strings.Contains(stderr.String(), "tool: builtin.write_file") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "debug.txt")); !os.IsNotExist(err) {
		t.Fatalf("write occurred: %v", err)
	}
}

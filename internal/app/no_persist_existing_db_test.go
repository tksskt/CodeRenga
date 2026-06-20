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

func TestNoPersistHelloDoesNotModifyExistingDatabaseOrRunTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"{\"tool\":\"builtin.write_file\",\"arguments\":{\"path\":\"acenga.d\",\"content\":\"unexpected\"}}"}}]}`)
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
	dbPath := filepath.Join(dir, "coderenga.db")
	if err := storage.Bootstrap(dbPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "--no-persist", "hello"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	after, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("database changed: before=%d/%s after=%d/%s", before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
	if strings.Contains(stdout.String(), "Execute builtin.write_file") || strings.Contains(stdout.String(), "tool loop exceeded") {
		t.Fatalf("unexpected tool behavior: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Hello!") {
		t.Fatalf("expected natural response: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "acenga.d")); !os.IsNotExist(err) {
		t.Fatalf("unexpected file: %v", err)
	}
}

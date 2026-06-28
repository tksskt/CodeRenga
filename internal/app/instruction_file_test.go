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
	"testing"

	"github.com/tks/coderenga/internal/storage"
)

func TestInstructionFileIsAppendedToInstruction(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(body)
		requestBody = string(encoded)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"done"}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	root := t.TempDir()
	writeTestConfig(t, root)
	dir := filepath.Join(root, "coderenga.d")
	if err := os.WriteFile(filepath.Join(dir, "llm.json"), []byte(fmt.Sprintf(`{"version":1,"profiles":{"local":{"baseURL":%q,"model":"test"}}}`, server.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools.json"), []byte(`{"version":1,"tool_policy":{},"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := storage.Bootstrap(filepath.Join(dir, "coderenga.db")); err != nil {
		t.Fatal(err)
	}
	instructionFile := filepath.Join(root, "task.txt")
	if err := os.WriteFile(instructionFile, []byte("file task body"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "--instruction-file", instructionFile, "prefix task"}, strings.NewReader(""), &stdout, &stderr, Options{ExecutableDir: root})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(requestBody, "prefix task") || !strings.Contains(requestBody, "file task body") {
		t.Fatalf("request body missing instruction content: %s", requestBody)
	}
}

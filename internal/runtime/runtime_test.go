package runtime

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeStreamsAndManagesTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"done"}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()
	root := t.TempDir()
	dir := filepath.Join(root, "coderenga.d")
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "modes"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"version":1,"defaultMode":"coder","defaultProfile":"test","state":{"database":"coderenga.db"}}`
	llm := fmt.Sprintf(`{"version":1,"profiles":{"test":{"baseURL":%q,"model":"m"}}}`, server.URL)
	for name, content := range map[string]string{"config.json": config, "llm.json": llm, "prompts/default.md": "system", "prompts/compact.md": "compact", "modes/coder.md": "---\nname: coder\nwrite: confirm\nshell: policy\nmcp: true\n---\ncode"} {
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
	if err = rt.RunInstruction(context.Background(), "hello", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "done") {
		t.Fatalf("output=%q", out.String())
	}
	out.Reset()
	if _, err = rt.Handle(context.Background(), "/tool disable builtin.read_file", &out); err != nil {
		t.Fatal(err)
	}
	if rt.Registry.Enabled("builtin.read_file") {
		t.Fatal("tool was not disabled")
	}
	if _, err = rt.Handle(context.Background(), "/session list", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), rt.SessionID) {
		t.Fatalf("sessions=%q", out.String())
	}
}

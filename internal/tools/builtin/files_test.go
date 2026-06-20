package builtin

import (
	"context"
	"github.com/tks/coderenga/internal/tools"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRejectsOutsideCWD(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x")
	os.WriteFile(outside, []byte("x"), 0644)
	_, e := read(context.Background(), tools.Request{Arguments: map[string]any{"path": outside}, Context: tools.Context{CWD: root}})
	if e == nil {
		t.Fatal("expected sandbox rejection")
	}
}
func TestDryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x")
	_, e := write(context.Background(), tools.Request{Arguments: map[string]any{"path": "x", "content": "x"}, Context: tools.Context{CWD: root, DryRun: true}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("dry-run wrote file")
	}
}

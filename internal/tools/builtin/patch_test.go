package builtin

import (
	"context"
	"github.com/tks/coderenga/internal/tools"
	"os"
	"path/filepath"
	"testing"
)

func TestUnifiedPatch(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	if e := os.WriteFile(p, []byte("one\ntwo\n"), 0644); e != nil {
		t.Fatal(e)
	}
	diff := "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+three\n"
	res, e := applyPatch(context.Background(), tools.Request{Arguments: map[string]any{"patch": diff}, Context: tools.Context{CWD: root}})
	if e != nil || !res.OK {
		t.Fatalf("result=%#v err=%v", res, e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "one\nthree\n" {
		t.Fatalf("content=%q", b)
	}
}

package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, err bytes.Buffer
	if c := Run([]string{"--version"}, strings.NewReader(""), &out, &err, Options{Version: "test"}); c != 0 || out.String() != "coderenga test\n" {
		t.Fatalf("code=%d out=%q err=%q", c, out.String(), err.String())
	}
}

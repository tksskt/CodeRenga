package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseNonInteractive(t *testing.T) {
	got, err := Parse([]string{"--non-interactive", "hello"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.NonInteractive || got.Instruction != "hello" {
		t.Fatalf("options=%#v", got)
	}
}

func TestHelpDocumentsNonInteractive(t *testing.T) {
	var out bytes.Buffer
	WriteHelp(&out)
	if !strings.Contains(out.String(), "--non-interactive") {
		t.Fatalf("help=%q", out.String())
	}
}

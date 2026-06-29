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
	if !strings.Contains(out.String(), "--non-interactive") || !strings.Contains(out.String(), "--auto-approve") {
		t.Fatalf("help=%q", out.String())
	}
}

func TestParseAutoApprove(t *testing.T) {
	got, err := Parse([]string{"--non-interactive", "--auto-approve", "read,write", "--auto-approve", "shell", "hello"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AutoApprove) != 2 || got.AutoApprove[0] != "read,write" || got.AutoApprove[1] != "shell" {
		t.Fatalf("autoApprove=%#v", got.AutoApprove)
	}
}

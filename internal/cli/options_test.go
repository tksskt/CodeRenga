package cli

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseInstructionAndCWD(t *testing.T) {
	got, err := Parse([]string{"--cwd", "project", "review", "this"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != "project" || got.Instruction != "review this" {
		t.Fatalf("unexpected options: %#v", got)
	}
}

func TestParseHelp(t *testing.T) {
	var out bytes.Buffer
	_, err := Parse([]string{"--help"}, &out)
	if !errors.Is(err, ErrHelp) {
		t.Fatalf("expected ErrHelp, got %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("help output was empty")
	}
}

func TestInitRejectsInstruction(t *testing.T) {
	_, err := Parse([]string{"--init", "do work"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error")
	}
}

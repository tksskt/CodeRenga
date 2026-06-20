package tools

import (
	"strings"
	"testing"
)

func TestParserAcceptsJSONToolCall(t *testing.T) {
	calls, err := ParseCalls(`{"tool":"builtin.read_file","arguments":{"path":"README.md"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "builtin.read_file" || calls[0].Arguments["path"] != "README.md" {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestParserRejectsMalformedToolCallClearly(t *testing.T) {
	_, err := ParseCalls(`{"tool":builtin.read_file}`)
	if err == nil || !strings.Contains(err.Error(), "invalid tool call") {
		t.Fatalf("err=%v", err)
	}
}

func TestParserDoesNotExecuteOrdinaryJSONObject(t *testing.T) {
	calls, err := ParseCalls(`{"answer":"done"}`)
	if err != nil || len(calls) != 0 {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
}

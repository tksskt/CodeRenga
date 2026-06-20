package tools

import "testing"

func TestPolicyMaximum(t *testing.T) {
	if Max(Allow, Block, Confirm) != Block {
		t.Fatal("block must win")
	}
}
func TestParserRequiresQualifiedName(t *testing.T) {
	if _, e := ParseCalls(`<tool>{"name":"read_file","arguments":{}}</tool>`); e == nil {
		t.Fatal("expected error")
	}
}

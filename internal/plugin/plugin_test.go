package plugin

import (
	"context"
	"github.com/tks/coderenga/internal/tools"
	"testing"
)

func TestRequiredSandboxRefusesWithoutBackend(t *testing.T) {
	var m Manifest
	m.Name = "x"
	m.Sandbox.Required = true
	r, e := Tool{Manifest: m}.Execute(context.Background(), tools.Request{})
	if e != nil || r.OK || r.Error == "" {
		t.Fatalf("result=%#v err=%v", r, e)
	}
}

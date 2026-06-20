package shell

import (
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/tools"
	"testing"
)

func TestCompoundUsesMaximumRisk(t *testing.T) {
	s, e := Split("git status; rm file")
	if e != nil {
		t.Fatal(e)
	}
	p := config.ShellPolicy{Unknown: "confirm", Allow: []config.ShellRule{{Cmd: "git", Args: []string{"status"}, Match: "argv_prefix"}}, Confirm: []config.ShellRule{{Cmd: "rm", Match: "argv_prefix"}}}
	if got := Evaluate(p, s); got != tools.Confirm {
		t.Fatalf("got %s", got)
	}
}
func TestCommandSubstitutionRejected(t *testing.T) {
	if _, e := Split("echo $(whoami)"); e == nil {
		t.Fatal("expected rejection")
	}
}

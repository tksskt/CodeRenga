package git

import (
	"bytes"
	"context"
	"github.com/tks/coderenga/internal/tools"
	"os/exec"
)

type Tool struct {
	name string
	args []string
}

func (t Tool) Name() string        { return t.name }
func (t Tool) Description() string { return t.name }
func (t Tool) Policy() tools.Level { return tools.Allow }
func (t Tool) Execute(ctx context.Context, r tools.Request) (tools.Result, error) {
	cmd := exec.CommandContext(ctx, "git", t.args...)
	cmd.Dir = r.Context.CWD
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	e := cmd.Run()
	res := tools.Result{OK: e == nil, Content: out.String(), Metadata: map[string]any{"stderr": errout.String()}}
	if e != nil {
		res.Error = e.Error()
	}
	return res, nil
}
func Register(r *tools.Registry) error {
	if e := r.Register(Tool{"git.status", []string{"status", "--short"}}); e != nil {
		return e
	}
	return r.Register(Tool{"git.diff", []string{"diff", "--"}})
}

package ui

import (
	"bufio"
	"context"
	"fmt"
	coderengaruntime "github.com/tks/coderenga/internal/runtime"
	"io"
	"strings"
)

func RunREPL(ctx context.Context, input io.Reader, output io.Writer, rt *coderengaruntime.Runtime) int {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	rt.Approve = func(name string, args map[string]any) bool {
		fmt.Fprintf(output, "Execute %s? [y/N] ", name)
		if !scanner.Scan() {
			return false
		}
		v := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return v == "y" || v == "yes"
	}
	fmt.Fprintf(output, "CodeRenga REPL (%s)\n", rt.CWD)
	for {
		fmt.Fprint(output, "coderenga> ")
		if !scanner.Scan() {
			break
		}
		done, e := rt.Handle(ctx, strings.TrimSpace(scanner.Text()), output)
		if e != nil {
			fmt.Fprintln(output, "error:", e)
		}
		if done {
			return 0
		}
	}
	if e := scanner.Err(); e != nil {
		fmt.Fprintln(output, "input error:", e)
		return 1
	}
	return 0
}

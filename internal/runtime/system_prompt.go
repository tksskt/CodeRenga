package runtime

import (
	"fmt"
	"strings"

	"github.com/tks/coderenga/internal/tools"
)

func (rt *Runtime) systemPrompt() string {
	var b strings.Builder
	b.WriteString(rt.Prompts.Build(rt.Mode))
	b.WriteString("\n\nRuntime policy:\n- Tool names are fully qualified.\n- Use tools only when the request explicitly requires file access, search, modification, shell, Git, MCP, or plugins. Answer greetings and general conversation directly without tools.\n- Never invent a path; use a user-supplied path or discover it with a read-only tool.\n- If a tool result says dry-run or executed=false, state that it was not executed and never claim a file was created, updated, or written.\n- To call a tool, output exactly one JSON object with keys \"tool\" and \"arguments\" and no prose or Markdown fence.\n- Example: {\"tool\":\"builtin.read_file\",\"arguments\":{\"path\":\"README.md\"}}\n- Policy order is block > confirm > unknown > allow.\nAvailable tools:\n")
	for _, name := range rt.Registry.Names() {
		tool, ok := rt.Registry.Info(name)
		if !ok || !rt.Registry.Enabled(name) {
			continue
		}
		if rt.modeDecision(rt.Mode, tool) == tools.Block {
			continue
		}
		if tools.ParseLevel(rt.Config.ToolPolicies[name]) == tools.Block {
			continue
		}
		fmt.Fprintln(&b, "-", name, toolHint(name))
	}
	return strings.TrimSpace(b.String())
}

func toolHint(name string) string {
	switch name {
	case "builtin.read_file":
		return `arguments: {"path":"relative/path"}`
	case "builtin.write_file":
		return `arguments: {"path":"relative/path","content":"text"}`
	case "builtin.apply_patch":
		return `arguments: {"patch":"unified diff"}`
	case "builtin.list_files":
		return `arguments: {"path":"optional/relative/path"}`
	case "builtin.search_text":
		return `arguments: {"pattern":"regular expression"}`
	case "git.status", "git.diff":
		return "arguments: {}"
	case "shell.run":
		return `arguments: {"command":"command text"}`
	default:
		return ""
	}
}

package shell

import (
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/tools"
	"strings"
)

func EvaluateCompound(policy config.ShellPolicy, segments [][]string) tools.Level {
	level := Evaluate(policy, segments)
	for _, rule := range policy.Block {
		if rule.Match != "compound" {
			continue
		}
		for i := 0; i+1 < len(segments); i++ {
			if len(segments[i]) == 0 || len(segments[i+1]) == 0 {
				continue
			}
			left, right := strings.ToLower(segments[i][0]), strings.ToLower(segments[i+1][0])
			if (rule.Pattern == "curl_pipe_sh" && left == "curl" && isShell(right)) || (rule.Pattern == "wget_pipe_sh" && left == "wget" && isShell(right)) {
				return tools.Block
			}
		}
	}
	return level
}
func isShell(command string) bool {
	return command == "sh" || command == "bash" || command == "zsh" || command == "powershell" || command == "powershell.exe"
}

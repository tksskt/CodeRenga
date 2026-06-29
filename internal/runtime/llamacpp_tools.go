package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tks/coderenga/internal/llm"
	"github.com/tks/coderenga/internal/tools"
)

type nativeToolSet struct {
	Tools          []map[string]any
	SafeToInternal map[string]string
}

func (rt *Runtime) nativeToolsEnabled() bool {
	profile, ok := rt.Config.Profiles[rt.Profile]
	return ok && profile.ToolProtocol == "llamacpp_tools"
}

func (rt *Runtime) llamaCppToolSet() (nativeToolSet, error) {
	set := nativeToolSet{SafeToInternal: map[string]string{}}
	for _, name := range rt.Registry.Names() {
		tool, ok := rt.Registry.Info(name)
		if !ok || !rt.Registry.Enabled(name) {
			continue
		}
		if rt.modeDecision(rt.Mode, tool) == tools.Block || tools.ParseLevel(rt.Config.ToolPolicies[name]) == tools.Block {
			continue
		}
		if err := addNativeTool(&set, name, tool); err != nil {
			return nativeToolSet{}, err
		}
	}
	return set, nil
}

func addNativeTool(set *nativeToolSet, name string, tool tools.Tool) error {
	schemaProvider, ok := tool.(tools.SchemaProvider)
	if !ok {
		return nil
	}
	safe := safeToolName(name)
	if prev, exists := set.SafeToInternal[safe]; exists && prev != name {
		return fmt.Errorf("native tool name collision: %q and %q both map to %q", prev, name, safe)
	}
	set.SafeToInternal[safe] = name
	set.Tools = append(set.Tools, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        safe,
			"description": shortDescription(tool.Description()),
			"parameters":  schemaProvider.Schema(),
		},
	})
	return nil
}

func safeToolName(name string) string { return strings.ReplaceAll(name, ".", "__") }

func shortDescription(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "CodeRenga tool."
	}
	if len(value) > 160 {
		return value[:160]
	}
	return value
}

func (rt *Runtime) runLlamaCppTools(ctx context.Context, instruction string, out io.Writer) error {
	if _, err := rt.addMessageNoCompact(ctx, "user", instruction); err != nil {
		return err
	}
	profile, ok := rt.Config.Profiles[rt.Profile]
	if !ok {
		return fmt.Errorf("unknown profile %q", rt.Profile)
	}
	profile.Model = rt.Model
	toolSet, err := rt.llamaCppToolSet()
	if err != nil {
		return err
	}
	profile.NativeTools = toolSet.Tools

	lastSignature := ""
	lastResults := map[string]string{}
	var callHistory []string
	var dryRunSkipped []tools.Request
	loopState := newLoopRuntimeState()
	loopRepairUsed := false
	var nativeHistory []llm.Message
	maxTurns := rt.maxTurns()
turnLoop:
	for turn := 0; turn < maxTurns; turn++ {
		remaining := maxTurns - turn
		if remaining <= 2 {
			if _, err := rt.addMessage(ctx, "user", maxTurnReminder(remaining)); err != nil {
				return err
			}
		}
		msgs, err := rt.context(ctx)
		if err != nil {
			return err
		}
		msgs = append(msgs, nativeHistory...)
		result, err := rt.LLM.ChatResult(ctx, profile, msgs, false, nil)
		if err != nil {
			return err
		}
		rt.recordTranscript(turn, "llm_output", "", "", result.Content, "", result.Raw)
		result.ToolCalls = ensureNativeToolCallIDs(result.ToolCalls)
		if len(result.ToolCalls) == 0 {
			final := result.Content
			if len(dryRunSkipped) > 0 {
				final = dryRunFinal(dryRunSkipped)
			}
			if _, err = rt.addMessage(ctx, "assistant", final); err != nil {
				return err
			}
			fmt.Fprintln(out, final)
			return nil
		}
		assistant := llm.Message{Role: "assistant", Content: result.Content, ToolCalls: result.ToolCalls, ReasoningContent: result.Reasoning}
		if result.Content == "" {
			assistant.Content = nil
		}
		nativeHistory = append(nativeHistory, assistant)
		for i, rawCall := range result.ToolCalls {
			call, callID, err := nativeToolRequest(rawCall, i, toolSet.SafeToInternal)
			if err != nil {
				toolMessage := nativeToolMessage(callID, tools.Result{OK: false, Error: err.Error()})
				nativeHistory = append(nativeHistory, toolMessage)
				rt.recordTranscript(turn, "tool_result", "", "", "", err.Error(), "")
				continue
			}
			signature := toolCallSignature(call)
			if signature == lastSignature {
				if loopRepairUsed {
					rt.recordTranscript(turn, "loop_error", call.Name, signature, lastResults[signature], "repeated_tool_call", "")
					return fmt.Errorf("repeated tool call detected after recovery: %s was requested again immediately after its result; previous result: %s", toolCallSummary(call), lastResults[signature])
				}
				loopRepairUsed = true
				nativeHistory = append(nativeHistory, llm.Message{Role: "tool", ToolCallID: callID, Content: repeatedToolCallRecoveryMessage(call, lastResults[signature])})
				rt.recordTranscript(turn, "recovery", call.Name, signature, lastResults[signature], "repeated_tool_call", "")
				continue turnLoop
			}
			lastSignature = signature
			callHistory = append(callHistory, toolCallSummary(call))
			rt.recordToolStatus(call.Name, ToolCallGenerated)
			rt.recordTranscript(turn, "tool_call", call.Name, signature, "", "", "")
			call.Context = tools.Context{CWD: rt.CWD, Mode: rt.Mode, SessionID: rt.SessionID, DryRun: rt.DryRun}
			rt.recordToolStatus(call.Name, ToolCallRunning)
			res, skipped := loopState.shouldSkipShell(call)
			var execErr error
			if !skipped {
				res, execErr = rt.Executor.Execute(ctx, call)
				if execErr != nil {
					return execErr
				}
			}
			lastResults[signature] = toolResultSummary(res)
			status := ToolCallDone
			if !res.OK {
				status = ToolCallFailed
				if strings.Contains(res.Error, "blocked by policy") {
					status = ToolCallBlocked
				}
			}
			rt.recordToolStatus(call.Name, status)
			rt.recordTranscript(turn, "tool_result", call.Name, signature, toolResultSummary(res), res.Error, "")
			reminders := loopState.afterTool(call, res)
			nativeHistory = append(nativeHistory, nativeToolMessage(callID, res))
			for _, reminder := range reminders {
				nativeHistory = append(nativeHistory, llm.Message{Role: "user", Content: reminder})
			}
			if rt.DryRun && rt.isSideEffectTool(call.Name) {
				dryRunSkipped = append(dryRunSkipped, call)
				fmt.Fprintf(out, "[dry-run] %s\n", call.Name)
				printToolArguments(out, call.Arguments)
			}
		}
	}
	return maxTurnExceededError(maxTurns, callHistory, loopState)
}

func nativeToolRequest(raw llm.ToolCall, index int, safeToInternal map[string]string) (tools.Request, string, error) {
	callID := raw.ID
	if callID == "" {
		callID = fmt.Sprintf("call_%d", index)
	}
	name := raw.Function.Name
	arguments := any(raw.Function.Arguments)
	if name == "" {
		name = raw.Name
		arguments = raw.Arguments
	}
	internal, ok := safeToInternal[name]
	if !ok {
		return tools.Request{}, callID, fmt.Errorf("unknown native tool name %q", name)
	}
	args, err := nativeArguments(arguments)
	if err != nil {
		return tools.Request{}, callID, fmt.Errorf("invalid arguments for %s: %w", internal, err)
	}
	return tools.Request{Name: internal, Arguments: args}, callID, nil
}

func nativeArguments(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return map[string]any{}, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[string]any{}
		}
		return out, nil
	case map[string]any:
		return v, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err = json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func nativeToolMessage(callID string, res tools.Result) llm.Message {
	body, _ := json.Marshal(res)
	return llm.Message{Role: "tool", ToolCallID: callID, Content: string(body)}
}
func ensureNativeToolCallIDs(calls []llm.ToolCall) []llm.ToolCall {
	out := make([]llm.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if out[i].ID == "" {
			out[i].ID = fmt.Sprintf("call_%d", i)
		}
		if out[i].Type == "" {
			out[i].Type = "function"
		}
	}
	return out
}

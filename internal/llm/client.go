package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/config"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

type ToolCall struct {
	ID        string       `json:"id,omitempty"`
	Type      string       `json:"type,omitempty"`
	Function  ToolFunction `json:"function,omitempty"`
	Name      string       `json:"name,omitempty"`
	Arguments any          `json:"arguments,omitempty"`
}

type ToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ChatResult struct {
	Content      string
	ToolCalls    []ToolCall
	Reasoning    string
	Raw          string
	FinishReason string
}

type Client struct{ HTTP *http.Client }

func New() *Client { return &Client{HTTP: &http.Client{Timeout: 5 * time.Minute}} }

func (c *Client) Chat(ctx context.Context, p config.Profile, msgs []Message, stream bool, onDelta func(string) error) (string, error) {
	result, err := c.ChatResult(ctx, p, msgs, stream, onDelta)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c *Client) ChatResult(ctx context.Context, p config.Profile, msgs []Message, stream bool, onDelta func(string) error) (ChatResult, error) {
	requestStream := stream
	body := map[string]any{"model": p.Model, "messages": msgs, "stream": requestStream, "temperature": p.Temperature}
	if p.MaxTokens > 0 {
		body["max_tokens"] = p.MaxTokens
	}
	for k, v := range p.ExtraBody {
		if isReservedChatBodyKey(k) {
			// ExtraBody is for provider-specific additive parameters only; CodeRenga-owned request fields are forced below.
			continue
		}
		body[k] = v
	}
	if p.ToolProtocol == "llamacpp_tools" {
		requestStream = false
		body["stream"] = false
		if len(p.NativeTools) > 0 {
			body["tools"] = p.NativeTools
			body["tool_choice"] = defaultToolChoice(p.ToolChoice)
			body["parallel_tool_calls"] = false
		}
	}
	b, _ := json.Marshal(body)
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, e := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if e != nil {
		return ChatResult{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return ChatResult{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		x, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return ChatResult{}, fmt.Errorf("chat completions: %s: %s", resp.Status, strings.TrimSpace(string(x)))
	}
	if !requestStream {
		return decodeNonStream(resp.Body)
	}
	return decodeStream(resp.Body, onDelta)
}

func isReservedChatBodyKey(k string) bool {
	switch k {
	case "model", "messages", "stream", "tools", "tool_choice", "parallel_tool_calls":
		return true
	default:
		return false
	}
}
func defaultToolChoice(value string) string {
	switch strings.TrimSpace(value) {
	case "none", "required":
		return value
	default:
		return "auto"
	}
}

func decodeNonStream(r io.Reader) (ChatResult, error) {
	var v struct {
		Choices []struct {
			Message struct {
				Content          any        `json:"content"`
				ToolCalls        []ToolCall `json:"tool_calls"`
				ReasoningContent string     `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return ChatResult{}, err
	}
	if e := json.Unmarshal(b, &v); e != nil {
		return ChatResult{}, e
	}
	if len(v.Choices) == 0 {
		return ChatResult{}, fmt.Errorf("chat completions returned no choices")
	}
	choice := v.Choices[0]
	return ChatResult{Content: contentString(choice.Message.Content), ToolCalls: choice.Message.ToolCalls, Reasoning: choice.Message.ReasoningContent, Raw: string(b), FinishReason: choice.FinishReason}, nil
}

func decodeStream(r io.Reader, onDelta func(string) error) (ChatResult, error) {
	var out strings.Builder
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &v) != nil || len(v.Choices) == 0 {
			continue
		}
		d := v.Choices[0].Delta.Content
		out.WriteString(d)
		if onDelta != nil {
			if e := onDelta(d); e != nil {
				return ChatResult{}, e
			}
		}
	}
	if err := s.Err(); err != nil {
		return ChatResult{}, err
	}
	return ChatResult{Content: out.String()}, nil
}

func contentString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

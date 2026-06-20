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
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Client struct{ HTTP *http.Client }

func New() *Client { return &Client{HTTP: &http.Client{Timeout: 5 * time.Minute}} }
func (c *Client) Chat(ctx context.Context, p config.Profile, msgs []Message, stream bool, onDelta func(string) error) (string, error) {
	body := map[string]any{"model": p.Model, "messages": msgs, "stream": stream, "temperature": p.Temperature}
	if p.MaxTokens > 0 {
		body["max_tokens"] = p.MaxTokens
	}
	b, _ := json.Marshal(body)
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, e := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		x, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", fmt.Errorf("chat completions: %s: %s", resp.Status, strings.TrimSpace(string(x)))
	}
	if !stream {
		var v struct {
			Choices []struct {
				Message Message `json:"message"`
			} `json:"choices"`
		}
		if e = json.NewDecoder(resp.Body).Decode(&v); e != nil {
			return "", e
		}
		if len(v.Choices) == 0 {
			return "", fmt.Errorf("chat completions returned no choices")
		}
		return v.Choices[0].Message.Content, nil
	}
	var out strings.Builder
	s := bufio.NewScanner(resp.Body)
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
			if e = onDelta(d); e != nil {
				return "", e
			}
		}
	}
	return out.String(), s.Err()
}

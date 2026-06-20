package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type SSEClient struct {
	endpoint string
	http     *http.Client
	body     io.ReadCloser
	mu       sync.Mutex
	nextID   int
	pending  map[int]chan rpcResp
}

func NewSSE(ctx context.Context, rawURL string) (Client, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	h := &http.Client{}
	resp, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("mcp SSE: %s", resp.Status)
	}
	c := &SSEClient{http: h, body: resp.Body, pending: map[int]chan rpcResp{}}
	ready := make(chan error, 1)
	go c.readLoop(rawURL, ready)
	select {
	case err = <-ready:
		if err != nil {
			c.Close()
			return nil, err
		}
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	}
	return c, nil
}

func (c *SSEClient) readLoop(base string, ready chan<- error) {
	scanner := bufio.NewScanner(c.body)
	event, data := "", ""
	readySent := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "":
			if event == "endpoint" && !readySent {
				u, err := url.Parse(data)
				if err == nil {
					b, _ := url.Parse(base)
					u = b.ResolveReference(u)
					c.endpoint = u.String()
				}
				if c.endpoint == "" {
					ready <- fmt.Errorf("MCP SSE did not provide an endpoint")
				} else {
					ready <- nil
				}
				readySent = true
			} else if data != "" {
				c.deliver([]byte(data))
			}
			event, data = "", ""
		}
	}
	if !readySent {
		ready <- fmt.Errorf("MCP SSE closed before endpoint event")
	}
}
func (c *SSEClient) deliver(data []byte) {
	var v struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &v) != nil {
		return
	}
	c.mu.Lock()
	ch := c.pending[v.ID]
	delete(c.pending, v.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- rpcResp{Result: v.Result, Error: v.Error}
	}
}
func (c *SSEClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResp, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	payload, _ := json.Marshal(rpcReq{"2.0", id, method, params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("mcp SSE POST: %s", resp.Status)
	}
	select {
	case v := <-ch:
		if v.Error != nil {
			return nil, fmt.Errorf("mcp: %s", v.Error.Message)
		}
		return v.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (c *SSEClient) Notify(ctx context.Context, method string, params any) error {
	payload := notification(method, params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mcp SSE notification: %s", resp.Status)
	}
	return nil
}
func (c *SSEClient) Close() error { return c.body.Close() }

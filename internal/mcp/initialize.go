package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func Initialize(ctx context.Context, c Client) error {
	_, e := c.Call(ctx, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "coderenga", "version": "0.1.0"}})
	if e != nil {
		return e
	}
	type notifier interface {
		Notify(context.Context, string, any) error
	}
	if n, ok := c.(notifier); ok {
		return n.Notify(ctx, "notifications/initialized", map[string]any{})
	}
	return nil
}
func notification(method string, params any) []byte {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	return b
}
func (s *StdioClient) Notify(_ context.Context, method string, params any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e := s.in.Write(append(notification(method, params), '\n'))
	return e
}
func (h *HTTPClient) Notify(ctx context.Context, method string, params any) error {
	req, e := http.NewRequestWithContext(ctx, "POST", h.URL, bytes.NewReader(notification(method, params)))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := h.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mcp notification: %s", resp.Status)
	}
	return nil
}

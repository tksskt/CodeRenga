package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type HTTPClient struct {
	URL  string
	HTTP *http.Client
}

func (h *HTTPClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	b, _ := json.Marshal(rpcReq{"2.0", 1, method, params})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := h.HTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("mcp HTTP: %s", resp.Status)
	}
	var v rpcResp
	if e = json.NewDecoder(resp.Body).Decode(&v); e != nil {
		return nil, e
	}
	if v.Error != nil {
		return nil, fmt.Errorf("mcp: %s", v.Error.Message)
	}
	return v.Result, nil
}
func (h *HTTPClient) Close() error { return nil }

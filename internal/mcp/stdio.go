package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

type StdioClient struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	scan *bufio.Scanner
	mu   sync.Mutex
	id   int
}

func NewStdio(ctx context.Context, command string, args []string) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	return &StdioClient{cmd: cmd, in: in, scan: scanner}, nil
}
func (s *StdioClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id++
	b, _ := json.Marshal(rpcReq{"2.0", s.id, method, params})
	if _, e := s.in.Write(append(b, '\n')); e != nil {
		return nil, e
	}
	for s.scan.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		var v rpcResp
		if json.Unmarshal(s.scan.Bytes(), &v) != nil {
			continue
		}
		if v.Error != nil {
			return nil, fmt.Errorf("mcp: %s", v.Error.Message)
		}
		return v.Result, nil
	}
	return nil, s.scan.Err()
}
func (s *StdioClient) Close() error { _ = s.in.Close(); return s.cmd.Wait() }

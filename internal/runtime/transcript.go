package runtime

import (
	"crypto/sha256"
	"encoding/hex"
)

type TranscriptEntry struct {
	Turn          int
	Kind          string
	Tool          string
	ArgumentsHash string
	ResultSummary string
	ErrorKind     string
	PolicyLevel   string
}

type ToolCallStatus string

const (
	ToolCallGenerated        ToolCallStatus = "generated"
	ToolCallValidated        ToolCallStatus = "validated"
	ToolCallBlocked          ToolCallStatus = "blocked"
	ToolCallAwaitingApproval ToolCallStatus = "awaiting_approval"
	ToolCallRunning          ToolCallStatus = "running"
	ToolCallDone             ToolCallStatus = "done"
	ToolCallFailed           ToolCallStatus = "failed"
	ToolCallCanceled         ToolCallStatus = "canceled"
)

type ToolCallRecord struct {
	Tool   string
	Status ToolCallStatus
}

func (rt *Runtime) recordTranscript(turn int, kind, tool, args, result, errorKind, policy string) {
	entry := TranscriptEntry{Turn: turn, Kind: kind, Tool: tool, ResultSummary: limitText(result, 256), ErrorKind: errorKind, PolicyLevel: policy}
	if args != "" {
		sum := sha256.Sum256([]byte(args))
		entry.ArgumentsHash = hex.EncodeToString(sum[:])
	}
	rt.Transcript = append(rt.Transcript, entry)
}

func (rt *Runtime) recordToolStatus(tool string, status ToolCallStatus) {
	rt.ToolCalls = append(rt.ToolCalls, ToolCallRecord{Tool: tool, Status: status})
}

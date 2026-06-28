package policy

import "fmt"

type RepetitionInspector struct {
	LastTool string
	LastHash string
}

func (r *RepetitionInspector) Inspect(req Request) Result {
	hash := req.Tool
	for key, value := range req.Arguments {
		hash += "|" + key + "=" + fmt.Sprint(value)
	}
	if req.Tool != "" && req.Tool == r.LastTool && hash == r.LastHash {
		return Result{Decision: Reject, Reason: "repeated tool call"}
	}
	r.LastTool, r.LastHash = req.Tool, hash
	return Result{Decision: AutoApprove, Reason: "not repeated"}
}

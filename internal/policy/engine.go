package policy

type Decision int

const (
	AutoApprove Decision = iota
	Unknown
	AskUser
	Reject
)

func (d Decision) String() string {
	return [...]string{"auto_approve", "unknown", "ask_user", "reject"}[d]
}

type Result struct {
	Decision Decision
	Reason   string
}

type Request struct {
	Tool      string
	Arguments map[string]any
	Mode      string
	CWD       string
}

type Inspector interface {
	Inspect(Request) Result
}

type InspectorFunc func(Request) Result

func (f InspectorFunc) Inspect(req Request) Result { return f(req) }

type Engine struct {
	Inspectors []Inspector
}

func (e Engine) Decide(req Request) (Decision, []Result) {
	decision := AutoApprove
	results := make([]Result, 0, len(e.Inspectors))
	for _, inspector := range e.Inspectors {
		result := inspector.Inspect(req)
		results = append(results, result)
		if result.Decision > decision {
			decision = result.Decision
		}
	}
	return decision, results
}

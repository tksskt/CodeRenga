package tools

func (r *Registry) Replace(t Tool) error {
	if !qualified(t.Name()) {
		return RegisterNameError(t.Name())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[t.Name()] = t
	return nil
}
func RegisterNameError(name string) error { return &registryNameError{name: name} }

type registryNameError struct{ name string }

func (e *registryNameError) Error() string { return "tool name is not fully qualified: " + e.name }

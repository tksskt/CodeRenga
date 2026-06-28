package tools

import (
	"fmt"
	"strings"
)

func (r *Registry) RegisterDynamic(t Tool) error {
	name := t.Name()
	if !qualified(name) {
		return RegisterNameError(name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[name]; exists {
		return fmt.Errorf("duplicate tool %q", name)
	}
	r.items[name] = t
	return nil
}

func (r *Registry) Replace(t Tool) error {
	name := t.Name()
	if !qualified(name) {
		return RegisterNameError(name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[name]; exists && !replaceableDynamicName(name) {
		return fmt.Errorf("duplicate tool %q", name)
	}
	r.items[name] = t
	return nil
}

func (r *Registry) RemovePrefix(prefix string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := []string{}
	for name := range r.items {
		if strings.HasPrefix(name, prefix) {
			delete(r.items, name)
			delete(r.disabled, name)
			removed = append(removed, name)
		}
	}
	return removed
}
func replaceableDynamicName(name string) bool {
	return strings.HasPrefix(name, "plugin.") || strings.HasPrefix(name, "mcp.")
}

func RegisterNameError(name string) error { return &registryNameError{name: name} }

type registryNameError struct{ name string }

func (e *registryNameError) Error() string { return "tool name is not fully qualified: " + e.name }

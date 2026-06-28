package tools

import (
	"context"
	"strings"
	"testing"
)

type registryTestTool string

func (t registryTestTool) Name() string        { return string(t) }
func (t registryTestTool) Description() string { return string(t) }
func (t registryTestTool) Policy() Level       { return Allow }
func (t registryTestTool) Execute(context.Context, Request) (Result, error) {
	return Result{OK: true}, nil
}

func TestReplaceRefusesDuplicateTool(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(registryTestTool("builtin.read_file")); err != nil {
		t.Fatal(err)
	}
	err := r.Replace(registryTestTool("builtin.read_file"))
	if err == nil || !strings.Contains(err.Error(), "duplicate tool") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegisterDynamicRefusesExistingPluginTool(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDynamic(registryTestTool("plugin.demo")); err != nil {
		t.Fatal(err)
	}
	err := r.RegisterDynamic(registryTestTool("plugin.demo"))
	if err == nil || !strings.Contains(err.Error(), "duplicate tool") {
		t.Fatalf("err=%v", err)
	}
}

func TestReplaceUpdatesExistingPluginTool(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterDynamic(registryTestTool("plugin.demo")); err != nil {
		t.Fatal(err)
	}
	if err := r.Replace(registryTestTool("plugin.demo")); err != nil {
		t.Fatal(err)
	}
}

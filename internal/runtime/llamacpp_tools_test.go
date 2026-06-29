package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/tools"
)

type nativeSchemaTool struct{ name string }

func (t nativeSchemaTool) Name() string        { return t.name }
func (t nativeSchemaTool) Description() string { return "test schema tool" }
func (t nativeSchemaTool) Policy() tools.Level { return tools.Allow }
func (t nativeSchemaTool) Execute(context.Context, tools.Request) (tools.Result, error) {
	return tools.Result{OK: true}, nil
}
func (t nativeSchemaTool) Schema() map[string]any { return tools.ObjectSchema(nil, nil) }

func TestAddNativeToolDetectsSafeNameCollision(t *testing.T) {
	set := nativeToolSet{SafeToInternal: map[string]string{}}
	if err := addNativeTool(&set, "foo.bar", nativeSchemaTool{name: "foo.bar"}); err != nil {
		t.Fatal(err)
	}
	err := addNativeTool(&set, "foo__bar", nativeSchemaTool{name: "foo__bar"})
	if err == nil || !strings.Contains(err.Error(), `native tool name collision: "foo.bar" and "foo__bar" both map to "foo__bar"`) {
		t.Fatalf("err=%v", err)
	}
}

package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type interruptBlockTool struct{}

func (interruptBlockTool) Name() string                  { return "blocktool" }
func (interruptBlockTool) Description() string           { return "" }
func (interruptBlockTool) Parameters() map[string]any    { return nil }
func (interruptBlockTool) Execute(context.Context, map[string]any) *tools.Result {
	return nil
}
func (interruptBlockTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptBlock }

func TestToolExecutionContext_BlockUsesWithoutCancel(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(interruptBlockTool{})
	l := &Loop{tools: reg, id: "agent"}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := l.toolExecutionContext(parent, "blocktool", nil)
	if ctx.Err() != nil {
		t.Fatalf("block tool ctx should not inherit cancel: %v", ctx.Err())
	}
}

func TestToolExecutionContext_CancelPropagates(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.NewExecTool("", false)) // default InterruptCancel
	l := &Loop{tools: reg, id: "agent"}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := l.toolExecutionContext(parent, "exec", nil)
	if ctx.Err() == nil {
		t.Fatal("cancel tool should inherit parent cancellation")
	}
}

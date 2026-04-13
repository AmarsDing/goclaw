package permissions

import (
	"context"
	"testing"
)

func TestResolveApproval_HeadlessDeniesAsk(t *testing.T) {
	g := NewGovernor()
	dec := &PermissionDecision{
		Action:         ActionAsk,
		Allowed:        false,
		Reason:         "mode asks",
		Classification: &SecurityClassification{Class: ClassExecute, Risk: "high"},
		Source:         SourceMode,
	}
	out, err := g.ResolveApproval(context.Background(), PermissionRequest{
		AgentID:  "a1",
		ToolName: "exec",
	}, dec)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Action != ActionDeny {
		t.Fatalf("expected deny, got %+v", out)
	}
	if out.Source != SourceHeadless {
		t.Fatalf("source = %q, want headless", out.Source)
	}
}

package tools

import "testing"

func TestExecConcurrencySafeCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"ls -la", true},
		{"ls", true},
		{"", false},
		{"  ", false},
		{"rm -rf /", false},
		{"cat foo | grep bar", true},
		{"cat foo | rm bar", false},
		{"echo hello", true},
		{"git log -1", true},
		{"git push", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := execConcurrencySafeCommand(tt.cmd)
			if got != tt.want {
				t.Fatalf("execConcurrencySafeCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestExecTool_IsConcurrencySafeWithArgs(t *testing.T) {
	tool := NewExecTool("/tmp", false)
	if !tool.IsConcurrencySafeWithArgs(map[string]any{"command": "ls -la"}) {
		t.Fatal("expected ls -la safe")
	}
	if tool.IsConcurrencySafeWithArgs(map[string]any{"command": "rm -rf x"}) {
		t.Fatal("expected rm unsafe")
	}
	if tool.IsConcurrencySafeWithArgs(nil) {
		t.Fatal("nil args should be unsafe")
	}
}

func TestRegistry_InterruptBehaviorFor_exec(t *testing.T) {
	r := NewRegistry()
	r.Register(NewExecTool(t.TempDir(), false))
	if got := r.InterruptBehaviorFor("exec"); got != InterruptCancel {
		t.Fatalf("exec interrupt = %q, want cancel", got)
	}
}

func TestRegistry_InterruptBehaviorFor_read_file(t *testing.T) {
	r := NewRegistry()
	r.Register(NewReadFileTool(t.TempDir(), false))
	if got := r.InterruptBehaviorFor("read_file"); got != InterruptBlock {
		t.Fatalf("read_file interrupt = %q, want block", got)
	}
}

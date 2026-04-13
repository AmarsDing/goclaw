package permissions

import (
	"context"
	"testing"
)

func TestDefaultSafetyChecker_paths(t *testing.T) {
	chk := DefaultSafetyChecker()
	ctx := context.Background()
	class := &SecurityClassification{Class: ClassReadOnly, Risk: "low"}

	tests := []struct {
		name   string
		req    PermissionRequest
		wantDeny bool
	}{
		{
			name: "ssh_private_key",
			req: PermissionRequest{
				ToolName:  "read_file",
				Arguments: map[string]any{"path": "/home/u/.ssh/id_rsa"},
			},
			wantDeny: true,
		},
		{
			name: "dot_env",
			req: PermissionRequest{
				ToolName:  "read_file",
				Arguments: map[string]any{"path": "project/.env.local"},
			},
			wantDeny: true,
		},
		{
			name: "normal_file",
			req: PermissionRequest{
				ToolName:  "read_file",
				Arguments: map[string]any{"path": "src/main.go"},
			},
			wantDeny: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := chk(ctx, tt.req, class)
			if err != nil {
				t.Fatal(err)
			}
			deny := v != nil && v.Action == ActionDeny
			if deny != tt.wantDeny {
				t.Fatalf("deny=%v, wantDeny=%v verdict=%+v", deny, tt.wantDeny, v)
			}
		})
	}
}

func TestDefaultSafetyChecker_exec(t *testing.T) {
	chk := DefaultSafetyChecker()
	ctx := context.Background()
	class := &SecurityClassification{Class: ClassExecute, Risk: "high"}

	v, err := chk(ctx, PermissionRequest{
		ToolName:  "exec",
		Arguments: map[string]any{"command": "sudo ls"},
	}, class)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || v.Action != ActionDeny {
		t.Fatalf("expected deny for sudo, got %+v", v)
	}

	v, err = chk(ctx, PermissionRequest{
		ToolName:  "exec",
		Arguments: map[string]any{"command": "ls -la"},
	}, class)
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("expected pass for ls, got %+v", v)
	}
}

func TestDefaultSafetyChecker_windowsPath(t *testing.T) {
	chk := DefaultSafetyChecker()
	ctx := context.Background()
	class := &SecurityClassification{Class: ClassReadOnly, Risk: "low"}

	v, err := chk(ctx, PermissionRequest{
		ToolName:  "read_file",
		Arguments: map[string]any{"path": `C:\Users\me\.ssh\id_rsa`},
	}, class)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil || v.Action != ActionDeny {
		t.Fatalf("expected deny for Windows ssh key path, got %+v", v)
	}
}

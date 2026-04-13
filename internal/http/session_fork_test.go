package http

import (
	"strings"
	"testing"
)

func TestSanitizeForkLabel(t *testing.T) {
	t.Parallel()
	if got := sanitizeForkLabel(""); len(got) != 8 {
		t.Fatalf("empty label: want len 8, got %q len %d", got, len(got))
	}
	if got := sanitizeForkLabel("  my-branch  "); got != "my-branch" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeForkLabel("a b c"); got != "a-b-c" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("x", 60)
	if got := sanitizeForkLabel(long); len(got) != 48 {
		t.Fatalf("truncation: len %d", len(got))
	}
}

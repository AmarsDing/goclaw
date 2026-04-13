package neterr

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestClassify_ContextTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	d := Classify(ctx.Err())
	if d.Kind != KindTimeout && d.Kind != KindCancelled {
		t.Fatalf("got %s %q", d.Kind, d.Summary)
	}
}

func TestClassify_DNS(t *testing.T) {
	t.Parallel()
	if d := Classify(&net.DNSError{Err: "no such host", IsTimeout: false}); d.Kind != KindDNS {
		t.Fatalf("want dns, got %q", d.Kind)
	}
}

func TestClassify_ConnReset(t *testing.T) {
	t.Parallel()
	d := Classify(syscall.ECONNRESET)
	if !ShouldRetry(d) {
		t.Fatal("expected retry")
	}
}

func TestClassify_Other(t *testing.T) {
	t.Parallel()
	d := Classify(errors.New("validation failed"))
	if d.Kind != KindOther {
		t.Fatalf("got %q", d.Kind)
	}
	if ShouldRetry(d) {
		t.Fatal("should not retry")
	}
}

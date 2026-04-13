package agent

import "testing"

func TestHandoffReview(t *testing.T) {
	ok, reason := HandoffReview("here is -----BEGIN PRIVATE KEY-----")
	if ok || reason == "" {
		t.Fatalf("expected block, ok=%v reason=%q", ok, reason)
	}
	ok, _ = HandoffReview("normal summary")
	if !ok {
		t.Fatal("expected ok for normal text")
	}
}

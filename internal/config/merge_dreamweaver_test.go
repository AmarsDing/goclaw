package config

import "testing"

func TestMergeChain(t *testing.T) {
	a := &DreamWeaverConfig{Enabled: true, CompressionEnabled: false}
	b := &DreamWeaverConfig{CompressionEnabled: true}
	c := &DreamWeaverConfig{HooksEnabled: true}
	out := MergeChain(a, b, c)
	if out == nil || !out.Enabled || !out.CompressionEnabled || !out.HooksEnabled {
		t.Fatalf("unexpected merged config: %+v", out)
	}
	if MergeChain(nil, b) == nil {
		t.Fatal("expected non-nil from MergeChain(nil, b)")
	}
}

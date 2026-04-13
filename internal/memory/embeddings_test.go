package memory

import (
	"context"
	"testing"
)

func TestClampEmbeddingDims(t *testing.T) {
	t.Parallel()
	v := []float32{1, 2, 3, 4, 5}
	got := ClampEmbeddingDims(v, 3)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("truncate: got %v", got)
	}
	short := []float32{1, 2}
	padded := ClampEmbeddingDims(short, 4)
	if len(padded) != 4 || padded[0] != 1 || padded[1] != 2 || padded[2] != 0 || padded[3] != 0 {
		t.Fatalf("pad: got %v", padded)
	}
	same := make([]float32, 1536)
	for i := range same {
		same[i] = 1
	}
	out := ClampEmbeddingDims(same, 1536)
	if len(out) != 1536 {
		t.Fatalf("len = %d", len(out))
	}
	same[0] = 999
	if out[0] != 999 {
		t.Fatal("same length should reuse slice backing")
	}
}

func TestClampEmbeddingProvider(t *testing.T) {
	t.Parallel()
	inner := &stubEmb{out: [][]float32{make([]float32, 2048)}}
	wrapped := NewClampEmbeddingProvider(inner, 1536)
	out, err := wrapped.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0]) != 1536 {
		t.Fatalf("len = %d, want 1536", len(out[0]))
	}
}

type stubEmb struct {
	out [][]float32
}

func (s *stubEmb) Name() string                       { return "stub" }
func (s *stubEmb) Model() string                      { return "m" }
func (s *stubEmb) Embed(context.Context, []string) ([][]float32, error) { return s.out, nil }

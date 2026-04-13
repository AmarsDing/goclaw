package memory

import (
	"context"
	"testing"
)

func TestLexicalRelevanceScorer_overlap(t *testing.T) {
	ctx := context.Background()
	s, err := LexicalRelevanceScorer(ctx,
		"user prefers golang for backend services",
		"we discussed golang and postgres for the backend yesterday",
	)
	if err != nil {
		t.Fatal(err)
	}
	if s < 0.1 {
		t.Fatalf("expected some overlap, got %v", s)
	}
}

func TestLexicalRelevanceScorer_disjoint(t *testing.T) {
	ctx := context.Background()
	s, err := LexicalRelevanceScorer(ctx,
		"quantum physics lecture notes",
		"recipe for chocolate cake baking temperature",
	)
	if err != nil {
		t.Fatal(err)
	}
	if s > 0.25 {
		t.Fatalf("expected low score, got %v", s)
	}
}

func TestDriftDetector_CheckDrift_lexical(t *testing.T) {
	d := NewDriftDetector(DriftConfig{Threshold: 0.2, CheckAfter: 1})
	d.RecordInjection([]L0Summary{
		{Topic: "t1", Summary: "project uses rust and wasm"},
	})
	d.Tick()
	if !d.ShouldRefresh() {
		t.Fatal("expected refresh after tick")
	}
	ctx := context.Background()
	drifted, err := d.CheckDrift(ctx,
		"today we only talk about python data science pandas",
		LexicalRelevanceScorer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !drifted {
		t.Fatal("expected drift vs unrelated context")
	}
}

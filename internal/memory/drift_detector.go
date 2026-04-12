package memory

import (
	"context"
	"log/slog"
	"time"
)

// DriftDetector monitors whether injected memories remain relevant to the
// evolving conversation. When drift is detected, it signals that the memory
// cache should be refreshed.
type DriftDetector struct {
	threshold    float64       // cosine similarity drop threshold (default 0.3)
	checkAfter   int           // re-check after N turns (default 5)
	lastInjected []L0Summary   // most recently injected memories
	turnsSince   int           // turns since last injection
	lastCheckAt  time.Time
}

// DriftConfig configures the drift detector.
type DriftConfig struct {
	Threshold  float64 // similarity drop to trigger refresh (default 0.3)
	CheckAfter int     // re-check every N turns (default 5)
}

// DefaultDriftConfig returns sensible defaults.
func DefaultDriftConfig() DriftConfig {
	return DriftConfig{
		Threshold:  0.3,
		CheckAfter: 5,
	}
}

// NewDriftDetector creates a drift detector.
func NewDriftDetector(cfg DriftConfig) *DriftDetector {
	return &DriftDetector{
		threshold:  cfg.Threshold,
		checkAfter: cfg.CheckAfter,
	}
}

// RecordInjection saves the memories that were injected this turn.
func (d *DriftDetector) RecordInjection(summaries []L0Summary) {
	d.lastInjected = summaries
	d.turnsSince = 0
	d.lastCheckAt = time.Now()
}

// Tick increments the turn counter.
func (d *DriftDetector) Tick() {
	d.turnsSince++
}

// ShouldRefresh reports whether the memory injection should be refreshed.
func (d *DriftDetector) ShouldRefresh() bool {
	if len(d.lastInjected) == 0 {
		return false
	}
	return d.turnsSince >= d.checkAfter
}

// CheckDrift compares the current conversation context against the last injected
// memories. Returns true if the conversation has drifted significantly.
// scorer returns a relevance score (0.0–1.0) for each memory against the current context.
func (d *DriftDetector) CheckDrift(
	ctx context.Context,
	currentContext string,
	scorer func(ctx context.Context, memory, context string) (float64, error),
) (bool, error) {
	if len(d.lastInjected) == 0 || scorer == nil {
		return false, nil
	}

	totalScore := 0.0
	scored := 0
	for _, mem := range d.lastInjected {
		score, err := scorer(ctx, mem.Summary, currentContext)
		if err != nil {
			slog.Warn("drift scorer error", "topic", mem.Topic, "err", err)
			continue
		}
		totalScore += score
		scored++
	}

	if scored == 0 {
		return false, nil
	}

	avgScore := totalScore / float64(scored)
	drifted := avgScore < d.threshold

	if drifted {
		slog.Debug("memory drift detected",
			"avg_score", avgScore, "threshold", d.threshold,
			"memories", scored)
	}

	d.turnsSince = 0
	d.lastCheckAt = time.Now()
	return drifted, nil
}

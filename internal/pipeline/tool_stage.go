package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ToolStage runs per iteration after PruneStage. Executes tool calls from
// ThinkState.LastResponse, checks exit conditions (loop kill, read-only streak, budget).
// User-cancel vs tool completion is handled by the agent adapter via ToolInterruptBehavior
// (InterruptBlock → WithoutCancel execution context).
type ToolStage struct {
	deps   *PipelineDeps
	result StageResult
}

// NewToolStage creates a ToolStage.
func NewToolStage(deps *PipelineDeps) *ToolStage {
	return &ToolStage{deps: deps, result: Continue}
}

func (s *ToolStage) Name() string       { return "tool" }
func (s *ToolStage) Result() StageResult { return s.result }

// Execute extracts tool calls, dispatches them, checks exit conditions.
func (s *ToolStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	resp := state.Think.LastResponse
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil // no tools — ThinkStage already set BreakLoop
	}

	toolCalls := resp.ToolCalls
	if s.deps.ExecuteToolCall == nil {
		return fmt.Errorf("ExecuteToolCall callback not configured")
	}

	// Parallel path: readonly/concurrency-safe tools execute in batches; mutating
	// tools stay sequential to preserve safety and ordering.
	if len(toolCalls) > 1 && s.deps.ExecuteToolRaw != nil && s.deps.ProcessToolResult != nil {
		return s.executePartitioned(ctx, state, toolCalls)
	}

	// Sequential fallback: ExecuteToolCall handles both I/O and state mutation.
	for _, tc := range toolCalls {
		msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
		if err != nil {
			return fmt.Errorf("execute tool %s: %w", tc.Name, err)
		}
		for _, msg := range msgs {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++
		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

func (s *ToolStage) executePartitioned(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	var parallelBatch []providers.ToolCall
	flushParallel := func() error {
		if len(parallelBatch) == 0 {
			return nil
		}
		if len(parallelBatch) == 1 {
			tc := parallelBatch[0]
			msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
			if err != nil {
				return fmt.Errorf("execute tool %s: %w", tc.Name, err)
			}
			for _, msg := range msgs {
				state.Messages.AppendPending(msg)
			}
			state.Tool.TotalToolCalls++
		} else {
			if err := s.executeParallel(ctx, state, parallelBatch); err != nil {
				return err
			}
		}
		parallelBatch = nil
		if state.Tool.LoopKilled {
			s.result = BreakLoop
		}
		return nil
	}

	for _, tc := range toolCalls {
		if s.isParallelSafe(tc.Name, tc.Arguments) {
			parallelBatch = append(parallelBatch, tc)
			continue
		}
		if err := flushParallel(); err != nil {
			return err
		}
		if s.result == BreakLoop {
			return nil
		}
		msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
		if err != nil {
			return fmt.Errorf("execute tool %s: %w", tc.Name, err)
		}
		for _, msg := range msgs {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++
		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
	}

	if err := flushParallel(); err != nil {
		return err
	}
	if s.result != BreakLoop {
		s.checkExitConditions(state)
	}
	return nil
}

// executeParallel runs tool I/O concurrently, then processes results sequentially.
// Soft-interrupt support: when InterruptCh fires (user injected a new message),
// only InterruptCancel-group tools are cancelled via a per-batch derived context;
// InterruptBlock-group tools use context.WithoutCancel and run to completion.
// The triggering message is already queued in InjectCh and will be processed by
// ObserveStage at the next turn boundary.
func (s *ToolStage) executeParallel(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	type rawResult struct {
		tc      providers.ToolCall
		msg     providers.Message
		rawData any
		err     error
	}

	// batchCtx is the context for the InterruptCancel group in this parallel batch.
	// It is derived from ctx so it inherits hard cancellation (AbortRun), but can
	// also be cancelled independently by a soft interrupt from InterruptCh.
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()

	// Monitor InterruptCh: a soft interrupt signals only the cancel-group.
	// The goroutine exits when batchCtx is done (either by interrupt or by the
	// outer ctx being cancelled — both result in cancelBatch being called).
	if s.deps.InterruptCh != nil {
		go func() {
			select {
			case <-s.deps.InterruptCh:
				cancelBatch()
			case <-batchCtx.Done():
			}
		}()
	}

	// Phase 1: parallel I/O (no state mutation).
	// Each goroutine receives batchCtx; ExecuteToolRaw applies toolExecutionContext
	// internally: InterruptBlock tools get context.WithoutCancel(batchCtx) and are
	// unaffected by cancelBatch(); InterruptCancel tools inherit batchCtx directly.
	results := make([]rawResult, len(toolCalls))
	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, tc providers.ToolCall) {
			defer wg.Done()
			msg, rawData, err := s.deps.ExecuteToolRaw(batchCtx, tc)
			results[idx] = rawResult{tc: tc, msg: msg, rawData: rawData, err: err}
		}(i, tc)
	}
	wg.Wait()

	// Phase 2: sequential state mutation (safe, deterministic order)
	for _, r := range results {
		if r.err != nil {
			return fmt.Errorf("execute tool %s: %w", r.tc.Name, r.err)
		}
		processed := s.deps.ProcessToolResult(ctx, state, r.tc, r.msg, r.rawData)
		for _, msg := range processed {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++
		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

func (s *ToolStage) isParallelSafe(toolName string, args map[string]any) bool {
	if s.deps.ToolConcurrencySafe == nil {
		return false
	}
	return s.deps.ToolConcurrencySafe(toolName, args)
}

// checkExitConditions checks read-only streak and tool budget.
func (s *ToolStage) checkExitConditions(state *RunState) {
	if state.Tool.LoopKilled {
		s.result = BreakLoop
		return
	}
	if s.deps.CheckReadOnly != nil {
		warningMsg, shouldBreak := s.deps.CheckReadOnly(state)
		if warningMsg != nil {
			state.Messages.AppendPending(*warningMsg)
		}
		if shouldBreak {
			s.result = BreakLoop
			return
		}
	}
	if s.deps.Config.MaxToolCalls > 0 && state.Tool.TotalToolCalls >= s.deps.Config.MaxToolCalls {
		s.result = BreakLoop
	}
}

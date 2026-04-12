package spirit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TaskStatus tracks the state of an orchestrated task.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskRunning    TaskStatus = "running"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
	TaskCancelled  TaskStatus = "cancelled"
)

// OrchestratedTask wraps a sub-task with execution state.
type OrchestratedTask struct {
	SubTask    SubTask    `json:"sub_task"`
	Status     TaskStatus `json:"status"`
	Result     string     `json:"result,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at,omitempty"`
	FinishedAt time.Time  `json:"finished_at,omitempty"`
}

// OrchestrationResult is the final output of a multi-agent orchestration.
type OrchestrationResult struct {
	Tasks       []OrchestratedTask `json:"tasks"`
	MergedResult string            `json:"merged_result"`
	TotalDuration time.Duration    `json:"total_duration"`
	Success      bool              `json:"success"`
}

// AgentExecutor runs a task on a specific agent.
type AgentExecutor interface {
	Execute(ctx context.Context, agentID string, task SubTask) (string, error)
}

// ResultMerger combines results from multiple agent executions.
type ResultMerger interface {
	Merge(ctx context.Context, results []OrchestratedTask) (string, error)
}

// Orchestrator manages multi-agent task execution for the spirit.
type Orchestrator struct {
	executor AgentExecutor
	merger   ResultMerger
}

// NewOrchestrator creates a spirit orchestrator.
func NewOrchestrator(executor AgentExecutor, merger ResultMerger) *Orchestrator {
	return &Orchestrator{
		executor: executor,
		merger:   merger,
	}
}

// Execute runs an intent's sub-tasks, respecting dependencies.
func (o *Orchestrator) Execute(ctx context.Context, intent *Intent) (*OrchestrationResult, error) {
	start := time.Now()

	tasks := make([]OrchestratedTask, len(intent.SubTasks))
	for i, st := range intent.SubTasks {
		tasks[i] = OrchestratedTask{
			SubTask: st,
			Status:  TaskPending,
		}
	}

	// Build dependency graph.
	taskMap := make(map[string]*OrchestratedTask)
	for i := range tasks {
		taskMap[tasks[i].SubTask.ID] = &tasks[i]
	}

	// Execute tasks respecting dependencies.
	completed := make(map[string]bool)
	var mu sync.Mutex

	for {
		// Find tasks ready to run (all dependencies met).
		var ready []*OrchestratedTask
		for i := range tasks {
			if tasks[i].Status != TaskPending {
				continue
			}
			allDepsMet := true
			for _, dep := range tasks[i].SubTask.DependsOn {
				mu.Lock()
				met := completed[dep]
				mu.Unlock()
				if !met {
					allDepsMet = false
					break
				}
			}
			if allDepsMet {
				ready = append(ready, &tasks[i])
			}
		}

		if len(ready) == 0 {
			// Check if all tasks are done.
			allDone := true
			for _, t := range tasks {
				if t.Status == TaskPending || t.Status == TaskRunning {
					allDone = false
					break
				}
			}
			if allDone {
				break
			}
			// Deadlock detection.
			slog.Warn("spirit orchestrator: no tasks ready, possible deadlock")
			break
		}

		// Execute ready tasks in parallel.
		var wg sync.WaitGroup
		for _, task := range ready {
			wg.Add(1)
			go func(t *OrchestratedTask) {
				defer wg.Done()
				o.executeTask(ctx, t)
				mu.Lock()
				completed[t.SubTask.ID] = true
				mu.Unlock()
			}(task)
		}
		wg.Wait()

		if ctx.Err() != nil {
			break
		}
	}

	// Merge results.
	result := &OrchestrationResult{
		Tasks:         tasks,
		TotalDuration: time.Since(start),
		Success:       true,
	}

	for _, t := range tasks {
		if t.Status == TaskFailed {
			result.Success = false
			break
		}
	}

	if o.merger != nil && result.Success {
		merged, err := o.merger.Merge(ctx, tasks)
		if err != nil {
			slog.Warn("spirit orchestrator: merge failed", "err", err)
		} else {
			result.MergedResult = merged
		}
	} else {
		// Simple concatenation fallback.
		var combined string
		for _, t := range tasks {
			if t.Result != "" {
				if combined != "" {
					combined += "\n\n---\n\n"
				}
				combined += fmt.Sprintf("## %s\n\n%s", t.SubTask.Description, t.Result)
			}
		}
		result.MergedResult = combined
	}

	slog.Info("spirit orchestration complete",
		"tasks", len(tasks), "success", result.Success,
		"duration", result.TotalDuration)

	return result, nil
}

func (o *Orchestrator) executeTask(ctx context.Context, task *OrchestratedTask) {
	task.Status = TaskRunning
	task.StartedAt = time.Now()

	agentID := task.SubTask.AgentID
	if agentID == "" {
		task.Status = TaskFailed
		task.Error = "no agent assigned"
		task.FinishedAt = time.Now()
		return
	}

	result, err := o.executor.Execute(ctx, agentID, task.SubTask)
	task.FinishedAt = time.Now()

	if err != nil {
		task.Status = TaskFailed
		task.Error = err.Error()
		slog.Warn("spirit task failed",
			"task", task.SubTask.ID, "agent", agentID, "err", err)
	} else {
		task.Status = TaskCompleted
		task.Result = result
	}
}

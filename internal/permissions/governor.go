package permissions

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// AgentMode controls the default permission behavior for tool execution.
type AgentMode string

const (
	ModeSafe        AgentMode = "safe"        // all destructive tools require approval
	ModeStandard    AgentMode = "standard"    // known-safe tools auto-approved, others ask
	ModeAutonomous  AgentMode = "autonomous"  // all tools auto-approved (no prompts)
	ModePlan        AgentMode = "plan"        // read-only planning mode: all write/exec tools ask
	ModeAuto        AgentMode = "auto"        // alias for autonomous (Claude Code parity)
	ModeAcceptEdits AgentMode = "acceptEdits" // auto-approve file edits, ask for shell/network
)

// SecurityClass categorises a tool's risk level.
type SecurityClass string

const (
	ClassReadOnly  SecurityClass = "read_only" // read files, search, list
	ClassWriteFS   SecurityClass = "write_fs"  // create/modify/delete files
	ClassExecute   SecurityClass = "execute"   // run shell commands
	ClassNetwork   SecurityClass = "network"   // HTTP requests, web fetch
	ClassDangerous SecurityClass = "dangerous" // system-level operations
)

// PermissionAction is the result of a rule evaluation.
type PermissionAction string

const (
	ActionAllow PermissionAction = "allow"
	ActionDeny  PermissionAction = "deny"
	ActionAsk   PermissionAction = "ask" // requires user approval
)

// PermissionRule is a persistent rule that controls tool access.
type PermissionRule struct {
	ID       string           `json:"id"`
	AgentID  string           `json:"agent_id"`  // "*" for global
	ToolName string           `json:"tool_name"` // "*" for all tools
	Action   PermissionAction `json:"action"`
	Priority int              `json:"priority"`          // higher wins
	Matcher  RuleMatcher      `json:"matcher,omitempty"` // optional argument matcher
	Comment  string           `json:"comment,omitempty"`
}

// RuleMatcher optionally restricts a rule to specific argument patterns.
type RuleMatcher struct {
	ArgKey   string `json:"arg_key,omitempty"`   // argument name to match
	ArgValue string `json:"arg_value,omitempty"` // regex or glob pattern
}

// SecurityClassification is the result of classifying a tool call.
type SecurityClassification struct {
	Class       SecurityClass `json:"class"`
	Description string        `json:"description"`
	Risk        string        `json:"risk"` // "low", "medium", "high"
}

// PermissionRequest is submitted to the governor for evaluation.
type PermissionRequest struct {
	AgentID   string         `json:"agent_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
	UserID    string         `json:"user_id"`
	RunID     string         `json:"run_id"`
	Channel   string         `json:"channel"`
}

// PermissionDecision is the governor's verdict on a permission request.
type PermissionDecision struct {
	Allowed        bool                    `json:"allowed"`
	Action         PermissionAction        `json:"action"`
	Reason         string                  `json:"reason"`
	Rule           *PermissionRule         `json:"rule,omitempty"`
	Mode           AgentMode               `json:"mode"`
	Classification *SecurityClassification `json:"classification,omitempty"`
	AuditID        string                  `json:"audit_id"`
	EvaluatedAt    time.Time               `json:"evaluated_at"`
}

// AuditEntry records a permission decision for compliance and review.
type AuditEntry struct {
	ID        string             `json:"id"`
	Timestamp time.Time          `json:"timestamp"`
	Request   PermissionRequest  `json:"request"`
	Decision  PermissionDecision `json:"decision"`
}

// Governor implements the 6-layer permission governance chain:
//  1. Rules — static allow/deny/ask per tool per agent
//  2. Mode — agent mode (safe/standard/autonomous)
//  3. Classifier — security classification of tool + arguments
//  4. Hook — pre/post decision hooks (extensible)
//  5. Approval — interactive approval for "ask" decisions
//  6. Audit — record all decisions
type Governor struct {
	mu    sync.RWMutex
	rules []PermissionRule
	modes map[string]AgentMode // agentID → mode

	classifier    *Classifier
	hooks         []GovernorHook
	audit         []AuditEntry
	alwaysAllowed map[string]bool
	auditStore    AuditStore

	// ApprovalFunc is called when a decision requires user approval.
	// Evaluate now preserves ActionAsk; callers can use ResolveApproval to turn
	// the ask into an allow/deny decision when interactive approval is available.
	ApprovalFunc func(ctx context.Context, req PermissionRequest, class *SecurityClassification) (bool, error)
	ToolChecker  ToolPermissionChecker
	SafetyCheck  SafetyChecker
}

// GovernorHook is called before/after permission decisions.
type GovernorHook struct {
	PreEvaluate  func(ctx context.Context, req PermissionRequest) error
	PostEvaluate func(ctx context.Context, req PermissionRequest, dec PermissionDecision)
}

// ToolPermissionChecker lets a concrete tool apply content-aware permission checks.
type ToolPermissionChecker func(ctx context.Context, req PermissionRequest, class *SecurityClassification) (*ToolPermissionVerdict, error)

// ToolPermissionVerdict is a tool-level permission outcome.
type ToolPermissionVerdict struct {
	Action      PermissionAction `json:"action"`
	Reason      string           `json:"reason,omitempty"`
	Interactive bool             `json:"interactive,omitempty"`
}

// SafetyChecker performs non-bypassable safety validation after rule/tool checks.
type SafetyChecker func(ctx context.Context, req PermissionRequest, class *SecurityClassification) (*SafetyVerdict, error)

// SafetyVerdict is the result of a safety check.
type SafetyVerdict struct {
	Action PermissionAction `json:"action"`
	Reason string           `json:"reason,omitempty"`
}

// AuditStore persists permission audit entries.
type AuditStore interface {
	AppendAuditEntry(ctx context.Context, entry AuditEntry) error
	ListAuditEntries(ctx context.Context, limit int) ([]AuditEntry, error)
}

// NewGovernor creates a governor with default configuration.
func NewGovernor() *Governor {
	return &Governor{
		modes:         make(map[string]AgentMode),
		classifier:    NewClassifier(),
		alwaysAllowed: make(map[string]bool),
	}
}

// Evaluate runs the 6-layer governance chain and returns a decision.
func (g *Governor) Evaluate(ctx context.Context, req PermissionRequest) (*PermissionDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	g.mu.RLock()
	hooks := make([]GovernorHook, len(g.hooks))
	copy(hooks, g.hooks)
	toolChecker := g.ToolChecker
	safetyCheck := g.SafetyCheck
	g.mu.RUnlock()

	// Layer 4a: Pre-hooks
	for _, h := range hooks {
		if h.PreEvaluate != nil {
			if err := h.PreEvaluate(ctx, req); err != nil {
				return nil, fmt.Errorf("permission hook: %w", err)
			}
		}
	}

	// Layer 3: Classify the tool call
	class := g.classifier.Classify(req.ToolName, req.Arguments)
	mode := g.getMode(req.AgentID)

	// Layer 1a: deny rules always win.
	if dec := g.evaluateRules(req, class, ActionDeny, false); dec != nil {
		g.recordAndNotify(ctx, req, dec, hooks)
		return dec, nil
	}
	if dec := g.evaluateRules(req, class, ActionDeny, true); dec != nil {
		g.recordAndNotify(ctx, req, dec, hooks)
		return dec, nil
	}

	// Layer 1b: general ask rules.
	askRule := g.evaluateRules(req, class, ActionAsk, false)
	contentAskRule := g.evaluateRules(req, class, ActionAsk, true)
	allowRule := g.evaluateRules(req, class, ActionAllow, false)
	contentAllowRule := g.evaluateRules(req, class, ActionAllow, true)

	// Layer 2: tool-specific permission checks.
	var toolVerdict *ToolPermissionVerdict
	if toolChecker != nil {
		var err error
		toolVerdict, err = toolChecker(ctx, req, class)
		if err != nil {
			return nil, fmt.Errorf("tool permission check: %w", err)
		}
		if toolVerdict != nil && toolVerdict.Action == ActionDeny {
			dec := g.newDecision(req, mode, class, ActionDeny, "tool check: "+toolVerdict.Reason, nil)
			g.recordAndNotify(ctx, req, dec, hooks)
			return dec, nil
		}
	}

	// Layer 3: content-aware ask rules after tool-specific checks.
	if contentAskRule != nil {
		g.recordAndNotify(ctx, req, contentAskRule, hooks)
		return contentAskRule, nil
	}
	if toolVerdict != nil && toolVerdict.Action == ActionAsk {
		dec := g.newDecision(req, mode, class, ActionAsk, "tool check: "+toolVerdict.Reason, nil)
		g.recordAndNotify(ctx, req, dec, hooks)
		return dec, nil
	}
	if askRule != nil {
		g.recordAndNotify(ctx, req, askRule, hooks)
		return askRule, nil
	}

	// Layer 4: safety checks are non-bypassable.
	if safetyCheck != nil {
		verdict, err := safetyCheck(ctx, req, class)
		if err != nil {
			return nil, fmt.Errorf("safety check: %w", err)
		}
		if verdict != nil && verdict.Action != "" && verdict.Action != ActionAllow {
			dec := g.newDecision(req, mode, class, verdict.Action, "safety: "+verdict.Reason, nil)
			g.recordAndNotify(ctx, req, dec, hooks)
			return dec, nil
		}
	}

	// Layer 5: mode bypass and always-allowed tools.
	action := g.modeDefault(mode, class)
	if action != ActionAllow && g.isAlwaysAllowed(req.ToolName) {
		action = ActionAllow
	}

	// Layer 6: explicit allow rules can promote remaining requests, but do not
	// override earlier ask/deny/safety/tool decisions.
	if contentAllowRule != nil {
		action = ActionAllow
	}
	if allowRule != nil {
		action = ActionAllow
	}

	dec := g.newDecision(req, mode, class, action, fmt.Sprintf("mode=%s class=%s", mode, class.Class), nil)
	g.recordAndNotify(ctx, req, dec, hooks)
	return dec, nil
}

// ResolveApproval converts an ask decision into allow/deny when an interactive
// approval function is configured.
func (g *Governor) ResolveApproval(ctx context.Context, req PermissionRequest, dec *PermissionDecision) (*PermissionDecision, error) {
	if dec == nil || dec.Action != ActionAsk {
		return dec, nil
	}
	if g.ApprovalFunc == nil {
		return dec, nil
	}
	approved, err := g.ApprovalFunc(ctx, req, dec.Classification)
	if err != nil {
		return nil, fmt.Errorf("approval: %w", err)
	}
	resolved := *dec
	if approved {
		resolved.Action = ActionAllow
		resolved.Allowed = true
		resolved.Reason = "approved: " + dec.Reason
	} else {
		resolved.Action = ActionDeny
		resolved.Allowed = false
		resolved.Reason = "rejected: " + dec.Reason
	}
	resolved.EvaluatedAt = time.Now()
	return &resolved, nil
}

// SetToolChecker sets the tool-specific checker.
func (g *Governor) SetToolChecker(checker ToolPermissionChecker) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ToolChecker = checker
}

// SetSafetyCheck sets the non-bypassable safety checker.
func (g *Governor) SetSafetyCheck(checker SafetyChecker) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.SafetyCheck = checker
}

// AllowAlways marks a tool as always-allowed after safety validation.
func (g *Governor) AllowAlways(toolName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.alwaysAllowed[toolName] = true
}

// RemoveAlwaysAllowed clears an always-allowed tool.
func (g *Governor) RemoveAlwaysAllowed(toolName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.alwaysAllowed, toolName)
}

// SetAuditStore configures durable audit persistence.
func (g *Governor) SetAuditStore(store AuditStore) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.auditStore = store
}

func (g *Governor) newDecision(
	req PermissionRequest,
	mode AgentMode,
	class *SecurityClassification,
	action PermissionAction,
	reason string,
	rule *PermissionRule,
) *PermissionDecision {
	return &PermissionDecision{
		Allowed:        action == ActionAllow,
		Action:         action,
		Reason:         reason,
		Rule:           rule,
		Mode:           mode,
		Classification: class,
		EvaluatedAt:    time.Now(),
	}
}

func (g *Governor) isAlwaysAllowed(toolName string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.alwaysAllowed[toolName]
}

func (g *Governor) evaluateRules(
	req PermissionRequest,
	class *SecurityClassification,
	action PermissionAction,
	matcherRequired bool,
) *PermissionDecision {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var best *PermissionRule
	bestPriority := -1

	for i := range g.rules {
		r := &g.rules[i]
		if r.Action != action {
			continue
		}
		hasMatcher := r.Matcher.ArgKey != "" || r.Matcher.ArgValue != ""
		if matcherRequired && !hasMatcher {
			continue
		}
		if !matcherRequired && hasMatcher {
			continue
		}
		if !ruleMatches(r, req) {
			continue
		}
		if r.Priority > bestPriority {
			best = r
			bestPriority = r.Priority
		}
	}

	if best == nil {
		return nil
	}

	mode := g.modes[req.AgentID]
	if mode == "" {
		mode = ModeStandard
	}
	return g.newDecision(req, mode, class, best.Action, fmt.Sprintf("rule %s: %s", best.ID, best.Comment), best)
}

func ruleMatches(r *PermissionRule, req PermissionRequest) bool {
	if r.AgentID != "*" && r.AgentID != req.AgentID {
		return false
	}
	if r.ToolName != "*" && r.ToolName != req.ToolName {
		return false
	}
	if r.Matcher.ArgKey == "" && r.Matcher.ArgValue == "" {
		return true
	}

	value, ok := req.Arguments[r.Matcher.ArgKey]
	if !ok {
		return false
	}
	actual := fmt.Sprint(value)
	if r.Matcher.ArgValue == "" {
		return actual != ""
	}
	if matched, err := regexp.MatchString(r.Matcher.ArgValue, actual); err == nil && matched {
		return true
	}
	if matched, err := filepath.Match(r.Matcher.ArgValue, actual); err == nil && matched {
		return true
	}
	return actual == r.Matcher.ArgValue
}

// AddRule adds a permission rule. Rules with higher priority take precedence.
func (g *Governor) AddRule(rule PermissionRule) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rules = append(g.rules, rule)
}

// RemoveRule removes a rule by ID.
func (g *Governor) RemoveRule(ruleID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, r := range g.rules {
		if r.ID == ruleID {
			g.rules = append(g.rules[:i], g.rules[i+1:]...)
			return
		}
	}
}

// SetMode sets the operating mode for an agent.
func (g *Governor) SetMode(agentID string, mode AgentMode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.modes[agentID] = mode
}

// GetMode returns the current mode for an agent (default: ModeStandard).
func (g *Governor) GetMode(agentID string) AgentMode {
	return g.getMode(agentID)
}

// SwitchMode atomically transitions an agent to a new mode and logs the change.
// Returns the previous mode. This is the primary API for runtime mode changes
// (e.g. CLI /mode plan or /mode auto).
func (g *Governor) SwitchMode(agentID string, next AgentMode, reason string) AgentMode {
	g.mu.Lock()
	prev := g.modes[agentID]
	if prev == "" {
		prev = ModeStandard
	}
	g.modes[agentID] = next
	g.mu.Unlock()
	slog.Info("permission mode switched",
		"agent", agentID, "from", prev, "to", next, "reason", reason)
	return prev
}

// AddHook adds a governor hook.
func (g *Governor) AddHook(hook GovernorHook) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.hooks = append(g.hooks, hook)
}

// AuditLog returns a copy of recent audit entries.
func (g *Governor) AuditLog(limit int) []AuditEntry {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if limit <= 0 || limit > len(g.audit) {
		limit = len(g.audit)
	}
	start := len(g.audit) - limit
	out := make([]AuditEntry, limit)
	copy(out, g.audit[start:])
	return out
}

func (g *Governor) getMode(agentID string) AgentMode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if m, ok := g.modes[agentID]; ok {
		return m
	}
	return ModeStandard
}

func (g *Governor) modeDefault(mode AgentMode, class *SecurityClassification) PermissionAction {
	switch mode {
	case ModeAutonomous, ModeAuto:
		return ActionAllow
	case ModeSafe:
		if class.Class == ClassReadOnly {
			return ActionAllow
		}
		return ActionAsk
	case ModePlan:
		// Plan mode: read-only only — any write/exec/network/dangerous operation requires approval.
		if class.Class == ClassReadOnly {
			return ActionAllow
		}
		return ActionAsk
	case ModeAcceptEdits:
		// AcceptEdits: file writes are auto-approved; shell, network, and dangerous still ask.
		switch class.Class {
		case ClassReadOnly, ClassWriteFS:
			return ActionAllow
		default:
			return ActionAsk
		}
	default: // ModeStandard
		switch class.Class {
		case ClassReadOnly:
			return ActionAllow
		case ClassDangerous, ClassExecute:
			return ActionAsk
		default:
			return ActionAllow
		}
	}
}

func (g *Governor) recordAndNotify(ctx context.Context, req PermissionRequest, dec *PermissionDecision, hooks []GovernorHook) {
	auditID := fmt.Sprintf("audit-%d", time.Now().UnixNano())
	dec.AuditID = auditID

	// Layer 6: Audit
	entry := AuditEntry{
		ID:        auditID,
		Timestamp: dec.EvaluatedAt,
		Request:   req,
		Decision:  *dec,
	}
	g.mu.Lock()
	g.audit = append(g.audit, entry)
	auditStore := g.auditStore
	// Cap audit log at 10000 entries.
	if len(g.audit) > 10000 {
		g.audit = g.audit[len(g.audit)-5000:]
	}
	g.mu.Unlock()

	if auditStore != nil {
		if err := auditStore.AppendAuditEntry(ctx, entry); err != nil {
			slog.Warn("permission audit persist failed", "audit", auditID, "err", err)
		}
	}

	slog.Debug("permission decision",
		"tool", req.ToolName, "agent", req.AgentID,
		"allowed", dec.Allowed, "action", dec.Action, "audit", auditID)

	// Layer 4b: Post-hooks
	for _, h := range hooks {
		if h.PostEvaluate != nil {
			h.PostEvaluate(ctx, req, *dec)
		}
	}
}

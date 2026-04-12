package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/compression"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/lifecycle"
	dwmessage "github.com/nextlevelbuilder/goclaw/internal/message"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/plugins"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/resume"
	"github.com/nextlevelbuilder/goclaw/internal/spirit"
	pgstore "github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/sdk"
)

func (l *Loop) initDreamWeaverServices() {
	if !l.dreamweaverEnabled() {
		return
	}

	l.lifecycleMgr = lifecycle.NewManager(l.domainBus)
	l.hookRegistry = hooks.NewRegistry()
	l.governor = permissions.NewGovernor()
	l.resumeEng = resume.NewEngine()
	l.sdkBridge = sdk.NewBridge()
	l.pluginRegistry = plugins.NewRegistry(filepath.Join(l.dataDir, "plugins"))
	profileStore := spirit.ProfileStore(noopProfileStore{})
	if l.runtimeDB != nil {
		profileStore = pgstore.NewPGSpiritProfileStore(l.runtimeDB)
		l.governor.SetAuditStore(pgstore.NewPGPermissionAuditStore(l.runtimeDB))
		l.topicWorker = consolidation.NewTopicWorker(
			pgstore.NewPGTopicStore(l.runtimeDB),
			staticTopicExtractor{},
			consolidation.TopicWorkerConfig{
				MinEpisodicCount: 1,
				MergeThreshold:   0.8,
				Interval:         time.Hour,
			},
		)
		l.dailyLogWorker = consolidation.NewDailyLogWorker(
			pgstore.NewPGDailyLogStore(l.runtimeDB),
			staticDailySummariser{},
			consolidation.DefaultDailyLogWorkerConfig(),
		)
	}
	l.profileMgr = spirit.NewProfileManager(profileStore)
	var classifier spirit.IntentClassifier
	if l.spiritEnabled() && l.provider != nil {
		classifier = spirit.NewLLMIntentClassifier(l.provider, l.model)
	}
	l.spiritRouter = spirit.NewRouter(classifier)
	if l.spiritDelegateRunFn != nil {
		exec := &delegateAgentExecutor{
			runFn:        l.spiritDelegateRunFn,
			fromAgentKey: l.id,
		}
		l.spiritOrchestrator = spirit.NewOrchestrator(exec, nil)
	}
	l.learningLoop = spirit.NewLearningLoop(l.profileMgr)
	if l.runtimeDB != nil {
		l.learningLoop.BindTopicStore(pgstore.NewPGTopicStore(l.runtimeDB), l.agentUUID.String())
	}
	l.compressionEng = compression.NewEngine(
		compression.DefaultConfig(),
		func(msgs []providers.Message) int {
			total := 0
			for _, msg := range msgs {
				total += len([]rune(msg.Content))/4 + len([]rune(msg.Thinking))/4 + len(msg.ToolCalls)*16
			}
			return total
		},
		nil,
	)
	l.loadDreamWeaverHooks(context.Background())

	l.lifecycleMgr.OnTransition(func(t lifecycle.Transition) {
		if !l.sdkBridgeEnabled() || l.sdkBridge == nil {
			return
		}
		l.sdkBridge.Emit(sdk.Event{
			Type:  sdk.EventStateChange,
			RunID: t.RunID,
			Data: sdk.StateChangeData{
				From:   t.From.String(),
				To:     t.To.String(),
				Reason: t.Reason,
			},
		})
	})

	if l.pluginsEnabled() && l.pluginRegistry != nil {
		registry, ok := l.tools.(*tools.Registry)
		if ok && l.dataDir != "" {
			l.pluginRegistry.OnActivate(func(inst *plugins.Instance) error {
				for _, decl := range inst.Manifest.Capabilities.Tools {
					registry.Register(pluginRuntimeTool{
						name:        decl.Name,
						description: decl.Description,
						parameters:  decl.Parameters,
						pluginName:  inst.Manifest.Name,
						registry:    l.pluginRegistry,
					})
				}
				return nil
			})
			pluginDir := filepath.Join(l.dataDir, "plugins")
			if err := l.pluginRegistry.LoadFromDir(context.Background(), pluginDir); err == nil {
				for _, inst := range l.pluginRegistry.List() {
					_ = l.pluginRegistry.Activate(context.Background(), inst.Manifest.Name)
				}
			}
		}
	}
}

func (l *Loop) dreamweaverEnabled() bool {
	return l.dreamweaverCfg != nil && l.dreamweaverCfg.Enabled
}

func (l *Loop) lifecycleEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.LifecycleEnabled
}

func (l *Loop) hooksEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.HooksEnabled
}

func (l *Loop) governorEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.GovernorEnabled
}

func (l *Loop) compressionEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.CompressionEnabled
}

func (l *Loop) resumeEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.ResumeEnabled
}

func (l *Loop) sdkBridgeEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.SDKBridgeEnabled
}

func (l *Loop) spiritEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.SpiritEnabled
}

func (l *Loop) pluginsEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.PluginsEnabled
}

func (l *Loop) promptSectionsEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.PromptSectionsEnabled
}

func (l *Loop) transitionLifecycle(ctx context.Context, runID string, to lifecycle.State, reason string) {
	if !l.lifecycleEnabled() || l.lifecycleMgr == nil || runID == "" {
		return
	}
	_ = l.lifecycleMgr.Transition(ctx, runID, to, reason)
}

func (l *Loop) registerLifecycleRun(runID, userID string) {
	if !l.lifecycleEnabled() || l.lifecycleMgr == nil || runID == "" {
		return
	}
	l.lifecycleMgr.Register(runID, l.agentUUID.String(), l.tenantID.String(), userID)
}

func (l *Loop) cleanupLifecycleRun(runID string) {
	if !l.lifecycleEnabled() || l.lifecycleMgr == nil || runID == "" {
		return
	}
	l.lifecycleMgr.Cleanup(runID)
}

func (l *Loop) storeTranscript(runID, sessionKey string, msgs []providers.Message) {
	if runID == "" {
		return
	}
	tr := dwmessage.NewTranscript(sessionKey)
	tr.Append(msgs...)
	l.transcripts.Store(runID, tr)
	l.writeTranscriptJSONL(runID, msgs, false)
}

func (l *Loop) appendTranscript(runID string, msgs ...providers.Message) {
	if runID == "" || len(msgs) == 0 {
		return
	}
	value, ok := l.transcripts.Load(runID)
	if !ok {
		return
	}
	if tr, ok := value.(*dwmessage.Transcript); ok {
		tr.Append(msgs...)
	}
	l.writeTranscriptJSONL(runID, msgs, true)
}

func (l *Loop) transcriptForRun(runID string) *dwmessage.Transcript {
	if runID == "" {
		return nil
	}
	value, ok := l.transcripts.Load(runID)
	if !ok {
		return nil
	}
	tr, _ := value.(*dwmessage.Transcript)
	return tr
}

func (l *Loop) deleteTranscript(runID string) {
	if runID == "" {
		return
	}
	l.transcripts.Delete(runID)
}

func (l *Loop) transcriptLogPath(runID string) string {
	if runID == "" || l.dataDir == "" {
		return ""
	}
	return filepath.Join(l.dataDir, "transcripts", runID+".jsonl")
}

func (l *Loop) writeTranscriptJSONL(runID string, msgs []providers.Message, appendMode bool) {
	path := l.transcriptLogPath(runID)
	if path == "" || len(msgs) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, msg := range msgs {
		_ = enc.Encode(msg)
	}
}

type noopProfileStore struct{}

func (noopProfileStore) GetProfile(context.Context, string, string) (*spirit.Profile, error) {
	return nil, nil
}
func (noopProfileStore) SaveProfile(context.Context, *spirit.Profile) error  { return nil }
func (noopProfileStore) DeleteProfile(context.Context, string, string) error { return nil }

func defaultDreamWeaverConfig(cfg *config.DreamWeaverConfig) *config.DreamWeaverConfig {
	if cfg != nil {
		return cfg
	}
	return &config.DreamWeaverConfig{
		Enabled:               false,
		LifecycleEnabled:      false,
		GovernorEnabled:       false,
		HooksEnabled:          false,
		CompressionEnabled:    false,
		ResumeEnabled:         false,
		SDKBridgeEnabled:      false,
		SpiritEnabled:         false,
		PluginsEnabled:        false,
		PromptSectionsEnabled: false,
	}
}

func makeHookPayload(event hooks.Event, req *RunRequest, data map[string]any) hooks.Payload {
	return hooks.Payload{
		Event:      event,
		RunID:      req.RunID,
		AgentID:    "",
		SessionKey: req.SessionKey,
		Timestamp:  time.Now(),
		Data:       data,
	}
}

type pluginPlaceholderTool struct {
	name        string
	description string
	parameters  map[string]any
	pluginName  string
}

func (t pluginPlaceholderTool) Name() string               { return t.name }
func (t pluginPlaceholderTool) Description() string        { return t.description }
func (t pluginPlaceholderTool) Parameters() map[string]any { return t.parameters }
func (t pluginPlaceholderTool) Execute(context.Context, map[string]any) *tools.Result {
	return tools.ErrorResult("plugin tool placeholder: " + t.pluginName + "/" + t.name + " is registered but has no runtime executor wired yet")
}

type pluginRuntimeTool struct {
	name        string
	description string
	parameters  map[string]any
	pluginName  string
	registry    *plugins.Registry
}

func (t pluginRuntimeTool) Name() string               { return t.name }
func (t pluginRuntimeTool) Description() string        { return t.description }
func (t pluginRuntimeTool) Parameters() map[string]any { return t.parameters }
func (t pluginRuntimeTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if t.registry == nil {
		return tools.ErrorResult("plugin runtime unavailable: registry is nil")
	}
	result, err := t.registry.ExecuteTool(ctx, t.pluginName, t.name, args)
	if err != nil {
		return tools.ErrorResult("plugin tool failed: " + err.Error())
	}
	return result
}

type staticTopicExtractor struct{}

func (staticTopicExtractor) ExtractTopics(_ context.Context, summaries []string) ([]consolidation.ExtractedTopic, error) {
	var out []consolidation.ExtractedTopic
	for idx, summary := range summaries {
		topic := inferTopicFromSummary(summary)
		if topic == "" {
			topic = fmt.Sprintf("session-topic-%d", idx+1)
		}
		out = append(out, consolidation.ExtractedTopic{
			Topic:   topic,
			Summary: truncateSummary(summary, 400),
			Tags:    topicTags(topic),
			Sources: []int{idx},
		})
	}
	return out, nil
}

type staticDailySummariser struct{}

func (staticDailySummariser) Summarise(_ context.Context, sessions []consolidation.SessionSummaryInput) (string, error) {
	parts := make([]string, 0, len(sessions))
	for _, session := range sessions {
		summary := strings.TrimSpace(session.Summary)
		if summary == "" {
			summary = "Session completed without an explicit summary."
		}
		parts = append(parts, summary)
	}
	return strings.Join(parts, "\n\n"), nil
}

func inferTopicFromSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	summary = strings.ReplaceAll(summary, "\n", " ")
	words := strings.Fields(summary)
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

func truncateSummary(summary string, limit int) string {
	if limit <= 0 {
		return summary
	}
	runes := []rune(summary)
	if len(runes) <= limit {
		return summary
	}
	return string(runes[:limit]) + "..."
}

func topicTags(topic string) []string {
	if topic == "" {
		return nil
	}
	seen := map[string]bool{}
	var tags []string
	for _, word := range strings.Fields(strings.ToLower(topic)) {
		word = strings.Trim(word, ".,:;!?()[]{}\"'")
		if word == "" || seen[word] {
			continue
		}
		seen[word] = true
		tags = append(tags, word)
	}
	return tags
}

func (l *Loop) loadDreamWeaverHooks(ctx context.Context) {
	if l.runtimeDB == nil || l.hookRegistry == nil {
		return
	}
	store := pgstore.NewPGHookConfigStore(l.runtimeDB)
	registered, err := store.ListHooks(ctx, l.tenantID, l.agentUUID)
	if err != nil {
		return
	}
	for _, hook := range registered {
		_ = l.hookRegistry.Register(hook)
	}
}

func (l *Loop) persistDreamWeaverContinuity(ctx context.Context, req *RunRequest, sessionKey string, tokensUsed int, finalContent string, compactionCount int) {
	if req == nil || sessionKey == "" {
		return
	}
	summary := strings.TrimSpace(l.sessions.GetSummary(ctx, sessionKey))
	if summary == "" {
		summary = strings.TrimSpace(finalContent)
	}
	if summary == "" {
		summary = "Session completed without a stored summary."
	}

	if l.topicWorker != nil {
		_ = l.topicWorker.Process(ctx, l.agentUUID.String(), l.tenantID.String(), req.UserID, []consolidation.EpisodicInput{{
			ID:      fmt.Sprintf("%s:%d", sessionKey, compactionCount),
			Summary: summary,
		}})
	}

	if l.dailyLogWorker != nil {
		date := time.Now().In(time.UTC).Format("2006-01-02")
		_ = l.dailyLogWorker.Process(ctx, l.agentUUID.String(), l.tenantID.String(), req.UserID, date, []consolidation.SessionSummaryInput{{
			SessionKey: sessionKey,
			Summary:    summary,
			ToolCalls:  0,
			Tokens:     tokensUsed,
			StartedAt:  time.Now().UTC(),
		}})
	}
}

func (l *Loop) buildDreamWeaverPromptSections(ctx context.Context, req *RunRequest) (spiritSect, memorySect, runtimeSect, rulesSect string) {

	if l.spiritEnabled() && l.profileMgr != nil && l.spiritRouter != nil && req != nil && req.Message != "" {
		profile, _ := l.profileMgr.Get(ctx, req.UserID, l.tenantID.String())
		agents := []spirit.AgentInfo{{
			ID:           l.id,
			Name:         l.displayName,
			Description:  l.agentType,
			Capabilities: []string{"chat", "code", "research", "design"},
			Available:    true,
		}}
		for _, target := range l.delegateTargets {
			agents = append(agents, spirit.AgentInfo{
				ID:           target.AgentKey,
				Name:         target.DisplayName,
				Description:  target.Description,
				Capabilities: []string{"delegated"},
				Available:    true,
			})
		}
		l.spiritRouter.SetAgents(agents)
		intent, err := l.spiritRouter.Route(ctx, req.Message, profile)
		if err == nil && intent != nil {
			spiritSect = fmt.Sprintf(
				"## DreamWeaver Spirit\n\nDetected intent: %s.\nConfidence: %.2f.\nSuggested agents: %v.\n",
				intent.Type, intent.Confidence, intent.SuggestedAgents,
			)
			// When the orchestrator is available and the intent has subtasks
			// with assigned agents, run them and embed results in the spirit
			// section so the LLM can synthesize a final answer from delegate outputs.
			if l.spiritOrchestrator != nil && intentHasAssignedSubTasks(intent) {
				orchResult, orchErr := l.spiritOrchestrator.Execute(ctx, intent)
				if orchErr == nil && orchResult != nil && orchResult.MergedResult != "" {
					spiritSect += fmt.Sprintf(
						"\n### Orchestration Results\n\nThe following subtasks were delegated and completed:\n\n%s\n",
						orchResult.MergedResult,
					)
				}
			}
		}
	}

	if l.hasMemory {
		memorySect = "## DreamWeaver Memory\n\nPrefer durable memory only for reusable facts, preferences, and ongoing goals. Avoid re-storing transient execution noise.\n"
	}

	if l.lifecycleEnabled() && l.lifecycleMgr != nil && req != nil {
		if current, ok := l.lifecycleMgr.Current(req.RunID); ok {
			runtimeSect = fmt.Sprintf(
				"## DreamWeaver Runtime\n\nCurrent lifecycle state: %s.\nUse compact tool calls and leave the session resumable if interrupted.\n",
				current.String(),
			)
		}
	}

	// 4-2: Discover project rule files (.claude/ dir + CLAUDE.md) and inject.
	if l.dreamweaverEnabled() {
		rulesSect = l.loadProjectRuleFiles()
	}

	return
}

// loadProjectRuleFiles scans the workspace for CLAUDE.md and .claude/*.md rule files
// and returns their combined content as a DreamWeaver prompt section.
// Files larger than 32 KB per file are truncated to avoid blowing the context window.
func (l *Loop) loadProjectRuleFiles() string {
	if l.workspace == "" {
		return ""
	}
	const maxFileBytes = 32 * 1024
	type ruleFile struct {
		path    string
		content string
	}
	var found []ruleFile

	// Check root CLAUDE.md
	claudeMD := filepath.Join(l.workspace, "CLAUDE.md")
	if data, err := os.ReadFile(claudeMD); err == nil {
		if len(data) > maxFileBytes {
			data = data[:maxFileBytes]
		}
		found = append(found, ruleFile{path: "CLAUDE.md", content: string(data)})
	}

	// Scan .claude/ directory for *.md files
	claudeDir := filepath.Join(l.workspace, ".claude")
	if entries, err := os.ReadDir(claudeDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fp := filepath.Join(claudeDir, e.Name())
			data, err := os.ReadFile(fp)
			if err != nil {
				continue
			}
			if len(data) > maxFileBytes {
				data = data[:maxFileBytes]
			}
			found = append(found, ruleFile{path: filepath.Join(".claude", e.Name()), content: string(data)})
		}
	}

	if len(found) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Project Rules\n\n")
	sb.WriteString("The following project-specific rules and guidelines apply to this workspace:\n\n")
	for _, rf := range found {
		sb.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", rf.path, strings.TrimSpace(rf.content)))
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// intentHasAssignedSubTasks returns true when at least one SubTask has a non-empty AgentID.
func intentHasAssignedSubTasks(intent *spirit.Intent) bool {
	for _, st := range intent.SubTasks {
		if st.AgentID != "" {
			return true
		}
	}
	return false
}

// delegateAgentExecutor bridges spirit.AgentExecutor to the tools.DelegateRunFunc so
// the spirit orchestrator can dispatch sub-tasks via the existing delegation infrastructure.
type delegateAgentExecutor struct {
	runFn        tools.DelegateRunFunc
	fromAgentKey string
}

func (e *delegateAgentExecutor) Execute(ctx context.Context, agentKey string, task spirit.SubTask) (string, error) {
	req := tools.DelegateRequest{
		ToAgentKey:   agentKey,
		Task:         task.Description,
		DelegationID: fmt.Sprintf("spirit-%s", task.ID),
		FromAgentKey: e.fromAgentKey,
	}
	result, err := e.runFn(ctx, req)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

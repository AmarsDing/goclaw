package agent

import (
	"context"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/lifecycle"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
	"github.com/nextlevelbuilder/goclaw/pkg/sdk"
)

// runViaPipeline delegates a run to the v3 pipeline.
func (l *Loop) runViaPipeline(ctx context.Context, req RunRequest) (*RunResult, error) {
	input := convertRunInput(&req)
	// Bridge runState shares loop detection state between pipeline and agent.
	bridgeRS := &runState{}
	deps := l.buildPipelineDeps(&req, bridgeRS)

	model := l.model
	if req.ModelOverride != "" {
		model = req.ModelOverride
	}
	provider := l.provider
	if req.ProviderOverride != nil {
		provider = req.ProviderOverride
	}

	p := pipeline.NewDefaultPipeline(deps)
	state := pipeline.NewRunState(input, nil, model, provider)

	l.registerLifecycleRun(req.RunID, req.UserID)
	defer func() {
		l.cleanupLifecycleRun(req.RunID)
		l.deleteTranscript(req.RunID)
	}()
	l.transitionLifecycle(ctx, req.RunID, lifecycle.StateInitializing, "pipeline run started")

	pResult, err := p.Run(ctx, state)
	if err != nil {
		l.transitionLifecycle(ctx, req.RunID, lifecycle.StateFailed, err.Error())
		return nil, err
	}
	l.transitionLifecycle(ctx, req.RunID, lifecycle.StateCompleted, "pipeline run completed")
	return convertRunResult(pResult), nil
}

// buildPipelineDeps maps Loop fields + methods to PipelineDeps callbacks.
func (l *Loop) buildPipelineDeps(req *RunRequest, bridgeRS *runState) pipeline.PipelineDeps {
	maxIter := l.maxIterations
	if req.MaxIterations > 0 && req.MaxIterations < maxIter {
		maxIter = req.MaxIterations
	}

	cb := l.pipelineCallbacks(req, bridgeRS)
	loadSessionHistory := cb.loadSessionHistory
	if l.resumeEnabled() {
		loadSessionHistory = func(ctx context.Context, sessionKey string) ([]providers.Message, string) {
			l.transitionLifecycle(ctx, req.RunID, lifecycle.StateResuming, "loading session history")
			history, summary := cb.loadSessionHistory(ctx, sessionKey)
			if l.resumeEng == nil || len(history) == 0 {
				return history, summary
			}
			resumed, err := l.resumeEng.Resume(ctx, history, nil)
			if err != nil || resumed == nil {
				return history, summary
			}
			return resumed.Messages, summary
		}
	}

	buildMessages := cb.buildMessages
	buildMessages = func(ctx context.Context, input *pipeline.RunInput, history []providers.Message, summary string) ([]providers.Message, error) {
		msgs, err := cb.buildMessages(ctx, input, history, summary)
		if err != nil {
			return nil, err
		}
		l.storeTranscript(req.RunID, req.SessionKey, msgs)
		return msgs, nil
	}

	callLLM := cb.callLLM
	callLLM = func(ctx context.Context, state *pipeline.RunState, chatReq providers.ChatRequest) (*providers.ChatResponse, error) {
		providerName := ""
		if state.Provider != nil {
			providerName = state.Provider.Name()
		}
		l.transitionLifecycle(ctx, req.RunID, lifecycle.StateThinking, "calling llm")
		if l.hooksEnabled() && l.hookRegistry != nil {
			_, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
				Event:      hooks.EventPreThink,
				RunID:      req.RunID,
				AgentID:    l.id,
				SessionKey: req.SessionKey,
				Timestamp:  time.Now(),
				Data: hooks.ThinkPayload{
					Iteration: state.Iteration,
					Model:     state.Model,
					Provider:  providerName,
				}.AsMap(),
			})
		}
		if tr := l.transcriptForRun(req.RunID); tr != nil {
			apiView := tr.ForAPI(req.RunID)
			if len(apiView.Messages) > 0 {
				chatReq.Messages = apiView.Messages
			}
		}
		resp, err := cb.callLLM(ctx, state, chatReq)
		if err == nil && resp != nil {
			l.appendTranscript(req.RunID, providers.Message{
				Role:                "assistant",
				Content:             resp.Content,
				Thinking:            resp.Thinking,
				ToolCalls:           resp.ToolCalls,
				Phase:               resp.Phase,
				RawAssistantContent: resp.RawAssistantContent,
			})
		}
		if l.hooksEnabled() && l.hookRegistry != nil {
			_, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
				Event:      hooks.EventPostThink,
				RunID:      req.RunID,
				AgentID:    l.id,
				SessionKey: req.SessionKey,
				Timestamp:  time.Now(),
				Data: hooks.ThinkPayload{
					Iteration: state.Iteration,
					Model:     state.Model,
					Provider:  providerName,
				}.AsMap(),
			})
		}
		return resp, err
	}

	pruneMessages := cb.pruneMessages
	if l.compressionEnabled() && l.compressionEng != nil {
		pruneMessages = func(msgs []providers.Message, budget int) []providers.Message {
			result, compressed, err := l.compressionEng.CompressToBudget(context.Background(), msgs, budget)
			if err != nil || !compressed || result == nil {
				return cb.pruneMessages(msgs, budget)
			}
			return result.Messages
		}
	}

	compactMessages := cb.compactMessages
	if l.compressionEnabled() && l.compressionEng != nil {
		compactMessages = func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error) {
			l.transitionLifecycle(ctx, req.RunID, lifecycle.StateCompacting, "compacting context")
			result, compressed, err := l.compressionEng.CompressToBudget(ctx, msgs, l.contextWindow)
			if err == nil && compressed && result != nil {
				return result.Messages, nil
			}
			return cb.compactMessages(ctx, msgs, model)
		}
	}

	executeToolCall := cb.executeToolCall
	executeToolCall = func(ctx context.Context, state *pipeline.RunState, tc providers.ToolCall) ([]providers.Message, error) {
		l.transitionLifecycle(ctx, req.RunID, lifecycle.StateActing, "executing tool")
		if l.governorEnabled() && l.governor != nil {
			decision, err := l.governor.Evaluate(ctx, permissions.PermissionRequest{
				AgentID:   l.id,
				ToolName:  tc.Name,
				Arguments: tc.Arguments,
				UserID:    req.UserID,
				RunID:     req.RunID,
				Channel:   req.Channel,
			})
			if err != nil {
				return nil, err
			}
			if l.sdkBridgeEnabled() && l.sdkBridge != nil && decision != nil && decision.Action == permissions.ActionAsk {
				l.sdkBridge.Emit(sdk.Event{
					Type:       sdk.EventPermissionRequest,
					RunID:      req.RunID,
					AgentID:    l.id,
					SessionKey: req.SessionKey,
					Data: sdk.PermissionRequestData{
						RequestID: tc.ID,
						ToolName:  tc.Name,
						Arguments: tc.Arguments,
						Risk:      decision.Classification.Risk,
						Class:     string(decision.Classification.Class),
					},
				})
			}
			if decision != nil && decision.Action == permissions.ActionAsk {
				l.transitionLifecycle(ctx, req.RunID, lifecycle.StateAwaitingApproval, decision.Reason)
				resolved, resolveErr := l.governor.ResolveApproval(ctx, permissions.PermissionRequest{
					AgentID:   l.id,
					ToolName:  tc.Name,
					Arguments: tc.Arguments,
					UserID:    req.UserID,
					RunID:     req.RunID,
					Channel:   req.Channel,
				}, decision)
				if resolveErr != nil {
					return nil, resolveErr
				}
				if resolved != nil {
					decision = resolved
				}
				l.transitionLifecycle(ctx, req.RunID, lifecycle.StateActing, "approval resolved")
			}
			if decision != nil && decision.Action == permissions.ActionDeny {
				if l.sdkBridgeEnabled() && l.sdkBridge != nil {
					l.sdkBridge.Emit(sdk.Event{
						Type:       sdk.EventPermissionResult,
						RunID:      req.RunID,
						AgentID:    l.id,
						SessionKey: req.SessionKey,
						Data: sdk.PermissionResultData{
							RequestID: tc.ID,
							Allowed:   false,
							Reason:    decision.Reason,
						},
					})
				}
				return []providers.Message{{
					Role:       "tool",
					Content:    "[permission denied] " + decision.Reason,
					ToolCallID: tc.ID,
					IsError:    true,
				}}, nil
			}
		}

		if l.hooksEnabled() && l.hookRegistry != nil {
			results, _ := l.hookRegistry.Fire(ctx, hooks.Payload{
				Event:      hooks.EventPreToolUse,
				RunID:      req.RunID,
				AgentID:    l.id,
				SessionKey: req.SessionKey,
				Timestamp:  time.Now(),
				Data: hooks.ToolPayload{
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Arguments:  tc.Arguments,
					Channel:    req.Channel,
					RunKind:    req.RunKind,
				}.AsMap(),
			})
			if updatedTC, blocked, reason := applyToolHookResults(tc, results); blocked {
				return []providers.Message{{
					Role:       "tool",
					Content:    "[hook blocked] " + reason,
					ToolCallID: tc.ID,
					IsError:    true,
				}}, nil
			} else {
				tc = updatedTC
			}
		}

		if l.sdkBridgeEnabled() && l.sdkBridge != nil {
			l.sdkBridge.Emit(sdk.Event{
				Type:       sdk.EventToolCallStart,
				RunID:      req.RunID,
				AgentID:    l.id,
				SessionKey: req.SessionKey,
				Data: sdk.ToolCallStartData{
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Arguments:  tc.Arguments,
				},
			})
		}
		msgs, err := cb.executeToolCall(ctx, state, tc)
		if err == nil {
			l.appendTranscript(req.RunID, msgs...)
		}
		if l.hooksEnabled() && l.hookRegistry != nil {
			_, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
				Event:      hooks.EventPostToolUse,
				RunID:      req.RunID,
				AgentID:    l.id,
				SessionKey: req.SessionKey,
				Timestamp:  time.Now(),
				Data: hooks.ToolPayload{
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Arguments:  tc.Arguments,
					Channel:    req.Channel,
					RunKind:    req.RunKind,
				}.AsMap(),
			})
		}
		l.transitionLifecycle(ctx, req.RunID, lifecycle.StateObserving, "tool execution completed")
		if l.sdkBridgeEnabled() && l.sdkBridge != nil {
			l.sdkBridge.Emit(sdk.Event{
				Type:       sdk.EventToolCallEnd,
				RunID:      req.RunID,
				AgentID:    l.id,
				SessionKey: req.SessionKey,
				Data: sdk.ToolCallEndData{
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Success:    err == nil,
					Duration:   "",
				},
			})
		}
		return msgs, err
	}

	return pipeline.PipelineDeps{
		TokenCounter: tokencount.NewTiktokenCounter(),
		EventBus:     l.domainBus,
		Config: pipeline.PipelineConfig{
			MaxIterations:      maxIter,
			MaxToolCalls:       l.maxToolCalls,
			CheckpointInterval: 5,
			ContextWindow:      l.contextWindow,
			MaxTokens:          l.effectiveMaxTokens(),
			Compaction:         l.compactionCfg,
			// V3 memory/retrieval flags removed — always true at runtime.
		},
		// Resolve per-model context window once per run. Falls back to
		// Config.ContextWindow when registry/model is unknown (existing
		// behaviour unchanged for tests and lite edition).
		ResolveContextWindow: func(provider, model string) int {
			if l.modelRegistry == nil || model == "" {
				return 0
			}
			spec := l.modelRegistry.Resolve(provider, model)
			if spec == nil {
				return 0
			}
			return spec.ContextWindow
		},
		EmitEvent: func(event any) {
			if ae, ok := event.(AgentEvent); ok {
				l.emit(ae)
			}
		},

		// V3 auto-inject: episodic memory L0 injection into system prompt.
		// Captures agent/tenant context via closure for store scoping.
		AutoInject: l.makeAutoInjectCallback(req),

		// Context injection + session history
		InjectContext:      cb.injectContext,
		LoadSessionHistory: loadSessionHistory,

		// Context callbacks
		ResolveWorkspace: cb.resolveWorkspace,
		LoadContextFiles: cb.loadContextFiles,
		BuildMessages:    buildMessages,
		EnrichMedia:      cb.enrichMedia,
		InjectReminders:  cb.injectReminders,

		// Think callbacks
		BuildFilteredTools: cb.buildFilteredTools,
		CallLLM:            callLLM,
		UniqueToolCallIDs:  uniquifyToolCallIDs,
		EmitBlockReply: func(content string) {
			sanitized := SanitizeAssistantContent(content)
			if sanitized != "" && !IsSilentReply(sanitized) {
				cb.emitRun(AgentEvent{
					Type:    protocol.AgentEventBlockReply,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": sanitized},
				})
				if l.sdkBridgeEnabled() && l.sdkBridge != nil {
					l.sdkBridge.Emit(sdk.Event{
						Type:       sdk.EventAssistantDelta,
						RunID:      req.RunID,
						AgentID:    l.id,
						SessionKey: req.SessionKey,
						Data:       sdk.DeltaData{Content: sanitized},
					})
				}
			}
		},

		// Prune callbacks
		PruneMessages:   pruneMessages,
		CompactMessages: compactMessages,

		// Memory flush
		RunMemoryFlush: cb.runMemoryFlush,

		// Tool callbacks
		ExecuteToolCall:   executeToolCall,
		ExecuteToolRaw:    cb.executeToolRaw,
		ProcessToolResult: cb.processToolResult,
		ToolConcurrencySafe: func(toolName string) bool {
			registry, ok := l.tools.(*tools.Registry)
			if !ok || registry == nil {
				return false
			}
			return registry.IsConcurrencySafe(l.resolveToolCallName(toolName))
		},
		CheckReadOnly:     cb.checkReadOnly,

		// Observe: drain InjectCh
		DrainInjectCh: func() []providers.Message {
			if req.InjectCh == nil {
				return nil
			}
			var msgs []providers.Message
			for {
				select {
				case injected := <-req.InjectCh:
					msgs = append(msgs, providers.Message{
						Role:    "user",
						Content: injected.Content,
					})
				default:
					return msgs
				}
			}
		},

		// Checkpoint + Finalize
		FlushMessages:          cb.flushMessages,
		SkillPostscript:        l.makeSkillPostscript(),
		SanitizeContent:        cb.sanitizeContent,
		StripMessageDirectives: StripMessageDirectives,
		DeduplicateMediaSuffix: deduplicateMediaSuffix,
		IsSilentReply:          IsSilentReply,
		EmitSessionCompleted: func(ctx context.Context, sessionKey string, msgCount, tokensUsed, compactionCount int) {
			if l.hooksEnabled() && l.hookRegistry != nil {
				_, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
					Event:      hooks.EventSessionEnd,
					RunID:      req.RunID,
					AgentID:    l.id,
					SessionKey: sessionKey,
					Timestamp:  time.Now(),
					Data: hooks.SessionPayload{
						Channel: req.Channel,
						RunKind: req.RunKind,
						UserID:  req.UserID,
					}.AsMap(),
				})
			}
			if l.domainBus != nil {
				// Include existing session summary (from previous compaction cycles).
				// Current cycle's compaction runs async so its summary isn't ready yet,
				// but previous summaries are available and useful for episodic creation.
				var summary string
				if compactionCount > 0 {
					summary = l.sessions.GetSummary(ctx, sessionKey)
				}
				l.domainBus.Publish(eventbus.DomainEvent{
					Type:     eventbus.EventSessionCompleted,
					TenantID: l.tenantID.String(),
					AgentID:  l.agentUUID.String(),
					UserID:   req.UserID,
					SourceID: sessionKey,
					Payload: &eventbus.SessionCompletedPayload{
						SessionKey:      sessionKey,
						MessageCount:    msgCount,
						TokensUsed:      tokensUsed,
						CompactionCount: compactionCount,
						Summary:         summary,
					},
				})
			}
			if l.sdkBridgeEnabled() && l.sdkBridge != nil {
				l.sdkBridge.Emit(sdk.Event{
					Type:       sdk.EventRunCompleted,
					RunID:      req.RunID,
					AgentID:    l.id,
					SessionKey: sessionKey,
					Data: sdk.RunCompletedData{
						Content:    bridgeRS.finalContent,
						Iterations: bridgeRS.iteration,
						ToolCalls:  bridgeRS.totalToolCalls,
						TokensUsed: tokensUsed,
					},
				})
			}
			l.persistDreamWeaverContinuity(ctx, req, sessionKey, tokensUsed, bridgeRS.finalContent, compactionCount)
		},
		UpdateMetadata:   cb.updateMetadata,
		BootstrapCleanup: cb.bootstrapCleanup,
		MaybeSummarize:   cb.maybeSummarize,
	}
}

func applyToolHookResults(tc providers.ToolCall, results []hooks.Result) (providers.ToolCall, bool, string) {
	updated := tc
	for _, result := range results {
		switch result.Action {
		case hooks.ResultActionBlock:
			reason := strings.TrimSpace(result.Output)
			if reason == "" {
				reason = "blocked by hook"
			}
			return tc, true, reason
		case hooks.ResultActionModify:
			if result.Mutations == nil {
				continue
			}
			if args, ok := result.Mutations["arguments"].(map[string]any); ok {
				updated.Arguments = args
			}
			if name, ok := result.Mutations["tool_name"].(string); ok && name != "" {
				updated.Name = name
			}
		}
	}
	return updated, false, ""
}

// convertRunInput converts agent.RunRequest to pipeline.RunInput.
func convertRunInput(req *RunRequest) *pipeline.RunInput {
	return &pipeline.RunInput{
		SessionKey:        req.SessionKey,
		Message:           req.Message,
		Media:             req.Media,
		ForwardMedia:      req.ForwardMedia,
		Channel:           req.Channel,
		ChannelType:       req.ChannelType,
		ChatTitle:         req.ChatTitle,
		ChatID:            req.ChatID,
		PeerKind:          req.PeerKind,
		RunID:             req.RunID,
		UserID:            req.UserID,
		SenderID:          req.SenderID,
		Stream:            req.Stream,
		ExtraSystemPrompt: req.ExtraSystemPrompt,
		SkillFilter:       req.SkillFilter,
		HistoryLimit:      req.HistoryLimit,
		ToolAllow:         req.ToolAllow,
		LightContext:      req.LightContext,
		RunKind:           req.RunKind,
		DelegationID:      req.DelegationID,
		TeamID:            req.TeamID,
		TeamTaskID:        req.TeamTaskID,
		ParentAgentID:     req.ParentAgentID,
		MaxIterations:     req.MaxIterations,
		ModelOverride:     req.ModelOverride,
		HideInput:         req.HideInput,
		ContentSuffix:     req.ContentSuffix,
		LeaderAgentID:     req.LeaderAgentID,
		WorkspaceChannel:  req.WorkspaceChannel,
		WorkspaceChatID:   req.WorkspaceChatID,
		TeamWorkspace:     req.TeamWorkspace,
	}
}

// convertRunResult converts pipeline.RunResult to agent.RunResult.
func convertRunResult(pr *pipeline.RunResult) *RunResult {
	if pr == nil {
		return nil
	}
	media := make([]MediaResult, len(pr.MediaResults))
	for i, m := range pr.MediaResults {
		media[i] = MediaResult{
			Path:        m.Path,
			ContentType: m.ContentType,
			Size:        m.Size,
			AsVoice:     m.AsVoice,
		}
	}
	return &RunResult{
		Content:        pr.Content,
		Thinking:       pr.Thinking,
		RunID:          pr.RunID,
		Iterations:     pr.Iterations,
		Usage:          &pr.TotalUsage,
		Media:          media,
		Deliverables:   pr.Deliverables,
		BlockReplies:   pr.BlockReplies,
		LastBlockReply: pr.LastBlockReply,
		LoopKilled:     pr.LoopKilled,
	}
}

// makeAutoInjectCallback creates the AutoInject callback that captures agent/tenant context.
// Returns nil if autoInjector is not configured (v3 retrieval disabled or no episodic store).
// Phase 9: plumbs recentContext through to enrich vector search queries for
// context-aware recall.
func (l *Loop) makeAutoInjectCallback(req *RunRequest) func(ctx context.Context, userMessage, userID, recentContext string) (string, error) {
	if l.autoInjector == nil {
		return nil
	}
	return func(ctx context.Context, userMessage, userID, recentContext string) (string, error) {
		result, err := l.autoInjector.Inject(ctx, memory.InjectParams{
			AgentID:       l.agentUUID.String(),
			UserID:        userID,
			TenantID:      store.TenantIDFromContext(ctx).String(),
			UserMessage:   userMessage,
			RecentContext: recentContext,
		})
		if err != nil || result == nil {
			return "", err
		}
		return result.Section, nil
	}
}

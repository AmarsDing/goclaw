# 对话 Query 循环与流式处理

## 功能概述

**Agent 式对话循环**是产品的核心：`query()` 内部通过 `queryLoop` 实现 **`while (true)`** 迭代——每一轮向 Claude API 发送当前消息历史（经上下文裁剪、压缩、用户/系统上下文注入后），**流式**消费模型输出；若出现 `tool_use`，则执行工具并把 **`tool_result` 以 UserMessage 形式写回历史**，再进入下一轮，直到模型不再产生需跟进的工具调用，并走完停止钩子（stop hooks）、Token 预算等收尾逻辑。

该循环还与以下机制交织：

- **自动压缩**（`autoCompactIfNeeded`）、**微压缩**（`microcompactMessages`）、可选 **snip**、**上下文折叠**（`CONTEXT_COLLAPSE`）等，用于控制上下文长度与缓存。
- **Token 告警与阻断**（`calculateTokenWarningState`）、**按轮次 Token 预算**（`createBudgetTracker` / `checkTokenBudget`，受 `TOKEN_BUDGET` 特性控制）。
- **流式工具执行**（`StreamingToolExecutor`）与批式 `runTools()` 二选一，由 Statsig gate `tengu_streaming_tool_execution2` 等决定。
- **`max_output_tokens` 恢复**、**模型回退**（`FallbackTriggeredError`）、**可暂存后释放的 API 错误**（prompt-too-long、媒体体积等与 reactive compact / collapse 协作）。

下文按「从用户敲回车到循环退出」顺序说明，并附**文件索引表**与**循环流程图**。

---

## 从 main 函数入口的实现流程

### 1. 输入处理到 Query 触发

| 环节 | 文件 / 符号 | 说明 |
|------|-------------|------|
| 提交入口 | `src/utils/handlePromptSubmit.ts` | `handlePromptSubmit(params)`：处理队列优先路径、校验、引用展开、与 `queryGuard` 协作等。 |
| 核心执行 | 同文件 `executeUserInput()` | 在通过守卫后，调用 `processUserInput` 得到新消息与 `shouldQuery`，再按需调用传入的 `onQuery`。 |
| 路由与建消息 | `src/utils/processUserInput/processUserInput.ts` | `processUserInput()`：区分斜杠命令与普通文本、附件、图片、IDE 选区等；`processUserInputBase()`（及关联函数）构建 `UserMessage` / 系统消息等并返回 `shouldQuery`、`allowedTools` 等。 |

`handlePromptSubmit` 与 REPL 的 `PromptInput` 通过 props 注入的 `onQuery`、`getToolUseContext`、`setAppState` 等连接，实现 **UI 与查询层解耦**。

### 2. Query 入口（REPL 层）

| 符号 | 位置 | 职责 |
|------|------|------|
| `onQuery` | `src/screens/REPL.tsx` | 包装层：在调用 `onQueryImpl` 前后处理 loading、`queryGuard`、中断恢复、`onBeforeQuery` 等。 |
| `onQueryImpl` | 同上 | **真正准备一次 query**：IDE 侧准备（如关 diff）、会话标题、将本回合 `additionalAllowedTools` 写入 `toolPermissionContext.alwaysAllowRules.command`、`getToolUseContext`、并行拉取 `getSystemPrompt` / `getUserContext` / `getSystemContext`、权限模式检查，最后调用导出的 `query()` 异步生成器并迭代事件。 |
| `onQueryEvent` | 同上 | 对 `query()` 产出的每条事件调用 `handleMessageFromStream`，更新 `messages`、Spinner、`streamingToolUses`、thinking、TTFT 指标等。 |

REPL 内对 **压缩边界消息**、**进度条去重**（如部分 ephemeral progress）、**tombstone** 等有特殊分支，避免 UI 与会话日志爆炸。

### 3. Query 核心循环

| 文件 / 符号 | 说明 |
|-------------|------|
| `src/query.ts` — `query()` | 顶层异步生成器：调用 `yield* queryLoop(...)`，正常返回后遍历 `consumedCommandUuids` 通知命令生命周期 `completed`。 |
| `queryLoop()` | 私有生成器：**`while (true)`** 主循环；维护可变 `State`（messages、toolUseContext、压缩跟踪、max_output 恢复计数、pendingToolUseSummary、stopHookActive、turnCount、`transition` 等）。 |
| `src/query/deps.ts` — `productionDeps()` | 生产依赖注入：`callModel` → `queryModelWithStreaming`；`microcompact`；`autocompact` → `autoCompactIfNeeded`；`uuid`。测试可传入 `deps` 覆盖。 |
| `src/query/config.ts` — `buildQueryConfig()` | 在 `query()` 入口快照一次 **非 `feature()`** 的运行时门控，如 `streamingToolExecution`、`emitToolUseSummaries`、`isAnt`、`fastModeEnabled`。 |
| `src/query/tokenBudget.ts` | `createBudgetTracker()`、`checkTokenBudget()`：在 `TOKEN_BUDGET` 开启且非子代理时，根据本回合产出 Token 与预算决定 **自动续写**（注入 meta UserMessage）或 **停止并打点**。 |
| `src/query/stopHooks.ts` — `handleStopHooks()` | 在「本轮模型已结束且无需工具跟进」时执行：用户配置的停止钩子、任务完成钩子等；可 **`preventContinuation`** 或注入 **blocking 错误消息** 触发下一轮迭代。 |

### 4. `queryLoop` 单次迭代步骤（逻辑顺序）

以下对应 `src/query.ts` 中一轮循环的主要阶段，便于对照源码阅读。

1. **产出 `stream_request_start`**  
   通知 UI 进入「请求中」Spinner 状态。

2. **更新 query 链跟踪**  
   `queryTracking`（`chainId` / `depth`）用于分析与日志。

3. **准备 `messagesForQuery`**  
   从 `getMessagesAfterCompactBoundary(messages)` 取出发往 API 的视图；再按需 `applyToolResultBudget`、可选 **snip**、`deps.microcompact`、可选 **context collapse**。

4. **组装 system prompt**  
   `fullSystemPrompt = asSystemPrompt(appendSystemContext(systemPrompt, systemContext))`；用户侧上下文通过 `prependUserContext` 在调用 API 时并入。

5. **`deps.autocompact`**  
   若需要则压缩对话，`buildPostCompactMessages` 生成新消息序列，**逐条 `yield`** 到 UI，并用压缩后的数组继续本轮后续步骤；同时维护 `autoCompactTracking`、`taskBudgetRemaining`（若启用 `taskBudget`）。

6. **Token 阻断检查**  
   在未刚压缩等条件下，用 `calculateTokenWarningState` 判断是否达到硬阻断，必要时 `yield` 错误助手消息并 `return { reason: 'blocking_limit' }`。

7. **调用 `deps.callModel({...})`**  
   内层 `for await` 消费流：每条消息经 **withhold** 判断（prompt-too-long、media、max_output_tokens 等可能暂不 yield），否则 `yield` 给 UI；`assistant` 消息收集到 `assistantMessages`，并驱动 `StreamingToolExecutor.addTool`（若启用）。

8. **流式工具结果**  
   在流式过程中即可 `yield` 已完成工具的 `tool_result` 消息；流结束后若仍使用 executor，还需 `getRemainingResults()` 兜底，避免 **tool_use 无 tool_result**。

9. **若 `abortController` 已取消**  
   消费 executor 剩余结果或合成中断 `tool_result`，`yield` 用户中断消息（部分 reason 跳过），`return`。

10. **若本轮无 `needsFollowUp`（无 tool_use）**  
    - 处理 **暂存类错误** 的恢复路径（collapse drain、reactive compact、max_output_tokens 升级到 `ESCALATED_MAX_TOKENS`、多轮 recovery UserMessage 等）。  
    - 若仍为 API 错误则避免进入 stop hooks 死循环。  
    - 否则 **`yield* handleStopHooks(...)`**；若阻止继续则返回。  
    - **`checkTokenBudget`**：可能注入续写 meta 消息并 `continue`。  
    - 否则 **`return { reason: 'completed' }`**。

11. **若有 tool_use**  
    - `toolUpdates = streamingToolExecutor ? getRemainingResults() : runTools(...)`，迭代 `yield` 每条工具结果并累积 `toolResults`。  
    - 处理 abort、hook 阻止继续、`maxTurns` 限制。  
    - **`getAttachmentMessages`**、memory prefetch、skill prefetch、队列命令 drain 等附加消息。  
    - 刷新工具列表（`refreshTools`）。  
    - 构造下一轮 **`state.messages = [...messagesForQuery, ...assistantMessages, ...toolResults]`**，`turnCount++`，**`continue`** 进入下一次 `while (true)`。

### 5. 流式事件处理

| 文件 | 符号 | 说明 |
|------|------|------|
| `src/utils/messages.ts` | `handleMessageFromStream(...)` | 统一处理 **完整 Message** 与 **`stream_event` / `stream_request_start`**：更新响应长度、Spinner 模式、流式文本、流式 `tool_use` JSON、`thinking_delta`、TTFT、`tombstone` 移除等。 |

典型 `stream_event` 分支包括：`message_start`、`content_block_start`（text / thinking / tool_use / 多种 server tool 类型）、`content_block_delta`（`text_delta`、`input_json_delta`、`thinking_delta` 等）、`message_stop`、`message_delta` 等。

### 6. 工具执行与循环闭合

| 文件 | 符号 | 说明 |
|------|------|------|
| `src/services/tools/toolOrchestration.ts` | `runTools()` | `partitionToolCalls` 将工具调用分为 **可并发只读批** 与 **需串行或非只读** 批；并发路径用 `runToolsConcurrently`，串行路径用 `runToolsSerially`；每步可更新 `ToolUseContext`。 |
| `src/services/tools/StreamingToolExecutor.ts` | `StreamingToolExecutor` | 在模型仍输出时并行启动工具；与 `query.ts` 中 `getCompletedResults` / `getRemainingResults` / `discard` 配合。 |
| `src/services/tools/toolExecution.ts` | `runToolUse()` | 单个 tool 的权限、钩子与实际调用（由 orchestration 批量调度）。 |

工具产出写入 `messages` 后，下一轮 `queryLoop` 将这些 **UserMessage（tool_result）** 与上一轮 **AssistantMessage（tool_use）** 一并发给模型，形成 **ReAct 式闭环**。

### 7. 自动压缩与相关模块

| 文件 | 符号 | 说明 |
|------|------|------|
| `src/services/compact/autoCompact.ts` | `autoCompactIfNeeded()`、`calculateTokenWarningState()` | 根据 Token 与配置决定是否触发自动压缩；告警/阻断状态计算。 |
| `src/services/compact/compact.ts` | `compactConversation()`、`buildPostCompactMessages()` | 执行压缩并生成压缩后消息列表（摘要、附件、钩子等）。 |
| `src/services/compact/microCompact.ts` | `microcompactMessages()` | 轻量级微压缩，在 autocompact 之前缩小可缓存片段等。 |

`query.ts` 中还包含与 **reactive compact**、**CACHED_MICROCOMPACT**、**HISTORY_SNIP** 等特性开关协作的分支，阅读时可从 `yield*` / `continue` / `state = next` 三类控制流入手。

---

## 关键文件索引

| 分类 | 路径 | 说明 |
|------|------|------|
| 提交 | `src/utils/handlePromptSubmit.ts` | `handlePromptSubmit`、`executeUserInput` |
| 输入路由 | `src/utils/processUserInput/processUserInput.ts` | `processUserInput`、`processUserInputBase` |
| REPL | `src/screens/REPL.tsx` | `onQuery`、`onQueryImpl`、`onQueryEvent` |
| 循环 | `src/query.ts` | `query`、`queryLoop` |
| 依赖 | `src/query/deps.ts` | `productionDeps`、`QueryDeps` |
| 配置 | `src/query/config.ts` | `buildQueryConfig` |
| Token 预算 | `src/query/tokenBudget.ts` | `createBudgetTracker`、`checkTokenBudget` |
| 停止钩子 | `src/query/stopHooks.ts` | `handleStopHooks` |
| 流式 UI | `src/utils/messages.ts` | `handleMessageFromStream` |
| API | `src/services/api/claude.js`（或同目录 ts） | `queryModelWithStreaming` |
| 工具 | `src/services/tools/toolOrchestration.ts` | `runTools` |
| 工具 | `src/services/tools/StreamingToolExecutor.ts` | 流式执行器 |
| 工具 | `src/services/tools/toolExecution.ts` | `runToolUse` |
| 压缩 | `src/services/compact/autoCompact.ts` | 自动压缩入口 |
| 压缩 | `src/services/compact/compact.ts` | 压缩构建消息 |
| 压缩 | `src/services/compact/microCompact.ts` | 微压缩 |

---

## 循环流程图（文本示意）

```
                    ┌──────────────────────────┐
                    │ 用户提交 / 队列出队        │
                    └────────────┬─────────────┘
                                 ▼
                    ┌──────────────────────────┐
                    │ handlePromptSubmit         │
                    │  → executeUserInput        │
                    │  → processUserInput        │
                    └────────────┬─────────────┘
                                 ▼
                    ┌──────────────────────────┐
                    │ REPL.onQuery               │
                    │  → onQueryImpl             │
                    │  → for await query()       │
                    │       onQueryEvent(事件)   │
                    └────────────┬─────────────┘
                                 ▼
         ┌───────────────────────────────────────────────┐
         │  queryLoop  while (true)                       │
         │    yield stream_request_start                   │
         │    准备 messagesForQuery（边界/snip/微压/折叠）   │
         │    autocompact → 可能 yield 压缩消息            │
         │    callModel 流式 → yield 增量/助手消息         │
         │    ┌─────────────────────────────────────────┐ │
         │    │ 有 tool_use ?                            │ │
         │    └─────────┬───────────────────┬───────────┘ │
         │              │ 否                │ 是          │
         │              ▼                  ▼             │
         │    错误恢复 / stop hooks      runTools 或      │
         │    / token budget              StreamingExec   │
         │    / return completed          → tool_results  │
         │                               → 合并 messages   │
         │                               → continue ─────┘
         └───────────────────────────────────────────────┘
```

---

## 小结

**Query 层**（`query.ts`）与 **REPL 层**（`onQueryImpl` + `handleMessageFromStream`）通过异步生成器 **解耦**：前者专注 API、压缩、工具与状态机，后者专注 **终端状态与消息数组**。阅读时建议先跟踪 **`yield { type: 'stream_request_start' }` 到 `callModel` 再到 `runTools` / `continue`** 的主路径，再按需展开 withhold、reactive compact 与 stop hooks 等分支。

# 02 — Runtime Continuity（运行时连续性）

对应长上下文与记忆：**分层压缩**、**主题/日粒度记忆**、**注入漂移检测**。

## 1. 分层上下文压缩 — `internal/compression/`

| 文件 | 职责 |
|------|------|
| `levels.go` | `Level` L0–L4：Microcompact、Snip、Full Compact（依赖外部 `Compactor`）、Context Collapse、Cache Reset；`Thresholds` 按上下文占用比例触发；`Engine.Compress` |

| 层级 | 行为摘要 |
|------|----------|
| L0 | 单条 tool 结果过长时截断并附说明 |
| L1 | 保留最近 N 条 tool 详情，更早的替换为 snip 摘要 |
| L2 | 调用注入的 `Compactor`（整段摘要） |
| L3 | 对 L2 结果再次 compact |
| L4 | 保留 system + 最近用户轮，必要时对前文再 compact |

**测试**：`levels_test.go`。

## 2. 巩固与日志 — `internal/consolidation/`（扩展）

| 文件 | 职责 |
|------|------|
| `topic_worker.go` | `TopicWorker`：将 episodic 输入聚类为 `TopicMemory`，`TopicStore` / `TopicExtractor` 接口；`GenerateWikilinks` 辅助生成 wikilink 文本 |
| `daily_log.go` | `DailyLogWorker`：按日聚合 `SessionSummaryInput`，经 `DailySummariser` 写入 `DailyLogStore` |

**说明**：存储与 LLM 摘要器由调用方注入，本库只定义数据结构与编排流程。

## 3. 记忆漂移 — `internal/memory/`（扩展）

| 文件 | 职责 |
|------|------|
| `drift_detector.go` | `DriftDetector`：`RecordInjection`、`Tick`、`ShouldRefresh`、`CheckDrift`（需外部 `scorer`） |

与既有 `AutoInjector`、`InjectParams` 文档见 [07-bootstrap-skills-memory.md](../07-bootstrap-skills-memory.md)。

---

## Ideas：融合需求对照（Claude Code → goclaw）

> 原载于 [DreamWeaver README](./README.md)「Ideas 需求索引」；**§1 的 E–G** 与本章「Runtime Continuity」主题对应。

#### E. Context / Memory / Prompt Assembly

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **分段系统提示词** `$X` sections 带 per-section cache | `prompt-assembly` docs, `queryContext.ts` | `systemprompt.go` 分段构建 + cache boundary | 🟢 已实现 | 结构对齐 |
| **CLAUDE.md 分层规则** enterprise→user→project→local→rules/ | `sj()` discovery, `claudemd.js` | Agent context files（无分层 .md 发现） | 🟡 部分实现 | 缺少自动扫描项目 `CLAUDE.md` 链路 |
| **Session Memory** 后台 fork agent 生成摘要写入 memory | `sessionMemoryCompact.ts` | 无 | 🔴 未实现 | 需新增后台 summarizer goroutine |
| **MCP instructions** 作为 system prompt section + cache break | `mcp_instructions` section | 无独立 MCP instructions section | 🔴 未实现 | MCP 指令未作为系统提示词段注入 |
| **Memoized context** getUserContext/getSystemContext 缓存 | `context.ts` memoization | systemprompt 无缓存，每次重建 | 🟡 部分实现 | 需缓存高开销段（git status 等）|

#### F. Compression / Compaction

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **多级压缩** micro→snip→autocompact→full→session memory | `compact.ts`, `microCompact.ts`, `snipCompact.ts`, `autoCompact.ts`, `sessionMemoryCompact.ts` | `internal/compression/levels.go` L0-L4 五级 | 🟢 已实现 | 策略名不同但层级数量对齐 |
| **Pre/Post compact hooks** | `executePreCompactHooks`, `executePostCompactHooks` | 无 | 🔴 未实现 | hooks 系统已有基础，需注册 compact 事件 |
| **Compact boundary message** 压缩边界作为消息类型嵌入 transcript | `SystemCompactBoundaryMessage` | 无独立 boundary 消息 | 🔴 未实现 | 需在 message 包加 CompactBoundary 类型 |
| **Forked summarizer** 压缩时 fork 子 agent 做摘要 | `runForkedAgent` in compact | 无 | 🔴 未实现 | 当前压缩是规则式的，非 LLM-assisted |
| **preservedSegment** 压缩后保留段用于 rewind | `preservedSegment` in compact result | 无 | 🔴 未实现 | 需在 resume 中加 rewind 锚点 |

#### G. Resume / Session / Transcript

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **JSONL transcript** 追加写、100ms 批量刷盘 | `sessionStorage.ts` `iC4` writer | `internal/message/transcript.go` 内存 transcript | 🟡 部分实现 | goclaw transcript 在内存，未持久化 JSONL |
| **Resume = replay + repair** 中断修复 | `I76` resume, `hq4` interrupted-turn repair | `internal/resume/engine.go` Resume/Repair | 🟢 已实现 | 对齐 |
| **Fork** 分支会话（新 UUID，保留主链） | `yHz` fork, `forkedFrom` | 无 | 🔴 未实现 | 需在 session 层加 fork 支持 |
| **Subagent transcript** 独立 JSONL | `subagents/agent-<id>.jsonl` | 无独立 subagent 日志 | 🔴 未实现 | 多 agent 场景需要 |
| **Background sessions** CLI bg/ps/logs/attach/kill | CLI bg session management | 无 | 🔴 未实现 | 适合作为 CLI 模式扩展 |

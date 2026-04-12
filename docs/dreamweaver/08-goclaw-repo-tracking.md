# 08 — 追踪 goclaw 源库更新（Repo Change Tracking）

对本仓库 **goclaw** 的变更做持续追踪，输出可消费的差异与影响信息，支撑评审、回归与 Agent 上下文。

**状态：🔴 未实现**（自动化管线与模块映射均未落地；见文末「实现现状」）。

---

## 1. 目标与价值

| 维度 | 说明 |
|------|------|
| **问题** | 主分支/发布分支频繁合并时，人工难以快速回答：改了什么、动到哪些子系统、是否破坏对外行为、是否需迁移或配置变更。 |
| **目标** | 在 **可重复** 的流程中（CI 或本地/定时任务），从 **权威源**（git / 托管 API）拉取变更事实，生成 **结构化 + 人类可读** 的产物，并可选触发通知。 |
| **受众** | 维护者、发布负责人、在 goclaw 上构建产品的团队；未来可给 **站内 Agent** 提供「近期仓库变化」摘要，减少幻觉与过时假设。 |

---

## 2. 范围

### 2.1 范围内

- **追踪对象**：本 monorepo **goclaw**（根目录即仓库根；含 `cmd/`、`internal/`、`ui/`、`migrations/`、`docs/` 等）。
- **时间维度**：按 **提交 / 合并提交 / tag 间** 的区间产生报告（可配置 `from..to` 或「自上次发布 tag」）。
- **内容维度**：文件级变更清单、路径到子系统的映射、变更类型粗分类、对迁移/配置/CLI 标志的 **显式提示**（基于规则与路径，不要求初版即 LLM 深度分析）。

### 2.2 范围外（可另立项）

- 追踪 **依赖库**（`go.mod` 间接升级）的供应链全貌 — 可做简化（仅列出 `go.sum` / 依赖 diff），不强制 CVE 自动关联。
- **用户业务仓库** 的变更 — 本需求仅 **goclaw 源库**。
- 与 DreamWeaver **运行时** 的深度耦合（例如在 `Loop` 内自动注入「上周 diff」）— 可作为 **Phase 3+** 增强，非 P0。

---

## 3. 用户场景

1. **发布前**：从 `vX.Y.(Z-1)` 到 `vX.Y.Z` 一键生成变更说明，供 release notes 与回归清单使用。
2. **每日/每周同步**：定时对比 `origin/main` 与本地基线或上一快照，输出 Markdown changelog，发到 issue 或 IM。
3. **PR 评审辅助**：在 CI 中对 PR 的 `base...head` 跑同样管线，评论中附带「受影响子系统」与是否触碰 `migrations/`。
4. **Agent 辅助开发**：开发者或 Agent 拉取「最近 N 次合并」摘要，避免重复实现已删除的 API。

---

## 4. 功能需求

### 4.1 变更抓取（FR-C1）

| ID | 需求 | 说明 |
|----|------|------|
| FR-C1a | **Git 区间 diff** | 支持 `git diff --stat`、`name-status`、可选 patch 大小写；输入为两个 ref（branch/tag/commit）。 |
| FR-C1b | **提交列表** | 支持 `git log` 格式化输出：hash、作者、日期、标题、body（可截断）；可过滤 merge commit 展示方式。 |
| FR-C1c | **托管集成（可选）** | 若远程为 GitHub/GitLab，可通过 API 补充 PR 标题、链接、标签；**无 token 时仅 git 本地/克隆亦可运行**。 |

### 4.2 变更分类与风险评估（FR-C2）

| ID | 需求 | 说明 |
|----|------|------|
| FR-C2a | **路径规则分类** | 基于路径前缀将变更归入：`migrations`（**数据面**）、`internal/config`（**配置面**）、`cmd`（**CLI 面**）、`ui/web`（**前端**）、`docs`（**文档**）、其他 `internal/*`（按子目录细分）。 |
| FR-C2b | **语义标签** | 在规则之上输出标签集合，例如：`breaking`（若触及公开 `pkg/` 或删除导出）、`security`（`permissions`、`tools/exec` 等敏感路径）、`performance`、`feature`、`chore`。初版以 **启发式** 为主，允许误报并在报告中声明置信度。 |
| FR-C2c | **迁移提示** | 若 `migrations/*.sql` 有变更，报告 **必须** 醒目标注「需执行迁移 / 回顾 down.sql」。 |

### 4.3 子系统映射（FR-C3）

| ID | 需求 | 说明 |
|----|------|------|
| FR-C3a | **目录 → 子系统** | 维护一张可版本化的映射表（YAML/Go map），例如：`internal/agent` → Agent Loop / pipeline；`internal/spirit` → Spirit；`internal/store` → 存储层；`internal/mcp` → MCP；DreamWeaver 相关分散路径可合并为「DreamWeaver」桶。 |
| FR-C3b | **受影响面输出** | 对每个子系统给出：变更文件数、增删行粗略统计（可选）、一句话摘要（可由最近一次 commit message 聚合生成）。 |

### 4.4 产物与通知（FR-C4）

| ID | 需求 | 说明 |
|----|------|------|
| FR-C4a | **Markdown 报告** | 固定章节建议：`Summary`、`Commits`、`Files by subsystem`、`Risk flags`、`Migration / config notes`、`Suggested follow-ups`。 |
| FR-C4b | **机器可读 JSON（可选）** | 便于 CI 下游或 Agent 消费：文件列表、子系统、标签。 |
| FR-C4c | **通知钩子** | 可选：写入 artifact、评论 PR、Webhook；**不绑定** 单一厂商 IM。 |

### 4.5 运行方式（FR-C5）

| ID | 需求 | 说明 |
|----|------|------|
| FR-C5a | **CI 为一等公民** | 提供可在 Linux runner 上运行的命令（如 `goclaw changelog` 或独立 `cmd/` 小工具），环境变量配置 `BASE_REF`、`HEAD_REF`。 |
| FR-C5b | **本地可运行** | 开发者在合并前本地执行同一命令，与 CI 结果一致（除 token 增强部分）。 |

---

## 5. 非功能需求

| ID | 说明 |
|----|------|
| NFR-1 | **确定性**：相同输入 ref 与仓库状态下，报告内容可复现（不依赖非固定排序的远程列表，除非显式 `--use-remote`）。 |
| NFR-2 | **性能**：在万级文件仓库上对「一周内的 merge」统计应在分钟级内完成；大 patch 默认不内联全文。 |
| NFR-3 | **机密**：token、私有 URL 不得写入报告；日志脱敏。 |
| NFR-4 | **可维护**：子系统映射表与路径规则与代码同源维护，变更需可 review。 |

---

## 6. 架构备选（实现时不强制唯一）

| 方案 | 要点 | 适用 |
|------|------|------|
| **A. 独立子命令** | `goclaw` 或 `cmd/changelog` 调 `git` + 模板渲染 | 与仓库同发版，易本地调试 |
| **B. 纯 CI 脚本** | bash/PowerShell 调 `git` + `jq`/`go run` | 最快上线，难测 |
| **C. 外部知识图** | 若已用 GitNexus 等索引本仓库，可复用图查询做「影响面」；仍建议保留 **不依赖外部服务** 的基线路径 | 团队已有索引基础设施时 |

---

## 7. 分期建议与验收

| 阶段 | 内容 | 验收标准 |
|------|------|----------|
| **P0** | `git diff` + `git log` + 路径规则分类 + Markdown 输出 + `migrations/` 强提示 | CI 或本地一条命令生成报告；无托管 API 也能跑通 |
| **P1** | 子系统映射表 + JSON 输出 + PR CI 注释或 artifact | 映射表可扩展；下游可解析 JSON |
| **P2** | 托管 API  enrichment + 通知 Webhook | 配置 token 后 PR 元数据出现；Webhook 可配置关闭 |
| **P3** | 与 Agent/网关集成（会话注入「近期变更摘要」）或 LLM 辅助摘要 | 产品级定义清晰；成本控制与缓存策略明确 |

---

## 8. 与现有文档的关系

- Ideas 索引与总表：[README.md](./README.md) 需求 2。
- 简要进度表：[06-integration-status.md](./06-integration-status.md)「Ideas：需求 2」。
- 本文件为 **需求细化与架构边界** 的单一事实来源；实现后应在 `06` 中更新子项状态。

---

## 9. 实现现状（对照 FR）

| 子项 | 状态 | 备注 |
|------|------|------|
| 本仓库变更抓取 | 🔴 未实现 | 无统一命令或 CI job |
| 变更价值 / 风险评估 | 🔴 未实现 | 无自动标签与规则引擎 |
| 与模块映射 | 🔴 未实现 | 无映射表与报告章节 |
| 变更通知 / changelog | 🔴 未实现 | 无定时或 PR 集成 |

---

## 10. 附录：子系统映射初稿（实现时可挪到 `docs/` 或代码内）

以下为 **示例**，落地时以仓库实际边界为准：

| 路径前缀 | 子系统名称 |
|----------|------------|
| `cmd/` | CLI / 入口 |
| `internal/agent/` | Agent Loop、管线适配 |
| `internal/pipeline/` | Pipeline 阶段 |
| `internal/tools/` | 工具执行与沙箱 |
| `internal/config/` | 配置加载 |
| `internal/store/`, `migrations/` | 持久化与迁移 |
| `internal/spirit/` | Spirit |
| `internal/lifecycle/`, `internal/message/`, `internal/permissions/`, `internal/resume/` | DreamWeaver 运行时核心（Phase1） |
| `internal/compression/`, `internal/consolidation/`, `internal/memory/` | 连续性与记忆 |
| `internal/hooks/`, `pkg/sdk/`, `internal/plugins/` | 扩展与 SDK |
| `internal/marketplace/`, `internal/workshop/`, `internal/alchemy/` | 生态 |
| `ui/web/` | Web 前端 |

# 03 — Extension Protocol（扩展协议）

标准化 **Hook**、**SDK/Bridge**、**插件清单与注册表**，便于 IDE、脚本与第三方包接入。

## 1. Hook 生命周期 — `internal/hooks/`

| 文件 | 职责 |
|------|------|
| `events.go` | `Event`：session、pre/post think、pre/post tool、pre/post permission、on_compact、on_resume、on_error、lifecycle.change；`Payload` / `Result` |
| `registry.go` | `Registry`：`Register` / `Unregister` / `Fire`；同步优先、异步并行；`HandlerSpec`（webhook / command / internal） |
| `executor.go` | Webhook POST JSON；shell 通过 `sh -c` 且 stdin 为 payload JSON |

## 2. SDK / Bridge — `pkg/sdk/`

| 文件 | 职责 |
|------|------|
| `events.go` | 结构化事件类型（run、state、assistant delta、tool、permission、progress、session 等）及各类 `*Data` 载荷 |
| `bridge.go` | `Bridge`：`Command` / `CommandType`；`OnCommand`；`Subscribe` + `Emit` / `EmitAll`；与 `EventSink` 解耦 |
| `client.go` | WebSocket `Client`：`On`、`StartRun`、`AbortRun`、`Approve` / `Deny`（需与网关协议对齐后使用） |

**说明**：客户端假定网关推送 JSON 形态 `Event`；实际 RPC 方法名与 payload 以网关实现为准，接入时需对齐 [04-gateway-protocol.md](../04-gateway-protocol.md)。远程 **SSE**（`/v1/bridge/events`）与 **`EventFrame` 字段**见 [sdk-remote-bridge.md](./sdk-remote-bridge.md)。

## 3. 插件协议 — `internal/plugins/`

| 文件 | 职责 |
|------|------|
| `manifest.go` | `Manifest`：能力（tools / hooks / mcp_servers / skills）、`PluginPermissions`、`State` |
| `registry.go` | `Registry`：Install / Configure / Activate / Deactivate / Uninstall；依赖检查；`LoadFromDir` 扫描 `plugin.json` |

沙箱与 MCP 细节见 [03-tools-system.md](../03-tools-system.md)、[09-security.md](../09-security.md)。

---

## Ideas：融合需求对照（Claude Code → goclaw）

> 原载于 [DreamWeaver README](./README.md)「Ideas 需求索引」；**§1 的 D、H、I、K** 与本章「Extension Protocol」主题对应。

#### D. Hooks 系统

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **Shell command hooks** JSON stdin/stdout 协议 | `utils/hooks.ts` | `internal/hooks/executor.go` webhook/command | 🟢 已实现 | 支持 shell + webhook |
| **丰富事件类型** PreToolUse/PostToolUse/Stop/SubagentStart/SessionStart/Compact/Elicitation/TaskCompleted/TeammateIdle 等 20+ 种 | `utils/hooks.ts` event families | `internal/hooks/events.go` ~10 种事件 | 🟡 部分实现 | 缺少 Compact/Elicitation/Teammate/Subagent 系列事件 |
| **Hook 可阻断/修改** hook 返回后可 block 工具执行或注入 context | `toolHooks.ts` blocking behavior | `hooks.Fire` 异步发射，不阻断 | 🔴 未实现 | 需新增同步 hook 执行路径 |
| **UserPromptSubmit hooks** 用户输入预处理 hook | `qe1` UserPromptSubmit | 无 | 🔴 未实现 | 需在 input 编译阶段加 hook 点 |
| **Hook buffer + async rewake** | hook buffering in executor | `internal/hooks/executor.go` 有异步队列 | 🟢 已实现 | 基础实现到位 |

#### H. MCP 集成

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **多层配置合并** plugin<user<project<local<CLI<enterprise | `config.ts`, `.mcp.json` | `internal/mcp/` 单层配置 | 🟡 部分实现 | 缺少层级合并 |
| **多传输** stdio/SSE/HTTP/WebSocket/SDK | `client.ts` Ao6/XN6/XC | `internal/mcp/` stdio + SSE | 🟡 部分实现 | 缺 WebSocket/SDK 传输 |
| **MCP 指令注入** `mcp_instructions` section, cache break | `mcp_instructions` | 无 | 🔴 未实现 | 见 E 节 |
| **goclaw 作为 MCP server** 暴露自身工具 | `entrypoints/mcp.ts` | 无 | 🔴 未实现 | 可暴露 ListTools/CallTool |
| **Dedup by signature** stdio:cmd+args / url:normalized | `Fw6` dedup | 无 | 🔴 未实现 | 防止重复连接 |
| **OAuth + Elicitation** MCP server 认证 | `McpAuthError`, elicitation | 无 | 🔴 未实现 | 需 OAuth flow |

#### I. Plugin 系统

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **plugin.json manifest** commands/agents/hooks/settings | `Plugin.ts`, plugin spec | `internal/plugins/manifest.go` | 🟢 已实现 | Manifest 结构已有 |
| **Marketplace 安装/启用** scope user/project/local | marketplace install lifecycle | `internal/marketplace/installer.go` | 🟢 已实现 | 安装流程可用 |
| **Plugin 工具注册** | runtime registration | `pluginPlaceholderTool` (stub Execute) | 🟡 部分实现 | **placeholder** 返回错误，需实现真实 tool dispatch |
| **Plugin hooks 注册** | hooks from plugin dirs | 未接入 hook registry | 🔴 未实现 | 需扫描 plugin hooks/ 目录注册 |
| **Plugin MCP 合并** plugin 内 MCP 配置合并到全局 | MCP dedup in plugin load | 无 | 🔴 未实现 | |

#### K. Bridge / SDK / Remote

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **SDK event bridge** 事件流出 | `bridgeMessaging.ts`, `sdk/coreTypes.ts` | `pkg/sdk/bridge.go` Emit | 🟢 已实现 | |
| **SDK command handler** 外部控制 | `controlSchemas.ts` | `pkg/sdk/bridge.go` OnCommand (框架) | 🟡 部分实现 | 无具体 command handler 注册 |
| **Remote bridge** WS/SSE 双栈 | `remoteBridgeCore.ts`, env-based bridge | 无 | 🔴 未实现 | |
| **stream-json façade** 无 UI 纯事件输出 | headless `stream-json` mode | 无 | 🔴 未实现 | 适合 CI/SDK 场景 |

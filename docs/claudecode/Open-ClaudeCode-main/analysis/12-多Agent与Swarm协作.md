# 多 Agent 与 Swarm 协作

## 功能概述

支持子 Agent 创建（AgentTool）、团队协作（TeamCreate/TeamDelete）、队友间消息传递（SendMessage）、权限委托（Leader → Worker）、Coordinator 模式（多 Agent 编排）。Agent 可以在后台运行、使用独立 Worktree、或作为进程内任务执行。

---

## 从 main 函数入口的实现流程

### 1. Agent 工具

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/tools/AgentTool/AgentTool.tsx` | `AgentTool` | 子 Agent 工具定义（name、schema、call、prompt） |
| `src/tools/AgentTool/AgentTool.tsx` | `AgentTool.call()` | 启动子 Agent：后台/Worktree/远程/队友模式 |
| `src/tools/AgentTool/runAgent.ts` | `runAgent()` | 运行子 Agent 的核心逻辑 |
| `src/tools/AgentTool/forkSubagent.ts` | `forkSubagent()` | Fork 子 Agent 进程 |
| `src/tools/AgentTool/loadAgentsDir.ts` | `parseAgentsFromJson()` | 从 agents/ 目录加载 Agent 定义 |
| `src/tools/AgentTool/loadAgentsDir.ts` | `getActiveAgentsFromList()` | 获取活跃 Agent 列表 |
| `src/tools/AgentTool/loadAgentsDir.ts` | `getAgentDefinitionsWithOverrides()` | 获取带覆盖的 Agent 定义 |
| `src/tools/AgentTool/loadAgentsDir.ts` | `isBuiltInAgent()` / `isCustomAgent()` | Agent 类型判断 |
| `src/tools/AgentTool/agentColorManager.ts` | Agent 颜色管理 | 分配 Agent 显示颜色 |

### 2. 团队/Swarm 系统

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/tools/TeamCreateTool/TeamCreateTool.ts` | `TeamCreateTool` | 创建团队（注册 lead agent、持久化元数据） |
| `src/tools/TeamDeleteTool/TeamDeleteTool.ts` | `TeamDeleteTool` | 删除团队 |
| `src/tools/SendMessageTool/SendMessageTool.ts` | `SendMessageTool` | 发消息给队友/广播（支持 UDS/Bridge） |
| `src/utils/teammate.ts` | `getAgentId()` | 获取当前 Agent ID |
| `src/utils/teammate.ts` | `isTeammate()` | 判断是否为队友 |
| `src/utils/teammate.ts` | `isTeamLead()` | 判断是否为 Lead |
| `src/utils/teammate.ts` | `setDynamicTeamContext()` | 设置动态团队上下文 |
| `src/utils/teammate.ts` | `getTeamName()` / `getAgentName()` | 获取团队/Agent 名称 |
| `src/utils/agentSwarmsEnabled.ts` | `isAgentSwarmsEnabled()` | 判断 Swarm 功能是否启用 |

### 3. Swarm 内部通信

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/utils/swarm/permissionSync.ts` | `createPermissionRequest()` | 创建权限请求 |
| `src/utils/swarm/permissionSync.ts` | `sendSandboxPermissionRequestViaMailbox()` | 通过 Mailbox 发送权限请求 |
| `src/utils/swarm/permissionSync.ts` | `sendSandboxPermissionResponseViaMailbox()` | 发送权限响应 |
| `src/utils/swarm/permissionSync.ts` | `registerSandboxPermissionCallback()` | 注册权限回调 |
| `src/utils/swarm/permissionSync.ts` | `isSwarmWorker()` | 判断是否为 Swarm Worker |
| `src/utils/swarm/permissionSync.ts` | `generateSandboxRequestId()` | 生成沙箱请求 ID |
| `src/utils/swarm/teamHelpers.ts` | 团队辅助函数 | 团队文件操作、成员管理 |
| `src/utils/swarm/inProcessRunner.ts` | 进程内运行器 | 在当前进程内运行队友 Agent（共享内存） |
| `src/utils/swarm/reconnection.ts` | `computeInitialTeamContext()` | 计算团队上下文（启动/重连） |
| `src/utils/swarm/teammatePromptAddendum.ts` | 队友提示附加 | 为队友 Agent 添加额外系统提示 |

### 4. Coordinator 模式

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/coordinator/coordinatorMode.ts` | `isCoordinatorMode()` | 判断是否为 Coordinator 模式 |
| `src/coordinator/coordinatorMode.ts` | `getCoordinatorSystemPrompt()` | 获取 Coordinator 专属系统提示 |
| `src/coordinator/coordinatorMode.ts` | `getCoordinatorUserContext()` | 获取 Coordinator 用户上下文 |
| `src/coordinator/coordinatorMode.ts` | `matchSessionMode()` | 匹配会话模式 |

**Coordinator vs Swarm**：
| 维度 | Coordinator 模式 | Swarm 模式 |
|------|-----------------|-----------|
| 触发 | `CLAUDE_CODE_COORDINATOR_MODE` 环境变量 | `TeamCreateTool` 工具调用 |
| 架构 | 中心化：Coordinator + Workers | 对等：Lead + Teammates |
| 权限 | Workers 有受限工具集 | Teammates 共享权限（通过 Leader 代理） |
| 通信 | Coordinator 分配任务 | Mailbox 消息传递 |

### 5. 进程内队友任务

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/tasks/InProcessTeammateTask/InProcessTeammateTask.ts` | `injectUserMessageToTeammate()` | 向进程内队友注入消息 |
| `src/tasks/InProcessTeammateTask/InProcessTeammateTask.ts` | `getAllInProcessTeammateTasks()` | 获取所有进程内队友任务 |
| `src/tasks/LocalAgentTask/LocalAgentTask.ts` | `isLocalAgentTask()` | 判断是否为本地 Agent 任务 |
| `src/tasks/LocalAgentTask/LocalAgentTask.ts` | `queuePendingMessage()` | 排队待处理消息 |
| `src/tasks/LocalAgentTask/LocalAgentTask.ts` | `appendMessageToLocalAgent()` | 追加消息到本地 Agent |
| `src/utils/forkedAgent.ts` | `runForkedAgent()` | 运行 Fork 的 Agent（独立上下文） |

### 6. Leader 权限桥接

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/utils/swarm/leaderPermissionBridge.ts` | `registerLeaderToolUseConfirmQueue()` | Leader 注册工具确认队列 |
| `src/utils/swarm/leaderPermissionBridge.ts` | `registerLeaderSetToolPermissionContext()` | Leader 注册权限上下文设置 |
| `src/hooks/useSwarmPermissionPoller.ts` | `registerPermissionCallback()` | 注册 Swarm 权限回调 |

---

## 连接到 main.tsx 的流程

```
main.tsx
    │
    ├── 懒加载团队模块:
    │     ├── require('./utils/teammate.js')
    │     ├── require('./utils/swarm/teammatePromptAddendum.js')
    │     ├── require('./utils/swarm/backends/teammateModeSnapshot.js')
    │     └── require('./coordinator/coordinatorMode.js')
    │
    ├── isAgentSwarmsEnabled() → 判断是否启用 Swarm
    │
    ├── computeInitialTeamContext() → 恢复/初始化团队上下文
    │
    ├── 权限模式调整:
    │     └── isTeammate() → 强制 plan 模式
    │
    └── REPL.tsx
          ├── useSwarmInitialization() → Swarm 初始化 Hook
          ├── useSwarmPermissionPoller() → 权限轮询
          ├── useTeammateViewAutoExit() → 队友视图自动退出
          │
          └── 工具调用时:
                ├── AgentTool.call() → 启动子 Agent
                │     ├── 后台模式 → forkSubagent()
                │     ├── Worktree 模式 → 独立 Git Worktree
                │     ├── 进程内 → inProcessRunner
                │     └── 远程 → Bridge
                │
                ├── TeamCreateTool.call() → 创建团队
                ├── SendMessageTool.call() → 队友通信
                └── 权限代理:
                      └── Worker → createPermissionRequest()
                            → Leader Mailbox → UI 确认
                            → sendPermissionResponse()
```

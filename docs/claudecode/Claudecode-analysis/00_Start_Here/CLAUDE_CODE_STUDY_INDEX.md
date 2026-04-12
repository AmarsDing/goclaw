# Claude Code Study Index

## 1. 这份索引是干什么的

这份索引不是再把源码讲一遍，而是帮你解决一个更实际的问题：

**资料已经很多了，应该先看什么，后看什么，每篇到底解决什么问题？**

如果没有索引，最容易出现两种情况：

- 文档很多，但不知道从哪开始
- 每篇都看过一点，但脑子里没有结构

所以这份索引的目标很简单：

1. 给你一个推荐阅读顺序
2. 告诉你每篇最值得学的点
3. 帮你区分“主干必看”和“按需深入”

---

## 2. 先记住这张总图

现在这套资料已经不是“20 篇并列文章”，而是 6 条主线：

1. `Foundation`
2. `Runtime Continuity`
3. `Execution & Extensibility`
4. `Autonomy & Multi-Agent`
5. `Product Surface`
6. `Methodology`

你可以把它们理解成：

```text
Foundation
  -> Claude Code 的主骨架

Runtime Continuity
  -> 它为什么能长期连续工作

Execution & Extensibility
  -> 它怎么接工具、接外部系统、做治理

Autonomy & Multi-Agent
  -> 它怎么从单 agent 走向长期助手和多 agent

Product Surface
  -> 它怎么变成用户真正能用的终端产品

Methodology
  -> 最后能带走哪些设计原则
```

---

## 3. 如果你时间有限，只看最核心的 6 篇

### 3.1 系统全局图

- [CLAUDE_CODE_ANALYSIS.md](../01_Foundation/CLAUDE_CODE_ANALYSIS.md)

看这篇是为了回答：

- 项目整体怎么分层
- 哪些模块最关键
- 从哪里开始读源码不容易迷路

### 3.2 Agent 主骨架

- [CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)

看这篇是为了回答：

- Claude Code 的 agent loop 到底在哪里
- QueryEngine / query / ToolUseContext 各自干什么

### 3.3 Prompt Runtime

- [CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md](../01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)

看这篇是为了回答：

- Claude Code 的 prompt 为什么不是一段大字符串
- system prompt 为什么更像“运行时拼装系统”

### 3.4 Tool / Permission 主链路

- [CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)

看这篇是为了回答：

- tool 调用怎么进主链路
- permission 为什么不是简单弹窗

### 3.5 长会话能力

- [CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md](../02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
- [CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)

看这两篇是为了回答：

- Claude Code 为什么能连续工作
- memory / compact / resume 到底怎么配合

### 3.6 产品表现层

- [CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md](../05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md)

看这篇是为了回答：

- Claude Code 为什么像个终端应用，而不是普通 CLI
- AppState 为什么是中枢

如果上面这 6 组看懂了，你对 Claude Code 的理解就已经从“会用”进入“能拆它的系统设计”了。

---

## 4. 推荐完整阅读顺序

### 第一阶段：建立全局地图

1. [CLAUDE_LEARNING_ROADMAP.md](../00_Start_Here/CLAUDE_LEARNING_ROADMAP.md)
2. [CLAUDE_CODE_ANALYSIS.md](../01_Foundation/CLAUDE_CODE_ANALYSIS.md)

目标：

- 先知道整个系统长什么样
- 先知道后面每个主题在系统中的位置

### 第二阶段：理解 Claude Code 的主骨架

3. [CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
4. [CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md](../01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
5. [CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)

目标：

- 理解它怎么“想”
- 理解它怎么“决定做什么”
- 理解模型和工具怎么接起来

### 第三阶段：理解它为什么能长期工作

6. [CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md](../02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
7. [CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md)
8. [CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md)
9. [CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)
10. [CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md](../02_Runtime_Continuity/CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md)

目标：

- 理解 Claude Code 为什么不是“一轮一轮的 chat”
- 理解 memory、compact、resume、message model 是如何构成连续工作能力的

### 第四阶段：理解它怎么变成平台

11. [CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md](../03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md)
12. [CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md](../03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md)
13. [CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md](../03_Execution_Extensibility/CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md)
14. [CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md](../03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md)
15. [CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md](../03_Execution_Extensibility/CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md)

目标：

- 理解 Claude Code 怎么调工具、接系统、做治理
- 理解它为什么不是一个封闭单体

### 第五阶段：理解自治能力和产品化能力

16. [CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md](../04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)
17. [Kairos_OpenClaw_Heartbeat_Analysis.md](../04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md)
18. [CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md](../05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md)
19. [CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md](../06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md)

目标：

- 理解从单 agent 到长期助手的升级路线
- 理解产品表现层和方法论如何收束整套系统

---

## 5. 如果你是按目标学习，而不是按顺序学习

### 5.1 我想做一个 Claude Code 类似的 agent runtime

优先看：

- [CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
- [CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md](../01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
- [CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)
- [CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md](../04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)

### 5.2 我想学长期记忆、长会话、恢复

优先看：

- [CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md](../02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
- [CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md)
- [CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md)
- [CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)

### 5.3 我想学产品级权限、扩展与治理

优先看：

- [CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)
- [CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md](../03_Execution_Extensibility/CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md)
- [CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md](../03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md)
- [CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md](../03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md)

### 5.4 我想学终端 UI 和消息模型

优先看：

- [CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md](../05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md)
- [CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md](../02_Runtime_Continuity/CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md)
- [CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md](../03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md)

### 5.5 我想学长期助手和多 agent

优先看：

- [CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md](../04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)
- [Kairos_OpenClaw_Heartbeat_Analysis.md](../04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md)

---

## 6. 哪几篇是“最关键的少数”

如果你问我，整套资料里哪几篇信息密度最高、最值得反复看，我会选这 8 篇：

1. [CLAUDE_CODE_ANALYSIS.md](../01_Foundation/CLAUDE_CODE_ANALYSIS.md)
2. [CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
3. [CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md](../01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
4. [CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md](../01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)
5. [CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md](../02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
6. [CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md](../02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)
7. [CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md](../04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)
8. [CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md](../06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md)

如果这 8 篇吃透了，你已经不仅是“看懂 Claude Code”，而是开始形成自己的 agent 系统设计语言了。

---

## 7. 哪些更适合当专项深入

下面这些更偏专项、延伸或产品化细节，适合在主干建立以后按需深入：

- [Kairos_OpenClaw_Heartbeat_Analysis.md](../04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md)
- [CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md](../03_Execution_Extensibility/CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md)
- [CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md](../03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md)
- [CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md](../03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md)
- [CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md](../03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md)
- [CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md](../03_Execution_Extensibility/CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md)

---

## 8. 最后怎么使用这套资料

最推荐的方式不是“每篇都平均用力”，而是：

1. 先用 `README` 建立全局地图
2. 再用这份 `Study Index` 选择阅读路径
3. 优先吃透 `Foundation`
4. 再补 `Runtime Continuity`
5. 最后按你的目标去读 `Execution / Autonomy / Product`
6. 读完回到 `Design Principles`，把源码理解提炼成方法论

这样你最后得到的就不是“看过很多文档”，而是一个真正成体系的 Claude Code 设计理解。

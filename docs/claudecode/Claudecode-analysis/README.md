# Claudecode Analysis

这不是一组分散的源码笔记，而是一套围绕 **Claude Code 系统设计** 组织出来的学习资料。

它想解决的不是“把源码看完”，而是下面这些更关键的问题：

- Claude Code 到底是什么样的系统
- 它为什么不像普通聊天产品，而像一个长期运行的 agent runtime
- 它的能力是怎么从主骨架一路长成产品的
- 哪些设计最值得借鉴到你自己的 agent / IDE / workflow 系统里

如果你准备把这个目录放到 GitHub，这个 `README` 应该承担两件事：

- 帮第一次进来的人迅速建立全局地图
- 告诉读者应该按什么顺序读，才不会越看越散

---

## Who This Repo Is For

这套资料更适合下面这几类读者：

- 想系统理解 `Claude Code`，而不是只会用它的人
- 想研究 `agent runtime`、`tool system`、`memory`、`resume` 这些设计的人
- 想从 Claude Code 提炼出自己系统设计方法的人
- 想做 agent、IDE、workflow 产品，但不想停留在 demo 层的人

如果你只是想快速知道“Claude Code 大概做了什么”，先看：

- [`00_Start_Here/CLAUDE_CODE_STUDY_INDEX.md`](./00_Start_Here/CLAUDE_CODE_STUDY_INDEX.md)
- [`01_Foundation/CLAUDE_CODE_ANALYSIS.md`](./01_Foundation/CLAUDE_CODE_ANALYSIS.md)

---

## How To Use This Repo

推荐这样使用这套资料：

1. 先读这份 `README`，建立全局地图
2. 再读 [`00_Start_Here/CLAUDE_CODE_STUDY_INDEX.md`](./00_Start_Here/CLAUDE_CODE_STUDY_INDEX.md)，选择阅读路径
3. 优先吃透 `01_Foundation`
4. 再根据兴趣进入 `02_Runtime_Continuity` 或 `03_Execution_Extensibility`
5. 最后回到 [`06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md`](./06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md)，把源码理解收束成方法论

如果你想最快进入状态，建议从这 4 篇开始：

- [`01_Foundation/CLAUDE_CODE_ANALYSIS.md`](./01_Foundation/CLAUDE_CODE_ANALYSIS.md)
- [`01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
- [`01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md`](./01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
- [`01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)

---

## Link Policy

这个仓库里的导航链接统一使用 **相对路径 Markdown 链接**，不使用本机绝对路径。

这意味着：

- 在本地目录里点链接可以跳
- 上传到 GitHub 之后，仓库内链接依然可以正常跳转

---

## 1. 先用一句话理解这套资料

Claude Code 可以粗略理解成这样一条成长路径：

```text
Agent Runtime
  -> Prompt / Tool / Permission
  -> Memory / Compact / Resume
  -> SDK / MCP / Hooks / Bridge
  -> Multi-Agent / Long-running Assistant
  -> UI / AppState / Message Model
  -> Design Principles
```

所以这套资料的结构也应该顺着这条路径来，而不是把所有文章并排放着。

---

## 2. 这套资料应该怎么读

最好的方式不是“从上往下把所有文件夹点一遍”，而是按三层来理解：

### 第一层：先搭主骨架

你要先知道 Claude Code 的核心到底是什么：

- 它是一个 `agent runtime`
- 它有清晰的 prompt、tool、permission 主链路
- 它不是靠某一段神奇 prompt 撑起来的

这一层对应：

- [`01_Foundation/CLAUDE_CODE_ANALYSIS.md`](./01_Foundation/CLAUDE_CODE_ANALYSIS.md)
- [`01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
- [`01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md`](./01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
- [`01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)

### 第二层：再看它为什么能长期工作

你要理解 Claude Code 为什么不是“一轮聊天结束就散了”的系统：

- 它怎么管 memory
- 它怎么做 compact
- 它怎么恢复 session
- 它为什么能把现场接回来

这一层对应：

- [`02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md`](./02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
- [`02_Runtime_Continuity/CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md)
- [`02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md)
- [`02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)
- [`02_Runtime_Continuity/CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md`](./02_Runtime_Continuity/CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md)

### 第三层：最后看它为什么像产品而不是 demo

这一层看的是：

- 它怎么接外部系统
- 它怎么治理生命周期
- 它怎么支持多 agent / 长期助手
- 它怎么把 runtime 做成一个真正可用的终端产品

这一层对应：

- [`03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md`](./03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md)
- [`03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md)
- [`03_Execution_Extensibility/CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md)
- [`03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md)
- [`03_Execution_Extensibility/CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md)
- [`04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md`](./04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)
- [`04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md`](./04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md)
- [`05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md`](./05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md)

最后再回到：

- [`06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md`](./06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md)

把前面的理解收束成方法论。

---

## 3. 整体知识网络

不要把这里想成“20 篇并列文章”，更应该把它看成一张层层展开的知识网络：

```text
┌──────────────────────────────────────────────┐
│ Start Here                                   │
│ 学习入口、路线、索引                         │
│                                              │
│ 00_Start_Here                                │
└──────────────────────────────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────┐
│ Foundation                                   │
│ Claude Code 的主骨架                         │
│                                              │
│ 项目总览 / Harness / Prompt / Tool Permission │
└──────────────────────────────────────────────┘
          │                    │
          │                    │
          ▼                    ▼
┌──────────────────────┐  ┌─────────────────────────┐
│ Runtime Continuity   │  │ Execution & Extensibility│
│ 长会话与连续工作能力  │  │ 工具执行、外部接入、治理 │
│                      │  │                         │
│ Memory               │  │ Tool Execution         │
│ Compaction           │  │ MCP / SDK / Bridge     │
│ Restore / Resume     │  │ Hooks / Permission UI  │
│ Message Modeling     │  │                         │
└──────────────────────┘  └─────────────────────────┘
          │                    │
          └──────────┬─────────┘
                     │
                     ▼
┌──────────────────────────────────────────────┐
│ Autonomy & Product Surface                    │
│ 多 agent、长期助手、UI、终端产品化能力        │
│                                              │
│ Subagent / Kairos / UI / AppState            │
└──────────────────────────────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────┐
│ Methodology                                  │
│ 把源码理解收束成可以借鉴的设计原则            │
│                                              │
│ 10 Key Design Principles                     │
└──────────────────────────────────────────────┘
```

如果你想用最短的一句话记住它：

```text
先看骨架
  -> 再看它怎么长期工作
  -> 再看它怎么扩展、协作、产品化
  -> 最后提炼方法论
```

---

## 4. 目录结构为什么这样重组

之前那种 `01` 到 `20` 每篇一个文件夹的方式，有个明显问题：

- 编号很整齐
- 但读者很难一眼看出哪些文章是同一层的问题
- 最终会变成“文章列表”，而不是“知识结构”

所以现在把目录收成 7 个更有语义的主题层：

### `00_Start_Here`

作用：

- 给第一次来的读者一个入口
- 解决“先看什么”和“怎么建立全局图谱”

包含：

- `CLAUDE_LEARNING_ROADMAP.md`
- `CLAUDE_CODE_STUDY_INDEX.md`

### `01_Foundation`

作用：

- Claude Code 的主骨架
- 也是整套资料里最该先读的一层

包含：

- `CLAUDE_CODE_ANALYSIS.md`
- `CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md`
- `CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md`
- `CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md`

### `02_Runtime_Continuity`

作用：

- 解释 Claude Code 为什么能长期连续工作

包含：

- `CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md`
- `CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md`
- `CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md`
- `CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md`
- `CLAUDE_TOOL_RESULT_MESSAGE_MODELING_GUIDE.md`

### `03_Execution_Extensibility`

作用：

- 解释 Claude Code 怎么把工具、扩展和治理能力接进 runtime

包含：

- `CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md`
- `CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md`
- `CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md`
- `CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md`
- `CLAUDE_PERMISSION_UI_APPROVAL_INTERACTION_FLOW_GUIDE.md`

### `04_Autonomy_Multi_Agent`

作用：

- 解释 Claude Code 如何从单 agent 走向多 agent 和长期助手模式

包含：

- `CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md`
- `Kairos_OpenClaw_Heartbeat_Analysis.md`

### `05_Product_Surface`

作用：

- 解释用户最终看到的产品层是怎么成立的

包含：

- `CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md`

### `06_Methodology`

作用：

- 把前面的源码理解提炼成方法论

包含：

- `CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md`

---

## 5. 推荐阅读路径

### 路径 A：我只想先抓主干

按这个顺序：

1. [`00_Start_Here/CLAUDE_CODE_STUDY_INDEX.md`](./00_Start_Here/CLAUDE_CODE_STUDY_INDEX.md)
2. [`01_Foundation/CLAUDE_CODE_ANALYSIS.md`](./01_Foundation/CLAUDE_CODE_ANALYSIS.md)
3. [`01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
4. [`01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md`](./01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
5. [`01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)

### 路径 B：我想研究长期运行 agent

按这个顺序：

1. [`02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md`](./02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
2. [`02_Runtime_Continuity/CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_COMPACTION_CONTEXT_TRIMMING_DEEP_DIVE.md)
3. [`02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_PERSISTENCE_DEEP_DIVE.md)
4. [`02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)

### 路径 C：我想研究产品化与外部扩展

按这个顺序：

1. [`03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md`](./03_Execution_Extensibility/CLAUDE_TOOL_EXECUTION_STREAMING_DEEP_DIVE.md)
2. [`03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_MCP_EXTERNAL_TOOLING_ARCHITECTURE_GUIDE.md)
3. [`03_Execution_Extensibility/CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_SDK_BRIDGE_REMOTE_CONTROL_GUIDE.md)
4. [`03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md`](./03_Execution_Extensibility/CLAUDE_HOOKS_EVENT_BUS_LIFECYCLE_ARCHITECTURE_GUIDE.md)
5. [`05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md`](./05_Product_Surface/CLAUDE_UI_STATE_APPSTATE_REACT_INK_RENDERING_GUIDE.md)

### 路径 D：我想研究自动助手和多 agent

按这个顺序：

1. [`04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md`](./04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)
2. [`04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md`](./04_Autonomy_Multi_Agent/Kairos_OpenClaw_Heartbeat_Analysis.md)

---

## 6. 如果你只想看最关键的少数

如果时间有限，我建议优先读下面这 8 篇：

1. [`01_Foundation/CLAUDE_CODE_ANALYSIS.md`](./01_Foundation/CLAUDE_CODE_ANALYSIS.md)
2. [`01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_HARNESS_AGENT_ARCHITECTURE_GUIDE.md)
3. [`01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md`](./01_Foundation/CLAUDE_PROMPT_SYSTEM_DESIGN_GUIDE.md)
4. [`01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md`](./01_Foundation/CLAUDE_TOOL_PERMISSION_ARCHITECTURE_GUIDE.md)
5. [`02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md`](./02_Runtime_Continuity/CLAUDE_CONTEXT_COMPRESSION_MEMORY_GUIDE.md)
6. [`02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md`](./02_Runtime_Continuity/CLAUDE_SESSION_RESTORE_RESUME_FLOW_DEEP_DIVE.md)
7. [`04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md`](./04_Autonomy_Multi_Agent/CLAUDE_SUBAGENT_MULTI_AGENT_GUIDE.md)
8. [`06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md`](./06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md)

---

## 7. 这套资料最后要带走什么

如果你把这里的文档都看完，真正应该带走的不是“我知道了好多 Claude Code 文件名”，而是下面这些判断能力：

- 你能看出一个 agent 系统的主骨架在哪里
- 你能分辨 prompt、tool、permission、memory、resume 各自在系统里扮演什么角色
- 你能区分“能跑的 demo”和“能长期工作的产品”
- 你能把 Claude Code 的设计抽象成自己的系统方法论

这也是为什么这里最后会落到 [`06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md`](./06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md)。

因为真正有价值的，不是看过这套源码，而是你以后也能用类似的方法设计自己的系统。

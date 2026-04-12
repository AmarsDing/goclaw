# 交互式 REPL 界面

## 功能概述

本项目的交互式终端界面基于 **React + 深度定制版 Ink** 构建，负责在 TTY 中完成「读入—展示—状态反馈」的完整闭环。主要能力包括：

- **输入**：多行提示框、斜杠命令与技能、图片粘贴、引用与历史、**Vim 模式**（`useVimInput`）、以及特性开关下的**语音输入**（`VOICE_MODE` 与 `VoiceProvider` / `useVoiceState` 等）。
- **输出**：消息列表、终端内 Markdown、代码高亮、大会话下的**虚拟滚动**，以及流式增量更新（与 `handleMessageFromStream` 协同）。
- **状态与权限**：底部状态栏（模型、消耗、Token 等）、加载 Spinner、**工具权限**通过 `useCanUseTool` 与 `ToolPermissionContext` 在 REPL 与工具执行链之间桥接（含队列与确认流）。
- **编排中心**：主屏幕集中在 `src/screens/REPL.tsx`（大型单文件，当前仓库约 **4700+ 行**），组合 `PromptInput`、`Messages`、`StatusLine` 等子组件，并持有 `messages`、工具列表、`onQuery` 等核心状态与回调。

下文从 CLI 入口到 Ink 渲染，梳理启动链、输入/展示/状态栏、定制 Ink 与全局状态管理，并附**关键文件索引表**与**组件层次示意图**。

---

## 从 main 函数入口的实现流程

整体路径与 `analysis/核心调用链分析.md` 中「阶段 1」一致，此处聚焦 **REPL 与 UI 子系统**。

```
cli.tsx: main()
  → main.tsx: main()  // 参数解析、认证、工具/命令注册等
  → launchRepl(root, appProps, replProps, renderAndRun)  // replLauncher.tsx
  → renderAndRun(root, <App><REPL /></App>)
  → Ink React 协调器驱动终端重绘与键盘事件
```

### 1. REPL 启动

| 步骤 | 文件 | 要点 |
|------|------|------|
| 动态挂载 | `src/replLauncher.tsx` | `launchRepl()` 使用动态 `import()` 拉取 `App` 与 `REPL`，避免启动时整树打包过重；最终调用传入的 `renderAndRun(root, element)` 进入 Ink。 |
| 全局包裹 | `src/components/App.tsx` | 自外向内：`FpsMetricsProvider` → `StatsProvider` → `AppStateProvider`（`onChangeAppState` 接 `onChangeAppState` 持久化/副作用）。为交互会话提供 FPS、统计与 App 级状态。 |
| 主屏 | `src/screens/REPL.tsx` | `REPL` 接收 `initialMessages`、`commands`、`initialTools`、`mcpClients`、`systemPrompt` 等 props；维护消息数组、`isLoading`、`abortController`、`onQuery` / `onQueryImpl` / `onQueryEvent`；向 `PromptInput` 传入 `handlePromptSubmit` 相关参数与 `onQuery`。 |

`main.tsx` 在不同子命令/场景下构造 `replProps`（例如远程会话、resume、初始消息等），但 **UI 树根始终是 `App` 包住 `REPL`**。

### 2. 输入系统

| 模块 | 路径 | 职责 |
|------|------|------|
| 终端输入框 | `src/components/PromptInput/PromptInput.tsx` | 组合 footer、模式切换、提交逻辑；与 REPL 的 `messages`、`commands`、`toolPermissionContext` 等联动。 |
| 底层文本 | `src/hooks/useTextInput.ts` | 字符插入、选区、与终端宽度相关的编辑行为。 |
| Vim | `src/hooks/useVimInput.ts` | Normal/Insert 等键位映射，与 `useTextInput` 组合。 |
| 模式 | `src/components/PromptInput/inputModes.ts` | **prompt / bash / plan** 等输入模式及切换语义。 |
| 历史上翻 | `src/hooks/useArrowKeyHistory.tsx` | 箭头键在历史记录间导航。 |
| 语音（可选） | `src/context/voice.js`、`src/hooks/useVoiceEnabled.js`、`PromptInputFooterLeftSide.tsx` 等 | `feature('VOICE_MODE')` 时启用 `VoiceProvider`（在 `AppState.tsx` 内条件注入）、Push-to-talk 提示与状态指示。 |

用户按回车提交时，REPL 侧通常进入 `handlePromptSubmit`（见文档 04），其内部再调用 `processUserInput` / `executeUserInput` 决定是否发起 `onQuery`。

### 3. 消息显示

| 模块 | 路径 | 职责 |
|------|------|------|
| 列表 | `src/components/Messages.tsx` | 消息列表容器、与全屏/压缩边界、分组、滚动等逻辑协作。 |
| 单条 | `src/components/Message.tsx` | 按消息类型分支渲染（用户/助手/系统/进度等）。 |
| 行布局 | `src/components/MessageRow.tsx` | 单行排版、与选择器/动作入口等。 |
| Markdown | `src/components/Markdown.tsx` | 终端友好的 Markdown 子集渲染。 |
| 虚拟列表 | `src/components/VirtualMessageList.tsx` | 大会话下只挂载可视区域附近节点，降低 Ink 协调成本。 |
| 代码高亮 | `src/components/HighlightedCode.tsx` | 代码块语法高亮（与 Markdown 输出衔接）。 |

流式阶段 REPL 通过 `onQueryEvent` 调用 `handleMessageFromStream`（`src/utils/messages.ts`），在写入 `messages` 状态的同时更新 Spinner 模式、流式工具 JSON、thinking 展示等（详见文档 04）。

### 4. 状态栏与加载反馈

| 模块 | 路径 | 职责 |
|------|------|------|
| 状态栏 | `src/components/StatusLine.tsx` | 底部展示模型名、费用/Token 等摘要信息（具体字段随产品配置变化）。 |
| Spinner | `src/components/Spinner.tsx` | 请求中动画与动词（如 requesting / responding / tool-input），与 `setStreamMode` 联动。 |

### 5. Ink 终端 UI 框架（`src/ink/`）

本仓库内 **`src/ink/` 约 96 个文件**，是在开源 Ink 基础上的**深度定制**：布局（含 Yoga）、事件（键盘/焦点/终端尺寸）、ANSI 输出、选择与滚动等均在仓库内闭环，便于与 Claude Code 的交互需求对齐。

| 文件 | 作用 |
|------|------|
| `src/ink/ink.tsx` | Ink 运行时入口、与 React 根的配合。 |
| `src/ink/renderer.ts` | React 树 → 终端绘制数据结构的桥梁。 |
| `src/ink/render-node-to-output.ts` | 单个节点到输出单元（行/样式）的映射。 |
| `src/ink/reconciler.ts` | 面向终端的 React reconciler（替代 DOM）。 |
| `src/ink/output.ts` | 缓冲、刷新、与 stdout 交互。 |
| `src/ink/render-to-screen.ts` | 将布局结果写入屏幕。 |
| 其他 | `components/Box.tsx`、`Text.tsx`、`ScrollBox.tsx`、`use-input.ts`、`parse-keypress.ts`、`termio/*` 等：盒模型、文本换行、滚动区域、按键解析、CSI/OSC 解析等。 |

应用层通过 `src/ink.js`（及 `Root` 类型）与上述实现对接；`renderAndRun` 在 `main.tsx` 中绑定具体终端实例。

### 6. 状态管理

| 模块 | 路径 | 职责 |
|------|------|------|
| Context + Store | `src/state/AppState.tsx` | `AppStateProvider`：基于 `createStore`（`src/state/store.ts`）持有可变的 `AppState`；条件挂载 `VoiceProvider`；导出类型时常 re-export `AppStateStore.ts`。 |
| 状态形状 | `src/state/AppStateStore.ts` | `AppState` 全字段定义（工具权限、MCP、模式、effort、推测状态等）。 |
| 微型 store | `src/state/store.ts` | 订阅/通知模型（Zustand 风格 API 的轻量实现）。 |
| 选择器 | `src/state/selectors.ts` | 从 store 中派生常用切片，减少组件重复订阅。 |
| 变更钩子 | `src/state/onChangeAppState.ts` | `App` 传入的 `onChangeAppState`，用于跨切持久化或遥测（具体逻辑见该文件）。 |

REPL 中大量 `setAppState` / `store.setState` 调用与 **`toolPermissionContext`**、`mcp.clients` 同步相关；`getToolUseContext` 从当前 store 读取最新工具与 MCP，避免闭包陈旧。

---

## 关键文件索引

| 分类 | 路径 | 说明 |
|------|------|------|
| 启动 | `src/replLauncher.tsx` | `launchRepl` |
| 根布局 | `src/components/App.tsx` | 三层 Provider |
| 主屏 | `src/screens/REPL.tsx` | 消息、查询、权限桥、PromptInput 参数 |
| 输入 | `src/components/PromptInput/PromptInput.tsx` | 主输入 UI |
| 输入 | `src/components/PromptInput/inputModes.ts` | prompt/bash/plan |
| 输入 | `src/hooks/useTextInput.ts`、`useVimInput.ts`、`useArrowKeyHistory.tsx` | 编辑与 Vim、历史 |
| 提交链路 | `src/utils/handlePromptSubmit.ts` | 提交入口（与文档 04 衔接） |
| 消息 | `src/components/Messages.tsx`、`Message.tsx`、`MessageRow.tsx` | 列表与单条 |
| 消息 | `src/components/Markdown.tsx`、`HighlightedCode.tsx` | 富文本与代码 |
| 消息 | `src/components/VirtualMessageList.tsx` | 虚拟滚动 |
| 流式 UI | `src/utils/messages.ts` | `handleMessageFromStream` |
| 状态栏 | `src/components/StatusLine.tsx`、`Spinner.tsx` | 底部栏与加载 |
| Ink | `src/ink/ink.tsx`、`renderer.ts`、`reconciler.ts`、`render-node-to-output.ts`、`output.ts` | 核心渲染链 |
| 状态 | `src/state/AppState.tsx`、`AppStateStore.ts`、`store.ts`、`selectors.ts` | 全局状态 |

---

## UI 组件层次图（文本示意）

```
                    ┌─────────────────────────────────────┐
                    │  App (FpsMetricsProvider)           │
                    │    └─ StatsProvider                 │
                    │         └─ AppStateProvider         │
                    │              └─ VoiceProvider?      │
                    └──────────────────┬──────────────────┘
                                       │
                    ┌──────────────────▼──────────────────┐
                    │  REPL                                 │
                    │  ├─ 对话框层（消息选择 / 任务 / 等）    │
                    │  ├─ Messages (+ VirtualMessageList)   │
                    │  │    └─ Message / MessageRow         │
                    │  │         └─ Markdown / HighlightedCode │
                    │  ├─ PromptInput (+ Footer / Modes)    │
                    │  └─ StatusLine / Spinner              │
                    └─────────────────────────────────────┘
```

数据流简述：

1. **下行**：`query()` 异步生成器产出事件 → REPL 的 `onQueryEvent` → `handleMessageFromStream` → `setMessages` / `setStreamMode` / 流式工具状态。
2. **上行**：键盘 → `PromptInput` → `handlePromptSubmit` → `processUserInput` → 可能 `onQuery` → `query()`（见文档 04）。

---

## 小结

交互式 REPL 并非单一组件，而是 **定制 Ink + REPL 编排层 + 输入/消息/状态栏模块 + AppState 全局存储** 的组合体。理解 UI 时建议：**从 `replLauncher.tsx` 和 `App.tsx` 看挂载树，从 `REPL.tsx` 看数据与回调边界，从 `src/ink/` 看终端如何被当作「画布」渲染。**

# Claude Code UI State / AppState / React Ink Rendering Guide

## 1. 这篇文档讲什么

这一篇我们专门讲 Claude Code 的 UI runtime，也就是：

- 它的状态是怎么组织的
- React 在终端里是怎么工作的
- AppState 为什么这么关键
- `print.ts`、`REPL.tsx`、`Messages.tsx`、`PromptInput.tsx` 为什么要这样拆
- Claude Code 为什么能在一个终端里撑起这么复杂的交互而不完全失控

很多人看到 Claude Code，会先被 agent loop、tool use、subagent 吸引。

但如果你真想学习“如何把一个 AI 编码助手做成产品”，你迟早要啃这一层。

因为真正把体验拉开差距的，不只是模型会不会回答问题，而是：

- UI 会不会乱跳
- 状态会不会打架
- 一边 streaming 一边输入会不会崩
- 后台任务、通知、权限弹窗、消息列表能不能同时活着

Claude Code 在这些地方做了很多很成熟的设计。

---

## 2. 一句话概括这套 UI 架构

最简洁的总结是：

**Claude Code 用一个外部 store 形式的 AppState 作为中枢，把 React + Ink 变成了一个可持续运行的终端应用，而不是一次性打印脚本。**

这里有几个关键词：

- 外部 store
- 选择性订阅
- AppState 中枢
- 非 React 代码也能读写状态
- React 负责渲染，runtime 负责推进流程

如果通俗一点说：

**Claude Code 不是“React 套在 CLI 外面”，而是“用 React/Ink 做了一个终端操作系统式的界面层”。**

---

## 3. 先抓主调用链

这条链是理解 UI 架构最关键的入口：

```text
print.ts
  -> launchRepl()
  -> components/App.tsx
  -> state/AppState.tsx (AppStateProvider)
  -> screens/REPL.tsx
  -> Messages.tsx / PromptInput.tsx / dialogs / status / tasks
  -> Ink render loop
```

对应关键文件：

- [cli/print.ts](../src/cli/print.ts)
- [replLauncher.tsx](../src/replLauncher.tsx)
- [components/App.tsx](../src/components/App.tsx)
- [state/AppState.tsx](../src/state/AppState.tsx)
- [state/AppStateStore.ts](../src/state/AppStateStore.ts)
- [state/store.ts](../src/state/store.ts)
- [screens/REPL.tsx](../src/screens/REPL.tsx)
- [components/Messages.tsx](../src/components/Messages.tsx)
- [components/VirtualMessageList.tsx](../src/components/VirtualMessageList.tsx)
- [components/PromptInput/PromptInput.tsx](../src/components/PromptInput/PromptInput.tsx)

---

## 4. 先解决一个基础问题：Ink 到底是什么

Ink 可以理解成：

**“让 React 渲染到终端”的框架。**

平时 React 是渲染到 DOM。

Ink 是渲染到 terminal screen buffer。

所以 Claude Code 的很多组件看起来像网页 React 组件：

- `<Box>`
- `<Text>`
- hooks
- context
- state

但它们最后不是画到浏览器，而是画到终端字符界面。

通俗解释：

如果浏览器里的 React 是在“画网页”，那 Ink 里的 React 就是在“画字符屏幕”。

所以它面对的问题会跟 Web 不太一样：

- 宽度很敏感
- 换行很敏感
- 重绘成本很敏感
- 滚动列表很敏感
- 一些错误会直接变成屏幕闪烁或黑屏

Claude Code 在很多地方的复杂设计，都是为了应对这些终端渲染问题。

---

## 5. print.ts 为什么这么重要

很多人第一次看这个项目，会误以为 `print.ts` 只是“输出用的文件”。

其实不是。

[print.ts](../src/cli/print.ts) 更像：

**主 runtime 和 UI runtime 的总装配厂。**

它负责的事情很多：

- 初始化 messages
- 管理 `getAppState` / `setAppState`
- 装配 tools / commands / mcp / sdk
- 接会话恢复
- 接 hook 事件
- 接 structured output / remote / bridge
- 最后把 REPL 启起来

所以你可以把它理解成：

- 不是一个组件
- 不是一个纯渲染文件
- 而是“把系统组装起来并交给 UI 层运行”的控制中心

通俗解释：

如果 `REPL.tsx` 是驾驶舱，那 `print.ts` 更像整车装配线。

---

## 6. launchRepl：为什么还要单独包一层

[replLauncher.tsx](../src/replLauncher.tsx) 很短，但很有代表性。

它做的事情是：

- 动态 import `App`
- 动态 import `REPL`
- 调用 `renderAndRun`

这说明 Claude Code 明确区分：

- 启动 orchestration
- 真正的 UI 组件树

这个分层的好处：

- 更容易做不同入口模式
- 更容易在非交互环境复用部分逻辑
- 有助于把“如何启动”与“启动后如何渲染”拆开

通俗解释：

就像一个剧院里：

- `print.ts` 是舞台总控
- `launchRepl()` 是“拉开帷幕”
- `REPL.tsx` 才是演员正式上场

---

## 7. App.tsx：为什么 Claude Code 还要再包一层 App

[components/App.tsx](../src/components/App.tsx) 非常简洁，但它暴露出一个重要思路：

- `FpsMetricsProvider`
- `StatsProvider`
- `AppStateProvider`

Claude Code 在这里把最关键的全局上下文先兜住。

这说明它不是让 `REPL.tsx` 自己边跑边长上下文，而是：

**先铺基础设施，再渲染主界面。**

这很像 Web 应用常见的顶层 Provider 树，但这里在 CLI 里同样成立。

通俗解释：

你可以把 `App.tsx` 看成“总开关箱”：

- 状态电路接上
- 性能统计接上
- fps 指标接上
- 然后整个系统再通电

---

## 8. AppState 为什么是 UI 架构的中枢

这一层最关键的文件是：

- [state/AppStateStore.ts](../src/state/AppStateStore.ts)
- [state/AppState.tsx](../src/state/AppState.tsx)
- [state/store.ts](../src/state/store.ts)

如果你只记一句话：

**Claude Code 用 AppState 把“整个终端应用当前长什么样、处于什么模式、有哪些后台对象在跑”集中起来了。**

看 `AppStateStore.ts` 里的字段，你会发现它远不只是几个 UI flag。

它包含：

- settings
- model
- statusLineText
- expandedView
- toolPermissionContext
- kairosEnabled
- remote / bridge 状态
- tasks
- agentNameRegistry
- mcp clients/tools/resources
- plugins
- fileHistory
- todos
- notifications
- elicitation queue
- sessionHooks
- tungsten / bagel / computer use 等特性状态

这说明 AppState 不是“某个页面的状态”。

它是：

**整个 Claude Code 终端应用的运行态快照。**

---

## 9. 最通俗地理解 AppState

如果你把 Claude Code 想成一个办公室，AppState 就像办公室前台那块超级大白板，上面同时记录：

- 今天谁值班
- 当前在哪个房间开会
- 哪些任务在跑
- 哪些权限待审批
- 哪个远程会话连着
- 哪些插件启用了
- 目前屏幕该显示什么

每个组件都不需要自己发明一套“世界观”。

它们只需要从这块大白板上读自己关心的部分。

这就是 AppState 的价值。

---

## 10. 为什么 Claude Code 不直接用 React useState 铺满全局

看 [store.ts](../src/state/store.ts)，你会发现它是个很朴素的外部 store：

- `getState`
- `setState`
- `subscribe`

这很像极简版 Zustand / Redux store 内核。

为什么 Claude Code 要这么做，而不是全靠 React `useState`/`useReducer`？

核心原因有三个。

### 10.1 非 React 代码也要读写状态

比如：

- `print.ts`
- tool 执行流程
- structured IO
- bridge / sdk / remote

这些很多都不是普通 React 组件。

它们需要：

- 同步读当前 state
- 在某个流程里更新 state

如果全靠 React 组件内状态，会很难把 runtime 和 UI 打通。

### 10.2 需要选择性订阅，减少重渲染

在 [AppState.tsx](../src/state/AppState.tsx) 里可以看到：

- `useSyncExternalStore`
- `useAppState(selector)`
- `useSetAppState()`
- `useAppStateStore()`

这说明组件不是整个订阅大状态，而是按 selector 订阅切片。

这在终端里尤其重要，因为重绘很贵。

### 10.3 需要一个统一的 onChange choke point

`createStore(initialState, onChange)` 允许状态更新统一走 `onChangeAppState.ts` 做副作用处理。

这比让每个调用点自己广播外部同步要稳很多。

---

## 11. AppState.tsx 的设计为什么很漂亮

[AppState.tsx](../src/state/AppState.tsx) 是一个非常值得学习的文件。

它做了几件很成熟的事。

### 11.1 Provider 不允许嵌套

它会检测 `HasAppStateContext`，防止你在另一个 `AppStateProvider` 里面再套一个。

这看起来像小事，实际上很重要。

因为双层 store 一旦出现，UI 很容易出现：

- 某些组件读的是旧 store
- 某些组件写的是新 store
- 状态诡异不同步

Claude Code 直接从设计上防掉了。

### 11.2 store 只创建一次

它用 `useState(() => createStore(...))` 来保证 store 实例稳定。

这意味着：

- 不会因为 rerender 重建 store
- 订阅关系稳定
- 非 React 引用也更安全

### 11.3 selector 模式订阅

`useAppState(selector)` 的注释写得很明白：

- 只订阅你关心的字段
- 不要返回新对象
- 否则每次都被当成变化

这就是典型的高性能外部 store 用法。

### 11.4 提供“只写不读”的 hook

`useSetAppState()` 返回稳定的 setter，不会导致额外 rerender。

这对很多交互组件非常重要。

通俗解释：

不是所有组件都要“盯着大白板看”。

有些组件只是偶尔要去“往白板上改一笔”。

那就给它一个笔，不让它每次都跟着全局变化而分心。

---

## 12. onChangeAppState：状态变化的副作用总闸门

[onChangeAppState.ts](../src/state/onChangeAppState.ts) 很值得反复读。

它的意义是：

**任何 AppState 的变化，只要触发了重要副作用，都尽量集中在这里处理。**

最典型的例子是 permission mode 的同步。

注释写得很坦率：

- 以前只有部分路径会同步 mode 到外部系统
- 其他路径会漏
- 所以现在把 mode diff 检测统一收束到这里

这是非常成熟的“单一副作用入口”思路。

通俗解释：

以前像是：

- 8 扇门都能改模式
- 但只有 2 扇门会记得顺手发通知

现在变成：

- 不管从哪扇门进来改模式
- 最后都经过这个总闸门
- 通知一定发

这类设计能大量减少“有时候同步、有时候不同步”的隐蔽 bug。

---

## 13. REPL.tsx：为什么它看起来这么“巨大”

[REPL.tsx](../src/screens/REPL.tsx) 很大，这是正常的。

因为它本质上不是一个简单页面，而是 Claude Code 的：

**交互主控屏。**

它要同时处理：

- 输入
- 消息渲染
- permission 弹窗
- elicitation dialog
- hook prompt dialog
- remote session
- teammate / swarm
- proactive / scheduled tasks
- IDE 集成
- session restore
- cost / token / notification
- background task navigation

也就是说，REPL 不是一个普通“页面组件”。

它更像一个实时控制台。

通俗解释：

网页里很多功能可以分到不同页面。

但 Claude Code 是终端应用，大量东西都挤在一个交互面板里同时活着，所以 REPL 自然会成为总调度场。

---

## 14. 为什么 REPL 不把所有状态都自己管

这是一个很值得学的点。

虽然 `REPL.tsx` 很大，但 Claude Code 并没有把所有东西都塞成 `useState(...)`。

它仍然把大量全局运行态放在 AppState 里，把本地交互态留在组件内部。

你可以把它理解成两类状态。

### 14.1 全局运行态

这些会被 AppState 管：

- 当前权限模式
- 当前任务列表
- MCP clients/tools
- 远程桥接状态
- session hooks
- notifications / elicitation queue

### 14.2 局部交互态

这些往往留在组件内部：

- 当前输入光标位置
- 某个 dialog 是否暂时打开
- 当前搜索关键词
- 某些临时 UI 选中项

这是一个很成熟的分层：

- 世界状态进 AppState
- 组件瞬时交互留本地

如果什么都进全局，系统会非常难维护。

---

## 15. Messages.tsx：它不只是“把消息 map 一遍”

[Messages.tsx](../src/components/Messages.tsx) 是理解 Claude Code UI 质量的关键文件之一。

它真正做的事情很多：

- 消息规范化
- tool use / thinking / attachment 等消息重组
- 各类折叠
- brief 模式过滤
- transcript / fullscreen / selector 等不同视图策略
- logo/header 状态
- virtualization 切换
- streaming text / streaming thinking 的拼接展示

所以它不是：

```tsx
messages.map(...)
```

而更像：

**对 runtime 里积累的大量消息对象，做“最终可呈现化”的转换层。**

这一步特别重要，因为 runtime 里的消息结构往往是为“正确性”设计的，不是为“好看”设计的。

Messages.tsx 就是把“能跑的内部消息”翻译成“用户能读的界面结构”。

---

## 16. 一个非常典型的产品化细节：brief mode

`Messages.tsx` 里有 `filterForBriefTool()` 和 `dropTextInBriefTurns()`。

这类逻辑很能说明 Claude Code 的设计水平。

它不是把所有消息无差别展示，而是根据产品模式重新组织可见内容。

例如在 brief-only 模式里：

- 只展示 Brief tool 的关键输出
- assistant 冗余文本会被隐藏
- 但必要的系统/错误消息仍保留

通俗解释：

Claude Code 很清楚：

系统里“存在的消息”不等于“用户应该看到的消息”。

这是很多项目从 demo 走向产品时必须跨过去的一步。

---

## 17. VirtualMessageList：为什么终端里也要虚拟列表

[VirtualMessageList.tsx](../src/components/VirtualMessageList.tsx) 是非常值得学习的一块。

它说明 Claude Code 不是“消息少的时候看起来还行”，而是认真考虑了长会话。

在长会话里，如果所有消息都一直渲染：

- 重绘很重
- 滚动很卡
- CPU 飙高
- Ink screen diff 成本上升

所以它做了虚拟列表、测量、高度缓存、搜索跳转等机制。

通俗解释：

不是 3000 条消息全画在屏幕上，而是：

- 只画当前看得到的那一段
- 其他的先放着
- 需要跳转时再快速定位

这跟 Web 大型列表虚拟化是一个思路，只是这里是在终端里做。

---

## 18. VirtualMessageList 里最值得学的几个点

### 18.1 把 virtualization 从 Messages.tsx 分出去

文件注释里直接写了：

为了保证 hooks 规则成立，`useVirtualScroll` 要无条件调用，所以把它拆到单独组件里。

这说明 Claude Code 的拆分不是“按功能名拆”，而是“按 React 运行约束拆”。

### 18.2 有高度测量与缓存意识

在终端里，不同宽度下文本高度会变化。

所以它明确把 `columns` 作为缓存失效条件。

这是很专业的终端 UI 思维：

- 宽度一变
- 文本重新换行
- 高度缓存就可能错
- 不处理会出现黑屏或滚动错位

### 18.3 search index / sticky prompt / cursor nav 都和列表耦合

它没有把这些能力粗暴地丢给上层，而是放在最了解“行高和滚动”的地方。

这也是对的。

因为只有列表本身最清楚：

- 当前可视区在哪
- 某条消息高度多少
- 某个搜索命中滚过去后应该落在哪

---

## 19. sticky prompt 是什么，为什么值得学

`VirtualMessageList.tsx` 里有 `stickyPromptText()` 相关逻辑。

它做的是：

当用户往下滚很远时，仍然能把当前相关 prompt 的摘要作为“粘性提示”保留。

这很像 Web 里的 sticky header，但这里是消息语义版。

通俗解释：

你滚到很下面看工具结果时，系统怕你忘了“这一大段到底是在回答哪个问题”，所以它会把用户原始 prompt 摘出来，像标签一样挂着。

这个细节特别有产品味。

它不是功能必须，但会显著提升长会话可读性。

---

## 20. PromptInput.tsx：为什么输入框比你想的复杂得多

[PromptInput.tsx](../src/components/PromptInput/PromptInput.tsx) 体量很大，因为输入框在 Claude Code 里根本不是一个普通文本框。

它至少同时承担：

- 普通文本输入
- slash command 输入
- 历史搜索
- typeahead / suggestion
- pasted references
- 图片粘贴
- modal dialog 协调
- vim 模式
- mode 切换
- fast / thinking / auto / plan 相关 UX
- teammate / swarm 视图切换
- command queue 的可编辑内容接管

所以如果你把它当“输入框组件”，会完全低估它。

更准确地说，它是：

**Claude Code 所有主动交互入口的统一控制台。**

---

## 21. 为什么 PromptInput 要和 command queue 接在一起

在 [PromptInput.tsx](../src/components/PromptInput/PromptInput.tsx) 里可以看到：

- `useCommandQueue()`
- `popAllEditable`
- 与 queued commands 的联动

这说明输入框不是只管键盘输入。

它还要能接住：

- 被队列缓存的可编辑输入
- 中途暂停的 prompt
- 某些被系统暂存后重新恢复的文本

通俗解释：

Claude Code 的输入框不是“你此刻手打的这一行字”。

它更像一个“正在编辑的意图缓冲区”。

而这个缓冲区，有时候来自键盘，有时候来自队列恢复。

这也是为什么它必须和统一消息队列接上。

---

## 22. UI 状态为什么要分三层来看

如果你真要学 Claude Code，我建议把 UI 状态分三层理解。

### 22.1 运行态全局状态

在 AppState 里：

- tasks
- mcp
- bridge
- permissions
- notifications
- view mode

### 22.2 页面级协调状态

多在 REPL 里：

- 当前屏幕模式
- 当前 dialog 是否活跃
- 当前流式消息态
- 当前搜索/选择状态

### 22.3 组件内部交互态

多在具体组件里：

- 光标位置
- hover
- 局部选择
- 某个输入法状态

如果你把这三层混在一起，系统就会很快失控。

Claude Code 的优秀之处，就是它虽然复杂，但大致遵守了这个分层。

---

## 23. useSyncExternalStore 为什么这么关键

在 `AppState.tsx`、`PromptInput.tsx`、`VirtualMessageList.tsx`、voice context 等地方都能看到 `useSyncExternalStore`。

它的价值在于：

- React 能稳定订阅外部 store
- 并发/一致性语义更安全
- 不需要整个树都靠 prop drilling

对 Claude Code 这种应用来说，这几乎是理想选择，因为它本来就不是纯组件驱动的。

它有大量：

- 非 React runtime
- 异步任务
- 外部事件源
- 队列
- bridge / sdk / remote

这时候，外部 store + 选择性订阅是很自然的。

通俗解释：

React 组件不是这个系统唯一的“活物”。

既然如此，就不能把状态只锁在 React 自己家里。

---

## 24. 为什么 Claude Code 这么重视 memo / re-render 控制

在 `Messages.tsx`、`VirtualMessageList.tsx` 等文件里，注释会非常认真地解释性能问题。

比如：

- logo header memo 化，否则会导致整棵消息树失去 blit 优势
- 长会话下可能 150K+ writes/frame
- 滚动时 closure / GC 的代价
- 宽度变化导致高度缓存失效

这说明 Claude Code 团队不是“用 React 写 CLI 看起来舒服就行”。

而是认真做了终端渲染性能优化。

这点特别值得学。

因为终端 UI 和 Web UI 很不一样：

- 浏览器帮你做了很多优化
- 终端字符重绘更原始
- 一点点多余 rerender 都可能直接变卡

所以他们会非常在意：

- selector 粒度
- memo 边界
- 虚拟列表
- OffscreenFreeze
- 渲染缓存

---

## 25. 为什么说 Claude Code 的 UI 不是“消息驱动”，而是“状态驱动 + 消息呈现”

很多 AI 产品会偷懒，直接把一切都塞进消息数组里驱动界面。

Claude Code 不是这样。

它当然有 `messages`，但还有一个非常大的 AppState。

这说明它的 UI 不是只靠 transcript 决定的。

真实决定界面的，是二者一起：

- messages 决定“说了什么”
- AppState 决定“现在处于什么运行场景”

举例：

- 是否 brief only
- 当前是否在 teammate view
- permission mode 是什么
- bridge 是否连接
- 有无 notifications
- 当前 dialog 队列里有什么

这些很多都不是 message。

通俗解释：

消息像“聊天记录”。

AppState 像“当前应用模式”。

真正的界面体验，是这两者叠加出来的。

---

## 26. 为什么说 Claude Code 的终端 UI 更像一个应用，而不是一个命令输出器

看到这里，你应该会越来越清楚：

Claude Code 的终端不是：

```text
输入一行
打印一段
结束
```

而更像一个常驻应用：

- 有全局状态
- 有多视图
- 有后台任务
- 有通知队列
- 有 modal
- 有输入模式
- 有远程连接状态
- 有插件与外部工具状态

这就是为什么它需要：

- AppState
- store
- lifecycle side effects
- virtualization
- 选择性订阅

这整套东西拼起来，Claude Code 才从“会说话的 CLI”变成了“终端中的 AI IDE”。

---

## 27. 这套设计最值得借鉴的地方

这一部分最适合你以后自己做系统时带走。

### 27.1 用一个统一运行态来承载复杂应用

当你的 agent 产品开始出现：

- 权限
- 通知
- 后台任务
- 多视图
- 外部工具
- remote session

就不要再幻想只靠局部组件 state 撑住。

你需要一个像 AppState 这样的统一运行态。

### 27.2 全局状态不等于全量订阅

Claude Code 很值得学的一点是：

- 有全局状态
- 但不是所有组件都订阅全部

而是 selector 化订阅。

这是性能和可维护性的关键。

### 27.3 非 React runtime 要有合法入口

工具执行、session restore、remote bridge、SDK 控制流，都不是 React 组件。

如果这些东西不能合法读写状态，最后就会出现各种野路子同步。

Claude Code 用外部 store 正好解决了这个问题。

### 27.4 渲染层要允许“二次组织”

内部消息结构不一定适合直接展示。

`Messages.tsx` 这种“最终可呈现化层”非常值得抄。

### 27.5 长会话从第一天就要考虑

虚拟列表、memo、缓存这些东西，不一定第一版就全做，但设计上要留口子。

否则后面会很痛苦。

---

## 28. 哪些地方你不要机械照搬

虽然 Claude Code 很强，但不是每个项目都要一比一复制。

### 28.1 AppState 字段不要一开始就做这么大

Claude Code 的 AppState 很庞大，是因为产品已经很成熟。

你自己的系统第一版可以先收敛到：

- session
- messages
- permissions
- tasks
- notifications
- currentView

### 28.2 不是所有项目都需要 Ink 级复杂度

如果你只是做：

- 一次性命令行工具
- 非交互批处理

那没必要上到这种终端应用架构。

### 28.3 虚拟列表要等真的有长会话需求再上

虚拟列表很值，但也增加复杂度。

如果你的消息规模还很小，可以先留设计空间，不急着完整实现。

---

## 29. 学这块时最容易误解的几个点

### 29.1 误解一：React 就是 UI 主体，所以业务逻辑都该写组件里

不对。

Claude Code 证明了：

- React 负责表达 UI
- runtime 负责推进流程
- 两者中间靠 store/context/queue 对接

### 29.2 误解二：有 messages 就够了

不对。

messages 只够做 transcript，不够做完整应用。

### 29.3 误解三：CLI 不需要太在意渲染性能

不对。

长会话终端 UI 一样会非常卡，甚至比 Web 更容易卡。

### 29.4 误解四：输入框只是一个文本框

不对。

在 Claude Code 里，输入框几乎是整个交互系统的总入口之一。

---

## 30. 推荐的阅读顺序

如果你准备继续自己读源码，我建议按这个顺序：

1. [state/store.ts](../src/state/store.ts)
2. [state/AppState.tsx](../src/state/AppState.tsx)
3. [state/AppStateStore.ts](../src/state/AppStateStore.ts)
4. [components/App.tsx](../src/components/App.tsx)
5. [replLauncher.tsx](../src/replLauncher.tsx)
6. [screens/REPL.tsx](../src/screens/REPL.tsx)
7. [components/Messages.tsx](../src/components/Messages.tsx)
8. [components/VirtualMessageList.tsx](../src/components/VirtualMessageList.tsx)
9. [components/PromptInput/PromptInput.tsx](../src/components/PromptInput/PromptInput.tsx)
10. [state/onChangeAppState.ts](../src/state/onChangeAppState.ts)

这个顺序会比较好消化。

---

## 31. 最后一段最通俗的总结

如果我用最白话的话来概括 Claude Code 这套 UI 架构，那就是：

**它先用 AppState 把“整个终端应用正在发生什么”统一起来，再用 React + Ink 把这些状态有选择地画出来。**

所以它不是那种：

- 模型一回答
- CLI 打一段字
- 然后结束

而是那种：

- 终端里一直有一个活着的应用
- 这个应用有自己的世界状态
- 你看到的消息、任务、通知、弹窗、输入框，其实都是这个世界状态的不同投影

这就是为什么 Claude Code 值得学。

因为它教你的不是“怎么做个聊天框”，而是：

**怎么把一个 AI runtime 做成一个真正可长期运行的终端产品。**

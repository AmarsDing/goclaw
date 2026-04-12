# Claude Session Compaction / Context Trimming Deep Dive

## 1. 这篇只讲最关键的问题

Claude Code 的 compact 机制，最值得学的不是“会总结上下文”。

真正关键的是这三个问题：

1. 上下文太长时，系统怎么安全地缩短它
2. 缩短以后，为什么还能继续干活，不会像失忆
3. 这套机制为什么不只是“把历史消息丢给模型总结一下”

如果你以后自己做 agent runtime，这一块非常重要。因为只要系统开始长会话、多工具、多轮工作，迟早会碰到：

- token 不够
- 历史太长
- 总结本身也会爆 context
- 压缩完以后模型变笨、变飘、忘细节

Claude Code 在这方面做得很成熟。

关键源码我主要参考这些：

- [commands/compact/compact.ts](../src/commands/compact/compact.ts)
- [services/compact/compact.ts](@src/services/compact/compact.ts)
- [services/compact/microCompact.ts](@src/services/compact/microCompact.ts)
- [services/compact/postCompactCleanup.ts](@src/services/compact/postCompactCleanup.ts)
- [services/compact/prompt.ts](@src/services/compact/prompt.ts)

---

## 2. 先给一句最通俗的定义

Claude Code 的 compact，不是“清空聊天记录”。

它更准确地说是：

**把旧上下文压缩成一个可继续工作的工作摘要，再把系统状态重新整理到一个更短、但还能接着干活的上下文里。**

这里面有两个关键词：

- 工作摘要
- 继续工作

很多系统只做到前者，Claude Code 明显在努力做到后者。

---

## 3. 它的主调用链是什么

最核心的链路在 [commands/compact/compact.ts](@src/commands/compact/compact.ts)：

```text
/compact
  -> 先拿 messages
  -> 去掉 compact boundary 之前已被裁掉的旧消息
  -> 优先尝试 session memory compaction
  -> 如需传统 compact，则先 microcompact
  -> 再 compactConversation()
  -> 成功后做 postCompactCleanup()
  -> 返回新的 compact 结果与展示文本
```

如果走 reactive 路径，还会多一条：

```text
executePreCompactHooks
  + build cache sharing params
  -> reactiveCompactOnPromptTooLong()
  -> execute post-compact cleanup
```

所以 compact 不是一个单函数。

它是一个多阶段流水线。

---

## 4. Claude Code 为什么把 compact 放成流水线

因为“上下文太长”不是单一问题。

它至少有四种不同层面的处理：

1. 先删一点不值钱但很占 token 的内容
2. 再决定要不要做真正总结
3. 总结时还要防止总结自己再次爆 context
4. 总结完要清理缓存和状态，不然系统会前后不一致

这正是 Claude Code 的做法：

- `microcompact` 先做局部压缩
- `compactConversation` 再做正式总结
- `postCompactCleanup` 做运行时善后

这很值得借鉴，因为很多系统一上来就：

```text
messages -> summarize -> replace
```

这样很快就会踩坑。

---

## 5. 第一步为什么是 `getMessagesAfterCompactBoundary`

在 [commands/compact/compact.ts](@src/commands/compact/compact.ts) 开头有一句很关键：

- REPL 会为了 UI scrollback 保留一部分被“剪掉”的消息
- 但 compact 模型不应该再去总结这些本来就被裁掉的内容

所以会先：

- `getMessagesAfterCompactBoundary(messages)`

这一步非常重要。

通俗解释：

UI 为了让你还能往上翻历史，会保留一些“展示用残影”。

但 compact 不能把这些残影当成真实活跃上下文再总结一遍，否则就会出现：

- 重复总结
- 旧内容污染新摘要
- token 浪费

这说明 Claude Code 明确区分：

- UI 还想显示什么
- 模型现在还应该知道什么

这就是成熟系统和 demo 的差别。

---

## 6. 为什么优先尝试 Session Memory Compaction

`compact.ts` 里第一优先不是立刻跑通用总结，而是：

- `trySessionMemoryCompaction(...)`

而且只在没有自定义 compact 指令时优先走这条路。

这反映出 Claude Code 的一个重要策略：

**如果能用更结构化、更便宜、更稳的记忆压缩方式解决，就不要急着走“大模型重总结整段对话”。**

通俗解释：

不是每次都重新写一篇长摘要。

有时候系统已经有一套更结构化的“会话记忆”机制，那就优先走它。

这很像：

- 能用数据库增量更新，就不要每次全表重算

这是产品级系统很典型的思路。

---

## 7. `microcompact` 是这套系统里最值得学的一个点

[microCompact.ts](@src/services/compact/microCompact.ts) 非常值得学。

因为它说明 Claude Code 不是一遇到 token 紧张就让模型做总结，而是先做：

**低风险、结构化、局部的上下文瘦身。**

它的核心思路是：

- 只针对某些特别占 token 的 tool 结果
- 不急着删消息结构本身
- 先把低价值的重内容清掉或替换掉

文件里定义了一个 `COMPACTABLE_TOOLS` 集合，比如：

- `FileRead`
- shell 类工具
- `Grep`
- `Glob`
- `WebSearch`
- `WebFetch`
- `FileEdit`
- `FileWrite`

这很合理，因为这些工具最容易产生：

- 长文件内容
- 大量终端输出
- 大段搜索结果

这些东西特别吃 context，但对“继续工作”未必都要原样保留。

---

## 8. 为什么 `microcompact` 值得你重点学

因为它背后的思想很强：

**先做结构化垃圾回收，再做语义总结。**

通俗解释：

如果一个会话里 60% 的 token 都是：

- 长日志
- 大文件读出
- 搜索结果

那你根本没必要立刻让模型总结“整段人生”。

先把这些大块低性价比内容瘦下来，往往就能省很多 token，甚至避免一次正式 compact。

这比“啥都交给模型总结”聪明得多。

这是 Claude Code 很实战的一点。

---

## 9. `microcompact` 不是随便删，而是带状态管理

`microCompact.ts` 不只是清文本，它还有一套状态机制：

- `pendingCacheEdits`
- `getPinnedCacheEdits`
- `pinCacheEdits`
- `markToolsSentToAPIState`
- `resetMicrocompactState`

这说明 microcompact 不是一次性的字符串替换。

它和 prompt cache / 后续请求位置稳定性 / 增量重发都有关系。

换句话说，Claude Code 在意的不只是“这次请求短一点”，还在意：

- 后面几次请求能不能继续吃到 cache
- 哪些 edits 需要固定重发
- 哪些 state 要跨轮保留

这就是非常产品化的工程思路。

---

## 10. 为什么 compact 连 prompt 都写得这么激进

[services/compact/prompt.ts](@src/services/compact/prompt.ts) 是另一个很值得学的文件。

开头那段 `NO_TOOLS_PREAMBLE` 非常直白：

- 只能输出文本
- 不要调用任何工具
- 你已经有足够上下文
- 调工具会被拒绝，而且会浪费你唯一一轮

这是一个非常成熟的补丁式设计。

它不是抽象层面的“模型最好别调工具”。

而是明确知道：

- compact 走的是 fork path
- fork path 为了 cache key 共享会继承完整工具集合
- 某些模型会忍不住调用工具
- 一旦调了工具，这次 compact 可能就废了

所以他们直接把规则写死在 prompt 前面。

通俗解释：

这就像考试时老师先说：

- 这道题只能写文字答案
- 不许翻资料
- 你只有一次机会

这不是优雅不优雅的问题，而是为了让系统稳定。

---

## 11. Claude Code 想让 compact 产出什么样的摘要

`prompt.ts` 很清楚地表明：

Claude Code 不是要一个“模糊摘要”，而是要一个**能接着开发的工作摘要**。

它要求的内容包括：

- 用户请求和意图
- 关键技术概念
- 涉及的文件与代码片段
- 错误与修复
- 问题解决过程
- 全部用户消息
- pending tasks
- current work
- optional next step

这说明 compact 在 Claude Code 里不是“聊天记忆”，而是“工作交接文档”。

这个定位非常关键。

通俗解释：

它不是“帮我概括一下刚刚聊了啥”。

它更像：

**“现在你要下班了，请把今天做的工作、改过的文件、剩下的事都交接给下一班的你自己。”**

---

## 12. 为什么它还要保留 `<analysis>` 再剥离

`compact/prompt.ts` 里要求模型先写 `<analysis>`，再写 `<summary>`。

而源码注释也说明：

- `<analysis>` 更像草稿区
- 最终进入上下文的是整理后的 summary

这很值得注意。

这不是为了好看，而是为了让模型先做一遍内部梳理，再输出结构化总结。

通俗解释：

有点像让一个人“先打草稿，再交正式版”。

正式版要短而稳，但草稿可以帮助它先把信息整理好。

这说明 Claude Code 很在意 summary 的质量，而不只是有没有。

---

## 13. `compactConversation()` 真正做的，不只是总结

[services/compact/compact.ts](@src/services/compact/compact.ts) 很大，但如果只抓核心，它做了几类事情：

1. 为 compact 准备干净消息
2. 剥离一些不适合送给总结模型的内容
3. 管理 prompt-too-long 重试
4. 处理 pre/post compact hooks
5. 重新构造 compact boundary 之后的新上下文
6. 记录和恢复必须保留的一些运行信息

所以它更像一个“压缩事务处理器”。

不是“发个 summarize API”这么简单。

---

## 14. 为什么要 strip images / documents

在 `compact.ts` 里有 `stripImagesFromMessages()`。

注释写得非常清楚：

- 图片对生成会话摘要通常不是必须的
- 但它们会显著增加 compact 请求本身的 token
- 所以先替换成 `[image]`、`[document]` 这类占位信息

这非常合理。

通俗解释：

如果用户之前传过很多图，compact 的目标不是重新“看懂这些图”，而是保留“这里曾经有图、图和任务有关”这个事实。

所以用轻量占位就够了。

这是典型的：

- 保留语义痕迹
- 丢掉高成本原文

---

## 15. 为什么要 strip reinjected attachments

`stripReinjectedAttachments()` 的意思也很关键：

- 有些 attachment 反正 compact 后会重新注入
- 那就没必要再送给 summarizer

这说明 Claude Code 不把 compact 看成“单次孤立行为”，而是放在整个 runtime 生命周期里思考。

换句话说，它知道：

- 哪些东西稍后系统自己会补回来
- 那么在总结阶段就不必浪费 token

这很像编译器做 dead-code elimination。

非常值得借鉴。

---

## 16. compact 自己也可能爆 context，这怎么解决

这是很多系统忽略的点。

Claude Code 明确处理了这个场景。

在 [services/compact/compact.ts](@src/services/compact/compact.ts) 里有：

- `PROMPT_TOO_LONG_ERROR_MESSAGE`
- `truncateHeadForPTLRetry(...)`

也就是说：

**连 compact 请求本身都可能太长。**

这时候系统不会直接死掉，而是：

- 先按 API round 分组
- 丢掉更老的一部分组
- 插一个 retry marker
- 再重试 compact

这非常重要，因为这是真正的“系统自救”。

通俗解释：

不是“我要总结这堆历史，但这堆历史太长，连总结都塞不进去，于是程序死了”。

而是：

- 先再砍掉最老一截
- 至少把人从彻底卡死里救出来

这就是成熟产品会做的兜底。

---

## 17. 为什么 compact 后还要 `postCompactCleanup`

[postCompactCleanup.ts](@src/services/compact/postCompactCleanup.ts) 是非常关键的一环。

很多系统做完 compact 就结束了，但 Claude Code 还要做一堆 cleanup：

- `resetMicrocompactState()`
- `clearSystemPromptSections()`
- `clearClassifierApprovals()`
- `clearSpeculativeChecks()`
- `resetGetMemoryFilesCache(...)`
- `clearSessionMessagesCache()`
- `clearBetaTracingState()`

这说明他们非常清楚：

**compact 改的不只是消息数组，而是整个运行时上下文的基线。**

如果不清理这些缓存和 tracking 状态，就会出现：

- 旧缓存引用旧消息
- classifier 记忆还沿用旧上下文
- memory files 加载逻辑失真
- tracing 或 attribution 状态前后不一致

通俗解释：

compact 不是只换了一页纸，而是整个桌面都重新整理了一遍。

那桌上所有“上一次布局”的便签、索引、缓存都得一起重置。

---

## 18. 为什么还特别区分 main thread compact 和 subagent compact

`postCompactCleanup.ts` 有一段很重要的逻辑：

- subagent 和 main thread 共享同一进程里的模块级状态
- 所以 subagent compact 时，不能乱清 main thread 的全局缓存

这点非常成熟。

它反映出 Claude Code 已经不是单一线程单一会话模型，而是：

- 主线程
- subagent
- 同进程共享部分模块状态

这时候 compact 就不再只是“本地数组换一下”，而必须考虑**作用域**。

这是非常值得学习的地方。

通俗解释：

一个房间里坐了主工程师和几个助手。

某个助手把自己的桌面整理了一遍，不代表整个办公室的公共白板也该被擦掉。

Claude Code 正是在防这种误伤。

---

## 19. compact 与 hooks 的关系，说明它不是内部私活

在 `commands/compact/compact.ts` 和 `services/compact/compact.ts` 里都能看到：

- `executePreCompactHooks`
- `executePostCompactHooks`
- `mergeHookInstructions`

这说明 compact 不是一个完全封闭的内部操作。

它也是 runtime 生命周期的一部分，可以被 Hook 系统感知和影响。

尤其是：

- pre-compact hook 还能影响 custom instructions

这很有意思，意味着外部扩展能够参与“这次总结应该偏重什么”。

这个设计非常平台化。

---

## 20. 这套 compact 设计真正厉害在哪

如果只看表面，你可能会说：

- 有 microcompact
- 有 summary
- 有 cleanup

但更深一层，它厉害在三个设计理念。

### 20.1 分层压缩

不是一刀切。

而是：

- 先结构化减肥
- 再语义总结
- 再状态重置

### 20.2 保持可继续工作

摘要目标不是“方便回忆”，而是“方便继续开发”。

### 20.3 把 compact 视为运行时事务

不是文本处理，而是：

- messages
- caches
- hooks
- prompt cache
- session state

都要一起调整。

---

## 21. 最值得借鉴的设计点

如果你以后自己做 agent 平台，这几条最值得直接拿走。

### 21.1 先局部瘦身，再整体总结

`microcompact` 这个思路非常实用。

不要一上来就让模型总结全部上下文。

先想想有没有明显的“高 token、低复用价值”内容可以结构化清理。

### 21.2 compact 的目标要写成“工作交接”

Claude Code 的 compact prompt 非常像工程交接文档，而不是聊天摘要。

如果你的 agent 是干活用的，这个定位非常值得学。

### 21.3 compact 本身要能自救

如果 compact 自己都可能爆 token，你必须有 retry / truncation 兜底。

不然一旦会话太长，系统就会进入“不能继续，也不能自救”的死锁状态。

### 21.4 compact 结束后一定要 reset 相关缓存

这是很多系统最容易漏的点。

只换 messages 不够。

任何基于旧消息、旧 prompt、旧审批状态、旧 memory 的缓存，都要重新审视。

### 21.5 要有边界意识

主线程 compact、subagent compact、remote session compact，影响范围不一定一样。

不要把所有 cleanup 都做成全局无差别重置。

---

## 22. 哪些地方你不要硬抄

Claude Code 这套 compact 很强，但不是每个项目都需要同等复杂度。

### 22.1 如果你还没有长会话，不要先上全套

第一版完全可以先做：

- 基础 summarize
- 简单 boundary
- 少量 cleanup

### 22.2 如果没有 prompt cache，就先别抄 cache edit 那套

`microcompact` 的一些状态管理和 prompt cache 强绑定。

如果你的系统还没有缓存层，照抄只会让复杂度虚高。

### 22.3 先定义 compact 的产品目标

Claude Code 的目标是“继续编码工作”。

如果你的产品目标是“知识问答连续性”，compact prompt 应该完全不同。

---

## 23. 一段最通俗的总结

Claude Code 的 compact，本质上不是“把聊天历史缩短”。

它更像：

**给一个长期工作的 agent 做一次“安全瘦身 + 工作交接 + 状态重置”。**

真正值得学的是它的顺序：

- 先删最占地方但最不值钱的东西
- 再总结真正重要的工作上下文
- 再把运行时里依赖旧上下文的缓存一起清掉

这三步配在一起，Claude Code 才能在长会话里继续稳定工作。

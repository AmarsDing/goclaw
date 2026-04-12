# Claude Code 上下文压缩与记忆系统解读

## 1. 这篇文档在讲什么

Claude Code 不是一个“把 prompt 一股脑丢给模型”的简单 CLI，它内部有一整套上下文治理系统，目标是解决两个很现实的问题：

1. 对话越跑越长，模型上下文一定会被吃满。
2. 有些信息只对“当前这轮”有用，有些信息应该跨会话长期保存，不能混在一起。

所以 Claude Code 做了两件事：

1. 做“上下文压缩（context compression / compaction）”
2. 做“长期记忆（memory）和会话恢复（session restore）”

通俗一点说：

- 上下文压缩，像是在旅行箱快塞满时，先把临时包装纸、收据、空盒子丢掉，再决定要不要压缩衣服，最后才换更大的收纳方式。
- 长期记忆，像是把“以后还会用到的事”单独记进笔记本，而不是一直塞在手里那张便签纸上。

这两套系统配合起来，Claude Code 才能既“连续工作很多轮”，又“不至于越聊越笨”。

---

## 2. 先给一个总图

### 2.1 当前会话里的上下文治理链

一次正常对话里，Claude Code 会按大致这样的顺序处理上下文：

1. 用户输入进入主循环
2. `query.ts` 在真正请求模型前分析 token 压力
3. 如果还没爆，就正常继续
4. 如果快爆了，先走轻量级削减
5. 还不够，再走 microcompact
6. 再不够，走 compact summary
7. 如果 build 打开了更激进的能力，还可能走 context collapse

源码主链在：

- [query.ts](../src/query.ts)
- [compact.ts](../src/services/compact/compact.ts)
- [microCompact.ts](../src/services/compact/microCompact.ts)
- [contextCollapse/index.ts](../src/services/contextCollapse/index.ts)

### 2.2 长期记忆和恢复链

长期持久化则是另一条线：

1. 当前会话消息被记入 transcript
2. 文件历史、归因、collapse 状态等也一起入库
3. 恢复会话时，从 transcript 和快照重建运行时状态
4. 另外，真正跨会话要记住的信息进入 memory 目录
5. 普通模式下以 `MEMORY.md + topic files` 为核心
6. Kairos 模式下改成 append-only daily log，再由 dream 流程整理

核心文件：

- [sessionStorage.ts](../src/utils/sessionStorage.ts)
- [sessionRestore.ts](../src/utils/sessionRestore.ts)
- [memdir.ts](../src/memdir/memdir.ts)
- [paths.ts](../src/memdir/paths.ts)
- [autoDream.ts](../src/services/autoDream/autoDream.ts)

---

## 3. Claude Code 为什么要做“分层压缩”

很多 agent demo 的做法很粗暴：

- 上下文满了，就截断前面消息
- 或者直接让模型总结整个历史

Claude Code 没这么干。它更像一个“分层降载系统”。

这是非常值得学的一点，因为不同类型的信息，价值不一样：

- 工具返回的超长输出，通常价值最低
- 最近几轮对话，价值很高
- 用户长期偏好，应该根本不跟“当前上下文”混在一起

所以它不是“一刀切压缩”，而是先做最便宜、最不伤语义的削减。

通俗解释：

- 不是先把书撕掉重写摘要
- 而是先把附录、截图、临时日志、重复说明拿走
- 真到不够了，才开始压缩正文

---

## 4. `query.ts` 里的真实压缩顺序

`query.ts` 是主 agent loop 的核心文件，也是压缩逻辑真正发生的地方。

从代码路径看，它在请求模型前后会分层尝试这些动作：

1. `applyToolResultBudget`
2. snip compact
3. microcompact
4. context collapse
5. autocompact / full compact

这些逻辑集中在：

- [query.ts](../src/query.ts)

### 4.1 第一层：先砍“工具结果预算”

这层的思想最朴素：

- 不是先碰用户消息和系统规则
- 而是先看工具输出是不是太大了

为什么？

因为很多工具输出只是“执行痕迹”，不是核心推理上下文。

比如：

- 一个 `grep` 找到了 200 行
- 一个 shell 命令打出很长日志
- 一个 file read 包含很大文件片段

这些内容如果原封不动留在上下文里，性价比很差。

这一步可以理解成：

- 先清理“缓存垃圾”
- 尽量不碰真正有语义价值的对话

---

## 5. `microCompact`：不是总结历史，而是局部清理工具痕迹

`microCompact` 是 Claude Code 非常巧妙的一层。

对应源码：

- [microCompact.ts](../src/services/compact/microCompact.ts)

### 5.1 它到底做什么

它不是把整段对话总结掉，而是优先处理“可压缩工具”产生的结果。

代码里有一个 `COMPACTABLE_TOOLS` 集合，里面包含：

- 文件读写
- shell
- grep / glob
- web search / web fetch

这说明 Claude Code 的策略是：

- 先压缩高噪声、低长期价值的工具结果
- 尽量保留用户意图和模型自己的推理结构

### 5.2 为什么这招很聪明

因为 agent 对话里最容易暴涨的，不是“用户说了很多”，而是“工具返回了很多”。

通俗理解：

- 用户一句“帮我搜整个项目”
- 工具可能回几千行

如果每次都让模型重新吃下这些内容，很浪费。

所以 microcompact 的本质是：

- 把“工具执行的痕迹”变成更轻的历史表示
- 让模型知道“做过这件事”，但不一定保留所有原始输出

### 5.3 它还考虑了 token 估算误差

`estimateMessageTokens()` 不是简单字符串长度，而是按 block 类型估算：

- text
- tool_result
- image / document
- thinking
- tool_use

这个细节很重要。

因为现代 agent message 不是纯文本，而是结构化 block 流。Claude Code 很清楚：

- 图片不是文字，但也要占预算
- tool_use 的 name + input 也要算
- thinking 也要算

这说明它不是“聊天系统思维”，而是“结构化 agent runtime 思维”。

### 5.4 它还有缓存编辑机制

`microCompact.ts` 里还有一层 `cached microcompact` 逻辑：

- `consumePendingCacheEdits()`
- `getPinnedCacheEdits()`
- `pinCacheEdits()`

这个设计的意思是：

- 压缩不是只为了少 token
- 还要尽量不破坏 prompt cache 命中

通俗解释：

- 不是“把东西删了就完”
- 而是“删的时候尽量别把前缀缓存打碎”

这就是工程味非常浓的地方。

---

## 6. `compact.ts`：真正的“整轮压缩”

当 microcompact 还不够时，才会进入更重的 compact 流程。

对应源码：

- [compact.ts](../src/services/compact/compact.ts)

### 6.1 这层会做哪些事

从 `compact.ts` 可以看出，它不是直接拿原消息去总结，而是先预处理：

1. 去掉图片和文档 block
2. 去掉那些后面本来就会重新注入的 attachment
3. 必要时做 prompt-too-long retry
4. 再调用 compact prompt 生成摘要

这在代码里有很清楚的意图：

- `stripImagesFromMessages()`
- `stripReinjectedAttachments()`
- `truncateHeadForPTLRetry()`

### 6.2 为什么要先 strip image / document

因为图片对“总结历史”帮助往往很小，却非常占 token。

代码甚至把它们替换成 `[image]`、`[document]` 这样的文本标记。

这招很实用：

- 不完全丢失语义
- 又大幅降低压缩请求本身的成本

通俗解释：

- 像做会议纪要时写“这里展示过一张图”
- 不需要把图重新贴进纪要里

### 6.3 为什么要 strip reinjected attachments

有些附件在后续 turn 本来就会重新注入。

如果你在 compact 阶段还把它们送去总结，会有两个问题：

1. 浪费 token
2. 让 summary 里混入过时附件信息

这非常像做系统设计时区分：

- “源数据”
- “派生数据”

Claude Code 在这里是把可重建的信息尽量不放进摘要。

### 6.4 `truncateHeadForPTLRetry()` 很值得学

当连 compact 请求本身都 prompt-too-long 时，它会退一步：

- 按 API round 分组
- 丢最老的 round
- 再试一次

这不是优雅的“最优算法”，但很实用。

源码注释里其实也承认了这一点：它是最后兜底的 fallback。

这说明 Claude Code 的工程哲学不是追求完美理论，而是：

- 先保证用户不会被卡死

这点很值得借鉴。

---

## 7. `context collapse`：更激进的长期折叠层

这份仓库里：

- [contextCollapse/index.ts](../src/services/contextCollapse/index.ts)

当前是一个 stub：

- `isContextCollapseEnabled(): false`
- `collapseContext(messages): 原样返回`

这说明外部构建里这块被阉割了。

但是从其他文件还能看出原设计意图。

比如：

- [sessionStorage.ts](../src/utils/sessionStorage.ts) 里有 `recordContextCollapseCommit()` 和 `recordContextCollapseSnapshot()`
- [sessionRestore.ts](../src/utils/sessionRestore.ts) 会在 resume 时恢复 collapse commit log

### 7.1 这意味着什么

它不是一次性 summary，而更像：

- 把一段旧上下文“归档折叠”
- 保留 commit 记录
- 恢复时还能重建视图

通俗理解：

- 不是把旧聊天简单浓缩成一段摘要
- 而是把旧历史做成“可追踪的折叠提交”

这很像 Git：

- 历史没有完全消失
- 只是主工作区只看压缩后的视图

这是比普通 compact 更高级的思路。

---

## 8. Claude Code 的 memory 系统不是“聊天记录”

很多人容易把 memory 和 transcript 混掉。

Claude Code 其实分得很清楚：

- transcript：这次会话发生了什么
- memory：以后还值得记住什么

核心定义在：

- [memdir.ts](../src/memdir/memdir.ts)

### 8.1 它明确限定了 memory 的类型

`buildMemoryLines()` 里会给模型一整套 memory 行为约束：

- 记什么
- 不记什么
- 怎么存
- 什么时候访问

而且是“类型化 memory”，不是随便往一个 markdown 里乱记。

它强调：

- 用户偏好
- 用户反馈
- 项目里不可从代码直接推导的信息
- 参考信息

同时明确排除：

- 能从代码和 git 推出来的东西

### 8.2 为什么这点特别重要

因为很多 agent 一旦有 memory，就会疯狂把一切都写进去。

结果就是：

- memory 越来越脏
- 很多内容过时
- 模型以后读 memory 反而被误导

Claude Code 的做法更保守：

- memory 只存“难以重建、跨会话有价值”的信息

通俗解释：

- “用户讨厌长解释”应该记
- “这个函数在第 42 行”不该记，因为代码一搜就知道

---

## 9. `MEMORY.md + topic files` 设计

普通模式下，它不是把所有记忆都写进一个大文件，而是：

1. 每条 memory 存成独立文件
2. `MEMORY.md` 只做索引入口

源码说明非常清楚：

- [memdir.ts](../src/memdir/memdir.ts)

### 9.1 为什么不直接只有一个 MEMORY.md

因为单文件会有三个问题：

1. 越写越长
2. 更新和删除很难维护
3. 难以按主题组织

所以它要求：

- memory 文件按主题拆开
- `MEMORY.md` 只保留一行索引指针

### 9.2 这本质上是“索引 + 正文”的设计

通俗理解：

- `MEMORY.md` 像目录页
- 各个 topic 文件像章节正文

这比“全塞一个 markdown”成熟很多。

---

## 10. Kairos 模式为什么改成 daily log

这部分在 `memdir.ts` 里也解释得很清楚。

当 Kairos 打开时，记忆策略会切换为：

- 不直接维护活的 `MEMORY.md`
- 改为往当天日志 append

对应：

- `buildAssistantDailyLogPrompt()`

### 10.1 为什么要这么改

因为 Kairos 是长时间运行助手。

如果还是实时维护索引，会很麻烦：

- 长会话里频繁重写 memory index
- 很容易冲突
- 也更容易污染稳定索引

所以它改成：

- 白天只追加日志
- 夜间再做整理

### 10.2 这其实是“冷热分层”

热层：

- daily log
- append-only
- 快速记下新东西

冷层：

- `MEMORY.md`
- topic files
- 经过整理后的稳定知识

通俗解释：

- 热层像随手笔记
- 冷层像整理后的知识卡片

这是一个很值得借鉴的 memory 架构。

---

## 11. `Searching past context`：memory 不够时还能搜历史

`memdir.ts` 里还有一段很重要的设计：

- `buildSearchingPastContextSection()`

它会教模型怎么查过去的内容：

1. 先搜 memory topic files
2. 再把 transcript 当最后手段

这说明 Claude Code 并不假设“所有历史都要常驻上下文”。

相反，它的理念是：

- 常驻上下文只放最重要的
- 不常用历史可以按需检索

这很像 RAG，但对象不是外部知识库，而是自己的会话和 memory 文件。

---

## 12. transcript 持久化：会话不是只活在内存里

长期恢复能力主要靠 transcript。

核心函数：

- [sessionStorage.ts](../src/utils/sessionStorage.ts#L1408)

### 12.1 `recordTranscript()` 做了什么

它会：

1. 清洗消息用于日志
2. 查哪些消息已经记过
3. 只写新消息
4. 维护 parent UUID 链

这里最巧妙的点在于：

- 它不是简单 append
- 它在维护“消息链”

所以 transcript 更像一个可恢复的事件链，而不是普通 log dump。

### 12.2 为什么 parent chain 很关键

因为 resume、continue、compact 后继续跑时，需要知道：

- 新消息接在哪个旧消息后面
- compact boundary 怎么截断链路

通俗解释：

- 这不是“聊天记录文本”
- 而是“可恢复执行轨迹”

---

## 13. `flushSessionStorage()` 和恢复机制

在运行过程中，Claude Code 会不断把状态刷到存储里。

关键函数：

- [sessionStorage.ts](../src/utils/sessionStorage.ts#L1583)

恢复时则走：

- [sessionRestore.ts](../src/utils/sessionRestore.ts)

### 13.1 它恢复的不只是消息

`restoreSessionStateFromLog()` 会恢复：

- 文件历史
- attribution 状态
- context collapse 提交记录
- todo 状态

这很重要，因为 agent 的“状态”不只是一串 message。

通俗解释：

- 如果只恢复聊天文本，不恢复工具状态
- 那它恢复出来的就不是原来的 agent

Claude Code 在这点上非常清醒：runtime state 和 transcript 是两个层次，但都要恢复。

---

## 14. `hydrateRemoteSession()`：远端会话也能落地到本地

`sessionStorage.ts` 里还有：

- `hydrateRemoteSession(sessionId, ingressUrl)`

这个函数会把远端 session logs 拉下来，写成本地 transcript 文件。

这说明它的会话体系设计不是“本地一套、远端一套互不相通”，而是：

- 远端 session 最终也能还原成统一的本地表示

这对多端和远程控制非常关键。

---

## 15. Claude Code 的核心思想：区分三种“记住”

这是理解整个系统最关键的一层。

Claude Code 实际上把“记住”拆成三类：

### 15.1 当前上下文记住

靠 `messages`

用途：

- 支持当前 turn 推理

特点：

- 最贵
- 最容易爆 token

### 15.2 当前会话记住

靠 transcript / session storage

用途：

- resume
- continue
- 恢复运行时状态

特点：

- 不要求每次都塞进 prompt
- 但必须持久化

### 15.3 跨会话长期记住

靠 memory dir

用途：

- 用户偏好
- 项目长期背景
- 未来还会用到的信息

特点：

- 更少、更稳、更语义化

通俗总结：

- `messages` 是工作台上的纸
- `transcript` 是项目过程记录
- `memory` 是长期知识卡片

很多 agent 系统做不好，就是把这三层混成一层。

---

## 16. Kairos 与普通模式在 memory 上的差异

普通模式：

- `MEMORY.md + topic files`
- 更像静态知识库

Kairos 模式：

- daily log append-only
- nightly dream consolidation

这说明 Kairos 不是“普通 agent 多加一点自动化”，而是运行范式都变了。

普通模式更像：

- 一次次对话

Kairos 更像：

- 一个持续在线的助手进程

所以 memory 策略也必须从“精致编辑索引”转成“日志先行，稍后整理”。

---

## 17. 最值得借鉴的设计点

这一部分是最适合你学习和复用的。

### 17.1 不要只做一种压缩

Claude Code 不是只有一个 `compact()`。

它至少分了：

- 工具结果预算控制
- microcompact
- compact summary
- context collapse

可借鉴点：

- 让压缩成为“分层降载链”，而不是单点功能

### 17.2 优先压缩低价值上下文

Claude Code 先碰的是：

- 工具结果
- 图片
- 可重建附件

而不是先碰：

- 用户意图
- system prompt

可借鉴点：

- 对上下文做“信息价值分级”

### 17.3 把 memory 和 transcript 分开

这是非常核心的架构思想。

可借鉴点：

- session log 负责恢复
- memory 负责长期知识
- 两者不要混写

### 17.4 long-lived assistant 要用日志式 memory

Kairos 那套 daily log 很值得抄。

可借鉴点：

- 高频新知识先 append
- 定时 consolidation
- 稳定索引和热日志分离

### 17.5 会话恢复不只是恢复 messages

Claude Code 恢复了：

- file history
- attribution
- todos
- collapse state

可借鉴点：

- agent state 要显式建模
- 不然 resume 只是“看起来像恢复”，本质并没恢复

### 17.6 压缩也要考虑 cache 命中

这点非常工程化，也很高级。

可借鉴点：

- 压缩策略不能只看 token
- 还要看是否破坏 prompt cache 和增量重用

---

## 18. 难点白话解释

### 18.1 `microcompact` 和 `compact` 到底差在哪

白话版：

- `microcompact` 像把聊天里的“工具执行垃圾”清掉
- `compact` 像把旧对话重写成一段总结

所以：

- microcompact 更轻、更保守
- compact 更重、更有损

### 18.2 为什么需要 transcript 和 memory 两套持久化

白话版：

- transcript 是“发生过什么”
- memory 是“以后值得记住什么”

一个偏执行记录，一个偏长期知识。

### 18.3 为什么 Kairos 要 daily log

白话版：

- 长期在线助手每天都会记很多小事
- 如果每记一条都去重写知识索引，成本太高，也容易乱

所以先写流水账，后面再整理成知识卡片。

### 18.4 `context collapse` 为什么像 Git commit

白话版：

- 不是把旧历史抹掉
- 而是把它折叠成一个“摘要提交”
- 需要时还能根据 commit log 恢复结构

---

## 19. 一句话总结

Claude Code 的上下文与记忆设计，本质上是在认真区分三件事：

1. 当前推理需要什么
2. 当前会话之后怎么恢复
3. 跨会话到底什么值得长期保存

它不是把“压缩”当成一个 API 调用，而是把它做成了一套分层的上下文治理系统；也不是把“记忆”当成一个 markdown 文件，而是做成了热日志、稳定索引、会话恢复三层配合的持久化架构。

如果你要自己做一个像样的 agent runtime，这套设计非常值得反复读。

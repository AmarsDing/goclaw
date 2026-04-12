# Claude Tool Result / Message Modeling Guide

## 1. 这篇为什么值得写

如果只选最后两篇里的其中一篇必须写，我会保留这篇。

因为 Claude Code 里很多看起来分散的设计，其实都建立在同一层基础上：

- tool use 为什么能和 tool result 对上
- UI 为什么能正确重排消息
- compact 为什么知道该压缩什么
- resume 为什么不会立刻把上下文搞坏
- subagent 为什么能复用同一套 runtime

这些事情的共同底层，其实是：

**Claude Code 对 message 的建模。**

不是“消息数组”这么简单，而是：

**把用户、助手、工具、进度、系统边界、附件，统一组织成一套既能渲染、又能发 API、还能恢复会话的消息模型。**

关键源码主要看这些：

- [utils/messages.ts](../src/utils/messages.ts)
- [query.ts](../src/query.ts)
- [components/Messages.tsx](../src/components/Messages.tsx)
- [utils/conversationRecovery.ts](../src/utils/conversationRecovery.ts)

---

## 2. 最通俗的一句话

Claude Code 的 message model，不是“聊天记录格式”。

它更准确地说是：

**把 agent runtime 里发生的所有关键事件，转成一套既能给模型看、也能给 UI 看、还能写进 transcript 供以后恢复的统一语言。**

这里最重要的是“统一语言”。

很多系统失败，不是因为 tool 不够强，而是因为消息建模太弱，最后：

- UI 看不懂
- API 发不稳
- 恢复会话时对不上
- 工具结果和工具调用失配

Claude Code 在这层做得很扎实。

---

## 3. Claude Code 的消息，不只有 user / assistant

如果你从普通聊天产品的视角看，会觉得 message 不就是：

- user
- assistant

但 Claude Code 明显不是这样。

从 [utils/messages.ts](../src/utils/messages.ts) 的 import 可以看出，它处理的消息非常多样：

- `UserMessage`
- `AssistantMessage`
- `AttachmentMessage`
- `ProgressMessage`
- `SystemMessage`
- `SystemCompactBoundaryMessage`
- `SystemStopHookSummaryMessage`
- `ToolUseSummaryMessage`
- 各种 API error / local command / metrics / schedule 等系统消息

这说明 Claude Code 的“消息”不是聊天语义，而是运行时语义。

通俗解释：

在 Claude Code 里，消息更像“系统事件的统一包装”，而不只是你一句我一句的聊天气泡。

---

## 4. 为什么这套模型这么重要

因为 Claude Code 要同时满足三种完全不同的消费方：

1. 模型 API
2. 终端 UI
3. transcript / resume / diagnostics

这三者对消息的要求并不一样。

### 4.1 API 关心

- 结构是否合法
- tool_use / tool_result 是否严格配对
- 多个 user message 是否需要合并
- 某些 display-only 消息是否应该过滤

### 4.2 UI 关心

- 怎么排版
- tool result 应该跟哪条 tool use 放一起
- progress 应该挂在哪
- hook 和 tool 的关系是什么

### 4.3 Resume 关心

- 从日志里读回来还能不能还原出有效会话
- 未完成 tool use 要不要清理
- interrupted turn 要不要继续

Claude Code 的 message model 正是在这三方之间找平衡。

---

## 5. `normalizeMessages()` 是这套系统的第一个关键点

[utils/messages.ts](../src/utils/messages.ts) 里最关键的函数之一就是：

- `normalizeMessages()`

它做的核心事情是：

**把一个 message 里多个 content block 拆成“每块一个消息”的归一化表示。**

这是一个很重要的设计决定。

为什么要这么做？

因为 Claude / tool / thinking 这些内容在原始协议层面，经常是：

- 一条 assistant message 里多个 block
- 一条 user message 里多个 block

但 UI、查找、关联、恢复、重排，很多时候更适合按 block 粒度处理。

所以 Claude Code 把它们拆细了。

通俗解释：

原始消息像一个大包裹，里面塞了好几样东西。

`normalizeMessages()` 会把包裹拆成一个个小盒子，后面系统就更容易处理。

---

## 6. 为什么拆 block 之后还要重新生成 UUID

`normalizeMessages()` 里还有个特别值得学的点：

- `deriveUUID(parentUUID, index)`

也就是：

**当一条消息被拆成多个归一化消息时，后续消息链条要生成新的稳定 ID。**

源码注释写得很明确：

- 如果多 block 消息被拆开
- 后续消息也要进入“新链”
- 否则顺序和唯一性都会出问题

这说明 Claude Code 非常认真地维护“消息链的稳定顺序语义”。

通俗解释：

如果一列火车中间有一节车厢被拆成三节，那后面车厢的编号也不能假装没变。

不然整个队列顺序都会混乱。

---

## 7. `tool_use` 和 `tool_result` 为什么是整套模型的中心

Claude Code 很多设计都围着这一对在转：

- assistant 发出 `tool_use`
- user 侧消息承载 `tool_result`

这点如果第一次看会有些绕，但它非常符合 Anthropic tool calling 协议的世界观：

- 模型“请求调用工具”
- 环境“返回工具结果”

所以从协议上看，`tool_result` 更接近“环境发给模型的用户侧输入”。

Claude Code 完整接受了这个模型。

这就是为什么很多地方都要显式判断：

- `isToolUseRequestMessage(...)`
- `isToolUseResultMessage(...)`

这不是小细节，而是消息系统的脊梁。

---

## 8. `reorderMessagesInUI()` 说明 UI 排序和协议顺序不是一回事

这是非常值得学习的点。

在 [utils/messages.ts](../src/utils/messages.ts) 里有：

- `reorderMessagesInUI(...)`

它会把消息按 `toolUseID` 分组，整理成：

- tool use
- pre hooks
- tool result
- post hooks

这说明 Claude Code 明确区分：

- 运行时/协议层面的原始顺序
- UI 层面最适合用户理解的顺序

这非常成熟。

通俗解释：

后台发生事情的顺序，和你想让用户看懂的顺序，不一定一样。

Claude Code 不会强迫 UI 忠实照抄底层顺序，而是做一次“面向理解的重排”。

很多系统缺的就是这一层。

---

## 9. `buildMessageLookups()` 是性能和关系语义的关键

另一个非常关键的函数是：

- `buildMessageLookups(...)`

它的目标不是“再算一遍消息”，而是：

**把消息关系预编译成很多 O(1) lookup。**

它会构建诸如：

- `toolUseIDsByMessageID`
- `toolUseIDToMessageID`
- `toolUseByToolUseID`
- `siblingToolUseIDs`
- `progressMessagesByToolUseID`
- `toolResultByToolUseID`
- `resolvedToolUseIDs`
- `erroredToolUseIDs`

这说明 Claude Code 并不把“消息渲染”当成无脑遍历。

它已经意识到：

- message 数量会很多
- tool / hook / progress 关系会复杂
- 每次都临时 O(n²) 计算会非常慢

所以它先建 lookup。

这既是性能优化，也是关系建模。

---

## 10. 为什么 message lookup 这么值得借鉴

很多 agent 项目一开始消息少，看不出问题。

等到后面有了：

- 长 transcript
- 多 tool
- progress
- hook
- subagent

就会发现每个组件都在重复做：

- 找配对
- 找兄弟节点
- 找 progress
- 找错误状态

这时 UI 会越来越慢，代码也会越来越乱。

Claude Code 提前把这层抽出来，非常值得学。

通俗解释：

不是每次看档案都从仓库里把所有箱子翻一遍。

而是先建索引。

这就是 `buildMessageLookups()` 的价值。

---

## 11. `normalizeMessagesForAPI()` 说明“给模型看的消息”不是原样 transcript

[utils/messages.ts](../src/utils/messages.ts) 里另一个关键函数：

- `normalizeMessagesForAPI()`

这个函数很重要，因为它清楚说明：

**Claude Code 存在一套“运行时内部消息表示”，以及另一套“真正发给 API 的消息表示”。**

它会做很多事情，比如：

- 重排 attachment
- 过滤 virtual messages
- 去掉不该再发的 synthetic API error
- 合并连续 user messages
- strip 某些坏掉的 document/image block
- 清理 unavailable tool references

这说明 transcript 不是 API payload，二者只是相关，不是同一个东西。

通俗解释：

你家仓库的原始零件，不等于可以直接装车的总成。

`normalizeMessagesForAPI()` 就是最后那道“装配前清洗和拼装”工序。

---

## 12. 为什么 Claude Code 这么在意“过滤掉不该发给 API 的消息”

因为它消息模型里有很多只为本地系统服务的内容，例如：

- progress
- 一些 system message
- 虚拟消息
- synthetic error
- transcript-only 内容

这些如果原样喂给模型，会出现两类问题：

1. token 浪费
2. 语义污染

更严重的是：

3. 某些 API/provider 对 message 格式有严格要求，会直接报错

所以 Claude Code 非常认真地区分：

- 本地运行态消息
- 对模型有效的消息

这是很成熟的工程边界。

---

## 13. `filterUnresolvedToolUses()` 为什么对 resume 特别重要

这个函数是理解 resume 稳定性的关键点之一。

`filterUnresolvedToolUses(messages)` 会：

- 扫描所有 `tool_use` ID
- 扫描所有 `tool_result` ID
- 找出没有结果配对的 `tool_use`
- 过滤掉“完全 unresolved”的 assistant tool_use 消息

源码注释写得很清楚：

- 如果这里用 normalize 过程生成新 UUID，再写回 transcript，resume 会造成指数级膨胀

这说明 Claude Code 对 resume 里的消息污染问题踩过坑，而且修得很仔细。

更重要的是：

**resume 时，半截 tool 调用是很危险的。**

如果不清理：

- API 侧可能出现 orphan tool calls
- UI 侧会显示悬空的工具请求
- 后续推理上下文会被污染

---

## 14. `filterOrphanedThinkingOnlyMessages()` 说明 thinking 也是高风险结构

Claude Code 连 thinking blocks 都做了专门的恢复期过滤。

这说明它知道一件事：

**不是所有“看起来像正常 assistant 输出”的东西都适合直接恢复进会话。**

尤其是 streaming 被打断时，可能会出现：

- 只有 thinking，没有完整配套回答
- message.id 合并不完整
- 中间还插进了用户消息

如果这些东西原样恢复，后面 API 很容易出错。

这就是为什么恢复逻辑要主动删掉某些孤立 thinking 消息。

---

## 15. 这个 message model 的一个核心思想：同一份原始历史，允许多种投影

Claude Code 很强的一点，是它并不执着于“只保留一种 message 形态”。

同一段历史，至少有这几种投影：

- transcript 持久化形态
- normalized UI 形态
- API 发送形态
- resume 修复后形态

这不是重复劳动，而是成熟的分层。

因为不同场景的优化目标不一样：

- transcript 关心保真与可恢复
- UI 关心可读与性能
- API 关心合法与高质量上下文
- resume 关心一致性与纠错

这就是为什么 Claude Code 的消息模型值得学。

---

## 16. 这套消息模型最值得借鉴的地方

### 16.1 把消息看成运行时语义，而不是聊天气泡

一旦你的 agent 系统开始包含：

- tools
- hooks
- progress
- background tasks
- resume

你就不能再把 message 只理解成“谁说了一句话”。

Claude Code 的启发是：

消息是 runtime 的统一事件语言。

### 16.2 允许“内部表示”和“API 表示”分离

这是极其重要的边界。

很多系统把 transcript 直接塞给 API，最后会越来越脆弱。

### 16.3 工具调用必须强配对建模

如果 `tool_use` / `tool_result` 的关系不是 first-class citizen，后面 UI、resume、compact 都会出问题。

### 16.4 关系 lookup 要前置，而不是让 UI 到处自己查

这点特别实用，长会话时差别会非常明显。

### 16.5 恢复期一定要允许“纠偏式过滤”

resume 不是“从盘里读出来就好”。

必须允许系统删掉一些会污染后续运行的半成品消息。

---

## 17. 哪些地方不要机械照抄

### 17.1 小系统不需要一开始就有这么多消息类型

第一版完全可以先有：

- user
- assistant
- tool_use
- tool_result
- system

### 17.2 只有在长会话和复杂 UI 出现后，lookup 才值得系统化

消息少的时候，简单遍历没问题。

### 17.3 normalize 和 transcript 之间的边界一定要想清楚

Claude Code 为了避免 resume 膨胀，对 UUID 和持久化非常谨慎。

这块如果没想清楚，后面很容易出诡异 bug。

---

## 18. 一段最通俗的总结

Claude Code 的 message model，本质上不是“消息格式定义”。

它更像：

**给 agent runtime 设计的一套统一事件语义层。**

有了这层，Claude Code 才能把：

- 工具调用
- 工具结果
- 进度
- 系统边界
- 附件
- 恢复逻辑
- UI 展示

都挂到同一个骨架上。

所以这层特别值得学。

因为它决定了你后面能不能把系统越做越复杂，而不会从根上散掉。

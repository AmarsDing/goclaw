# Claude Code Prompt System Design Guide

日期：2026-04-08

## 这篇文档想解决什么问题

很多人第一次看 Claude Code，会以为它的 prompt 设计大概就是：

- 一个 system prompt
- 一个 user prompt
- 再加一点工具说明

但实际不是。

Claude Code 的 prompt 系统，更像一个“小型 prompt runtime”。

它不是在某个地方写死一整段大 prompt，而是按层次、按模式、按缓存策略、按会话状态，把 prompt 逐段拼起来。

如果你想真正学到东西，这篇文档最值得你带走的是三件事：

1. Claude Code 的 prompt 不是“一段字符串”，而是“分层结构”
2. prompt 设计和缓存设计是绑在一起的
3. prompt 不是单独工作的，它和 agent 模式、tool 权限、memory、MCP、session 状态一起组成系统

## 一句话总览

Claude Code 的 prompt 系统可以理解成 5 层：

1. 默认系统人格层
2. 任务执行规范层
3. 动态上下文层
4. 模式增强层
5. agent / custom override 层

主链路大概是：

```text
getSystemPrompt()
  -> 生成 default system prompt 分段
  -> 插入 dynamic boundary
  -> 拼接动态 section

fetchSystemPromptParts()
  -> 取 defaultSystemPrompt + userContext + systemContext

buildEffectiveSystemPrompt()
  -> 根据 agent / proactive / coordinator / custom prompt 决定最终覆盖或追加策略

QueryEngine / REPL
  -> 把 systemPrompt + userContext + systemContext 送进 query()
```

## 先看核心文件

最关键的几个文件是：

- [prompts.ts](../src/constants/prompts.ts)
- [systemPrompt.ts](../src/utils/systemPrompt.ts)
- [systemPromptSections.ts](../src/constants/systemPromptSections.ts)
- [queryContext.ts](../src/utils/queryContext.ts)
- [QueryEngine.ts](../src/QueryEngine.ts)

## 1. Prompt 在 Claude Code 里不是“一整段字符串”

Claude Code 最重要的设计之一，是把 prompt 拆成很多 section。

在 [prompts.ts](../src/constants/prompts.ts) 里你会看到很多这种函数：

- `getSimpleIntroSection()`
- `getSimpleSystemSection()`
- `getSimpleDoingTasksSection()`
- `getActionsSection()`
- `getUsingYourToolsSection()`
- `getOutputEfficiencySection()`
- `getLanguageSection()`
- `getOutputStyleSection()`
- `getMcpInstructionsSection()`
- `getProactiveSection()`
- `getBriefSection()`

这说明 Claude Code 的思路不是：

> 先写一大段 mega prompt，以后有需求就不断往里面追加

而是：

> 先把 system prompt 拆成可组合、可缓存、可替换的 section

### 白话解释

这有点像搭积木。

不是直接拿一整块混凝土浇出来，而是先拆成：

- 自我介绍模块
- 工具规则模块
- 风格模块
- 风险提示模块
- 模式模块

这样后面才好做：

- 替换
- 复用
- 缓存
- 条件拼装

## 2. `getSystemPrompt()` 是默认 prompt 的核心入口

入口函数在：

- [prompts.ts](../src/constants/prompts.ts#L444)

它的作用是：

> 生成“默认系统 prompt 数组”

注意，不是单个字符串，而是 `string[]`。

### 它大致做了什么

#### 第一种路径：`CLAUDE_CODE_SIMPLE`

如果是简单模式，它直接返回一个极简 prompt：

- 你是谁
- 当前工作目录
- 当前日期

这是最轻量路径。

#### 第二种路径：Proactive / Kairos

如果处于 proactive/Kairos，会走一条更“自治 agent 化”的 prompt 路径：

- autonomous agent 身份
- system reminders
- memory prompt
- 环境信息
- language
- MCP instructions
- scratchpad
- function result clearing
- summarize tool results
- proactive section

这条路径明显不是普通 CLI 问答，而是“自动 agent 模式”。

#### 第三种路径：普通默认模式

普通模式则会：

1. 先生成一组 static sections
2. 再生成一组 dynamic sections
3. 中间插入一个 boundary marker

这是 Claude Code prompt 系统最值得学的工程设计之一。

## 3. 最重要的设计：Static / Dynamic Prompt Boundary

Claude Code 有一个非常关键的常量：

- `SYSTEM_PROMPT_DYNAMIC_BOUNDARY`

定义在：

- [prompts.ts](../src/constants/prompts.ts#L114)

注释写得很清楚：

- boundary 前面的内容可以用 global cache
- boundary 后面的内容是用户/会话相关动态内容

### 为什么这很高级

很多人写 prompt 系统时，只考虑“内容对不对”，不考虑“缓存效率”。

Claude Code 则明确把 prompt 分成：

- 可全局缓存部分
- 动态变化部分

也就是说，它不是单纯在做 prompt engineering，而是在做：

> prompt architecture + cache architecture

### 白话解释

想象一下：

- 有些规则每次都一样，比如“你是 Claude Code”“要小心风险”“尽量简洁”
- 有些规则每轮都可能变，比如“当前有哪些 MCP server”“当前语言偏好”“当前 memory”

如果把这些全部混在一起，每轮都会破坏缓存。

Claude Code 的做法相当于：

> 把“不变的大头”冻起来，只让“会变的小尾巴”更新

这会直接影响：

- 延迟
- token 成本
- cache hit rate

## 4. `systemPromptSection()`：把 prompt section 做成可缓存对象

`systemPromptSection()` 定义在：

- [systemPromptSections.ts](../src/constants/systemPromptSections.ts#L20)

它的作用很简单，但很关键：

> 用 name + compute 封装一个可 memoize 的 prompt section

还有一个危险版：

- `DANGEROUS_uncachedSystemPromptSection()`

意思是：

> 这个 section 每轮都可能变，允许打破 prompt cache，但你必须显式承认它危险

### 这个设计为什么很值钱

因为它把“prompt 是否稳定”变成了显式工程概念。

很多系统默认都是：

- 想加一句就加一句
- 想查点状态就查点状态

最后结果是 prompt 缓存完全打碎。

Claude Code 则反过来要求你：

- 默认 section 都应该可缓存
- 只有真的必要，才允许 uncached

### 白话解释

这像前端性能优化里区分：

- 静态资源
- 动态资源

Claude Code 相当于对 prompt 也做了这种工程化区分。

## 5. `resolveSystemPromptSections()`：不是只拼，还会缓存

函数在：

- [systemPromptSections.ts](../src/constants/systemPromptSections.ts#L43)

逻辑很清楚：

1. 读 section cache
2. 如果这个 section 不是 cacheBreak，而且缓存里有，直接复用
3. 否则执行 compute
4. 再写回缓存

这说明 prompt section 在 Claude Code 里不是“临时字符串”，而是“带生命周期的对象”。

### 可借鉴点

如果你以后做自己的 agent 系统，我非常建议借鉴这个设计：

1. 不要把 system prompt 当成一条字符串
2. 把 prompt 拆成 section registry
3. 给 section 显式命名
4. 给每个 section 加缓存策略

这样你后面想做：

- 模式切换
- 动态工具
- 用户偏好注入
- 远程配置下发

都会容易很多。

## 6. `getSystemPrompt()` 里到底有哪些 section

这是理解 Claude Code 行为的重点。

### 6.1 静态部分

在普通模式里，boundary 前主要有这些：

- intro
- system
- doing tasks
- actions
- using your tools
- tone and style
- output efficiency

它们大多是“长期稳定的行为约束”。

### 6.2 动态部分

boundary 后面主要是：

- session guidance
- memory
- ant model override
- env info
- language
- output style
- MCP instructions
- scratchpad
- function result clearing
- summarize tool results
- numeric length anchors
- token budget
- brief

这些更接近“当前运行时状态”。

### 白话解释

静态部分像“公司员工手册”。  
动态部分像“今天这班车的运行通知”。

## 7. Prompt 系统和模式切换是绑在一起的

Claude Code 不是只有一套 prompt。

它至少有这些模式分流：

- simple mode
- default mode
- proactive mode
- Kairos assistant mode
- coordinator mode
- agent mode
- custom system prompt mode

也就是说：

> prompt 不是内容文件，而是 runtime mode 的体现

## 8. `buildEffectiveSystemPrompt()`：决定谁覆盖谁

这个函数在：

- [systemPrompt.ts](../src/utils/systemPrompt.ts#L41)

它解决的问题是：

> 现在系统里可能同时有 default prompt、agent prompt、custom prompt、append prompt、coordinator prompt，到底谁覆盖谁？

### 它的优先级大致是

1. overrideSystemPrompt
2. coordinator system prompt
3. agent system prompt
4. custom system prompt
5. default system prompt
6. appendSystemPrompt 最后追加

但里面有一个很高级的例外：

### 在 proactive 模式下，agent prompt 不再替换 default，而是追加

代码注释写得很清楚：

- proactive 模式下，默认 prompt 已经包含 autonomous agent 身份
- agent prompt 应该是在这个基础上再叠加 domain-specific instructions

所以它不是：

- default vs agent 二选一

而是：

- default autonomous runtime
- 再加 agent domain specialization

### 白话解释

这相当于：

- 普通模式：你换了岗位说明书，就直接替换
- 主动模式：先保留“自动驾驶规范”，再附加“你是财务助理/代码审查员/探索 agent”

## 9. `fetchSystemPromptParts()`：把 prompt 和 context 分离

这个函数在：

- [queryContext.ts](../src/utils/queryContext.ts#L44)

它会返回三部分：

- `defaultSystemPrompt`
- `userContext`
- `systemContext`

### 为什么要拆成三份

这其实非常关键。

如果把所有信息都塞进 system prompt，会带来几个问题：

1. 太难管理
2. 变化粒度太粗
3. cache 不容易复用
4. 不同来源信息混在一起

Claude Code 的做法是：

- system prompt：长期行为规则
- user context：更偏用户侧、会话侧补充信息
- system context：更偏系统运行时补充信息

### 白话解释

system prompt 是“宪法”。  
user/system context 更像“这次案件的卷宗”和“当前系统状态说明”。

## 10. Prompt 不只是说明身份，还规定工作风格

很多人会把 prompt 只理解成“让模型扮演某个角色”。

Claude Code 明显更进一步，它的 prompt 里还深度规定了：

- 怎么做任务
- 怎么做代码修改
- 怎么对待风险操作
- 怎么对待 hooks
- 怎么看待 system reminders
- 输出长度
- 是否要主动
- 是否允许等待
- 是否要使用特定工具

例如在 `getSimpleDoingTasksSection()` 里，能看到大量工程化规则：

- 不要过度抽象
- 不要乱加 comments
- 要忠实报告测试结果
- 要优先做安全正确的代码

### 可借鉴点

如果你以后设计 coding agent，不要只写这种 prompt：

> 你是一个乐于助人的编程助手

而要像 Claude Code 这样，把“工程行为准则”写清楚：

- 改动边界
- 风险偏好
- 验证标准
- 报告口径
- 工具使用规范

## 11. Prompt 系统和工具系统是耦合的

Claude Code 的 prompt 不是脱离工具存在的。

它会根据当前 enabled tools 来生成一些 section，比如：

- using your tools
- session guidance
- MCP instructions
- brief/proactive instructions

换句话说：

> Prompt 在这里不是模型自言自语，而是工具运行时契约的一部分

### 白话解释

工具系统像硬件接口。  
prompt 系统像操作手册。  
Claude Code 的聪明之处在于：操作手册会跟着硬件接口动态更新。

## 12. Prompt 系统和 memory 系统也深度耦合

`getSystemPrompt()` 会注入：

- `loadMemoryPrompt()`

这说明 Claude Code 不是把 memory 当成外挂，而是把“记忆怎么用”直接注入系统 prompt。

### 为什么重要

因为 memory 并不是：

- 只要把文件放那儿，模型就自然会用

实际上还需要 prompt 告诉模型：

- 记忆放哪
- 怎么写
- 什么该记
- 什么不该记

这也是为什么 Claude Code 的 memory 系统更像 runtime contract，而不是简单向量检索。

## 13. Prompt 系统和 MCP / 外部能力也是动态绑定的

在动态 section 里有：

- `mcp_instructions`

而且它被显式标为：

- `DANGEROUS_uncachedSystemPromptSection`

原因也写得很清楚：

- MCP server 会在会话过程中 connect/disconnect

这说明 Claude Code 的 prompt 系统不是“启动时生成一次就结束”，而是要适应运行时能力变化。

### 可借鉴点

如果你以后做 agent 平台，一定要考虑：

- 工具是动态接入的
- 所以 prompt 也必须跟着动态更新

但又不能每次都把整个缓存打碎。  
Claude Code 这套 boundary + section cache 的做法很值得借鉴。

## 14. 为什么 prompt 设计里会出现 `Length limits`

在动态 sections 里还能看到：

- numeric length anchors

这类 section 非常有意思。

它不是定义身份，也不是定义任务，而是在调输出长度和 token 效率。

这说明 Claude Code 团队把 prompt 当成可调 runtime 参数，而不是只当“产品文案”。

### 白话解释

这像给模型加一个“说话节流器”。

不是为了让它更礼貌，而是为了控制：

- token 消耗
- 冗长输出
- 中间过程啰嗦

## 15. `appendSystemPrompt` 和 `customSystemPrompt` 的区别

这是很多人容易混淆的点。

### `customSystemPrompt`

更像替换默认 prompt 的主体。

### `appendSystemPrompt`

更像在最终 prompt 末尾附加新规则。

### 为什么两者都要有

因为两类需求不一样：

- 有时你要彻底换掉默认人格
- 有时你只是要在现有系统上再补一条规则

Claude Code 把这两种能力拆开，是很合理的。

### 白话解释

- `customSystemPrompt`：换整套校规
- `appendSystemPrompt`：在原校规后面加补充通知

## 16. Prompt 设计中最值得你学习的 6 个点

### 1. 把 prompt 拆成 section，而不是一整段

这是最基础也最重要的。

### 2. 给 section 加名字和缓存语义

不只是“内容是什么”，还要知道“是否稳定”。

### 3. 明确区分 static 和 dynamic

这会极大提升 cache 设计质量。

### 4. Prompt 不是孤立层，而是 runtime 层的一部分

它必须和：

- tools
- memory
- permissions
- agent mode
- MCP

一起设计。

### 5. 用 prompt 约束工程行为，而不仅是人格语气

Claude Code 值得学的不是“写得像人”，而是“写得像靠谱工程师”。

### 6. 用 override / append / mode composition，而不是不断拼接脏字符串

这会让系统更可维护。

## 17. 最适合你借鉴到自己项目里的做法

如果你将来自己做 agent 系统，我最建议你抄走这几条：

### A. Prompt section registry

做一个类似：

```ts
type PromptSection = {
  name: string
  compute: () => string | null | Promise<string | null>
  cacheBreak: boolean
}
```

这会比维护大字符串强太多。

### B. Dynamic boundary

在 prompt 结构里明确插入静态/动态边界。

### C. Effective prompt builder

做一个专门负责组合：

- default
- custom
- mode
- agent
- append

的地方，而不是到处 if/else 拼 prompt。

### D. Prompt 和 runtime 联动

别把 prompt 看成配置文件。  
它应该读：

- 当前工具集合
- 当前模式
- 当前权限策略
- 当前 memory 状态

### E. Prompt 也要做性能设计

Claude Code 的优秀之处，是把 prompt 当成性能关键路径的一部分。

## 18. 难点总结，用最白话的话再讲一遍

### 难点 1：为什么 prompt 不是一个字符串

因为系统太复杂了。

一个真正长期运行的 agent，不可能只靠一条静态系统提示词。

### 难点 2：为什么 prompt 还要缓存

因为 prompt 很长，很贵。  
如果每次都变一点点，缓存就废了。

### 难点 3：为什么动态内容不全放 user message

因为有些东西本质上是 system-level contract，不适合降级成普通 user turn。

### 难点 4：为什么 proactive 模式下 agent prompt 要追加而不是替换

因为 proactive 模式本身已经定义了一套自治运行规则，agent prompt 只是再加专业分工。

## 19. 一句话最终总结

Claude Code 的 prompt 系统，本质上不是“写 prompt”，而是：

> 用可缓存、可组合、可模式化的方式，构建一个长期运行的 agent 行为操作系统。

## 源码证据索引

- [prompts.ts](../src/constants/prompts.ts)
- [systemPrompt.ts](../src/utils/systemPrompt.ts)
- [systemPromptSections.ts](../src/constants/systemPromptSections.ts)
- [queryContext.ts](../src/utils/queryContext.ts)
- [QueryEngine.ts](../src/QueryEngine.ts)

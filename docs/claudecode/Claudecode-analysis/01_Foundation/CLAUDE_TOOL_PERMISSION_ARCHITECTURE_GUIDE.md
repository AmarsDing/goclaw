# Claude Code Tool & Permission Architecture Guide

日期：2026-04-08

## 这篇文档想解决什么问题

如果说 Claude Code prompt 系统解决的是“Claude 应该怎么想”，  
那么 tool + permission 系统解决的就是：

> Claude 什么时候可以动手，怎么动手，动手到什么程度，谁来批准，失败了怎么办。

很多 agent demo 对工具系统的理解很浅：

- 模型输出一个工具名
- 程序执行一下
- 返回结果

Claude Code 明显不是这个级别。  
它的工具架构更像：

- 标准化工具协议
- 多阶段权限判断
- hook/classifier/规则/模式共同决策
- 并发与串行编排
- 交互式与 headless 两条权限路径
- 工具执行前后可插 hook

这篇文档会重点帮你看懂：

1. 工具在 Claude Code 里是怎样的一等公民
2. 权限决策为什么是“决策链”，不是“弹窗”
3. `hasPermissionsToUseTool()` 到底做了什么
4. `toolExecution.ts` 为什么是整个 agent runtime 的关键
5. 哪些设计特别值得借鉴

## 一句话总览

Claude Code 的 tool/permission 主链路大概是：

```text
模型输出 tool_use
  -> query.ts 识别 tool_use
  -> toolOrchestration.ts 决定串行/并发
  -> toolExecution.ts 执行单个工具
     -> pre-tool hooks
     -> resolveHookPermissionDecision
     -> canUseTool / hasPermissionsToUseTool
        -> deny / ask / tool.checkPermissions / bypass / allow rules / auto mode
     -> 真正执行 tool.call()
     -> post-tool hooks
     -> 写回 tool_result message
```

## 先看核心文件

最关键的文件有：

- [Tool.ts](../src/Tool.ts)
- [toolExecution.ts](../src/services/tools/toolExecution.ts)
- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)
- [permissions.ts](../src/utils/permissions/permissions.ts)
- [useCanUseTool.tsx](../src/hooks/useCanUseTool.tsx)

## 1. 工具在 Claude Code 里不是普通函数

这是最重要的基础。

在 [Tool.ts](../src/Tool.ts) 里，工具有完整协议定义。

它至少包含这些能力：

- input schema
- output schema
- 是否启用
- 是否只读
- 是否并发安全
- 执行函数 `call()`
- 权限检查
- UI 渲染
- tool result 映射

### 为什么这很重要

因为在 Claude Code 里，工具不是“随手封装的函数库”，而是 agent runtime 的标准能力单元。

白话解释：

普通函数像厨房里一把没贴标签的刀。  
Claude Code 工具像一件登记过的设备：

- 名字是什么
- 输入输出是什么
- 危险不危险
- 可不可以并发用
- 出了问题怎么展示给人看

## 2. `ToolUseContext`：工具不是裸跑的

另一个重要点是 `ToolUseContext`。

它定义在：

- [Tool.ts](../src/Tool.ts#L158)

里面包含很多运行时能力：

- `getAppState()`
- `setAppState()`
- `abortController`
- `readFileState`
- `options.tools`
- `mcpClients`
- `agentDefinitions`
- `messages`
- `handleElicitation`
- `requestPrompt`
- `appendSystemMessage`
- `sendOSNotification`

### 这意味着什么

工具执行时不是只有“输入参数”，而是拿到了一整套运行时环境。

白话解释：

这像手术室。

医生不是只拿一把手术刀进来，还会有：

- 病历
- 护士
- 麻醉师
- 监护仪
- 紧急制动开关

`ToolUseContext` 就是这套配套环境。

## 3. 工具执行主链路：`toolExecution.ts`

单个工具执行的主干在：

- [toolExecution.ts](../src/services/tools/toolExecution.ts)

这个文件非常关键，因为它不只是“执行工具”，而是在做一个完整流程：

1. 校验调用是否合法
2. 预启动 classifier
3. pre-tool hooks
4. 权限决策
5. 真正执行工具
6. post-tool hooks
7. 封装 tool_result
8. 记录 telemetry / tracing / stats

### 白话解释

它像一个“单工具执行管线”，不是一脚油门踩到底。

## 4. 工具执行前为什么先跑 hooks

在 [toolExecution.ts](../src/services/tools/toolExecution.ts#L800) 附近，能看到：

- `runPreToolUseHooks(...)`

这个阶段 hook 可以做很多事：

- 输出 progress
- 直接给 permission result
- 改写输入
- 阻止后续执行
- 增加额外上下文

### 这很高级

它说明 Claude Code 的工具系统不是硬编码死的，而是支持：

> 在真正执行之前，插一个策略层

### 白话解释

像机场安检：

- 有人可以直接放行
- 有人要二次检查
- 有人材料不对就拦下
- 有人要改签

不是所有请求都直接进登机口。

## 5. 权限系统的核心：`hasPermissionsToUseTool()`

入口在：

- [permissions.ts](../src/utils/permissions/permissions.ts#L473)

这个函数非常值得认真学，因为它体现了 Claude Code 的权限哲学：

> 权限不是一个 if 判断，而是一条决策流水线。

### 它的大致流程

先看内层：

- `hasPermissionsToUseToolInner(...)`

定义在：

- [permissions.ts](../src/utils/permissions/permissions.ts#L1158)

流程大概是：

1. 如果请求已中断，直接 abort
2. 检查 deny rule
3. 检查 ask rule
4. 调用 `tool.checkPermissions()`
5. 如果工具自己 deny，返回 deny
6. 如果工具要求交互，即使 bypass 也保留 ask
7. 如果命中内容级 ask rule，保留 ask
8. 如果命中 safetyCheck，保留 ask
9. 再看 mode 是否允许 bypass
10. 再看 tool 是否 always allowed
11. 最后把 passthrough 转成 ask

### 这个顺序很讲究

不是“先看 mode，再看别的”。

而是：

- 某些风险检查是 bypass-immune
- 某些 content-specific ask rule 优先级更高
- 某些 safety check 不能被 shortcut 绕过

这说明 Claude Code 很在意：

> 自动化便利性不能突破安全边界

## 6. 为什么工具自己也能做权限检查

在 `hasPermissionsToUseToolInner()` 里，有一步是：

- `tool.checkPermissions(parsedInput, context)`

这意味着：

> 不只是全局权限系统在判断，具体工具自己也能做内容级权限判断

### 为什么必须这样

因为有些危险性只有工具自己最清楚。

比如：

- Bash 命令里是不是 `rm -rf`
- FileEdit 改的是不是 `.git`、`.claude`、`.vscode`
- 某个路径是不是敏感配置

全局权限系统只能判断“你是不是 Bash”，  
但工具实现层才能判断“这个 Bash 命令具体危险不危险”。

### 白话解释

这就像公司里：

- 门禁系统知道“你能不能进研发楼”
- 但具体实验室还要自己判断“你能不能碰这台机器”

## 7. 权限系统里最值钱的点：决策链不是单点判断

Claude Code 的权限来源至少包括这些：

- mode
- allow rules
- deny rules
- ask rules
- tool.checkPermissions
- hooks
- classifier
- sandbox override
- working dir checks
- safety checks

这意味着 Claude Code 并不是：

> 用户允许了 Bash，所以什么 Bash 都能跑

而是：

> 即使总体允许，具体到某个输入、某个路径、某个上下文，仍然可能 ask/deny

### 可借鉴点

如果你以后做 agent 产品，千万不要只做：

- 全局“允许工具 A”

你更应该做：

- 工具级允许
- 内容级规则
- 模式级快捷路径
- 风险级保底拦截

## 8. `useCanUseTool()`：交互式权限的桥梁

真正把“权限判断”和“UI/交互处理”连起来的是：

- [useCanUseTool.tsx](../src/hooks/useCanUseTool.tsx)

它的角色可以理解成：

> 权限决策调度器

### 它做了什么

1. 先创建 permission context
2. 调 `hasPermissionsToUseTool()`
3. 如果结果是 allow，直接放行
4. 如果是 deny，走拒绝路径
5. 如果是 ask，再按场景分流：
   - coordinator handler
   - swarm worker handler
   - speculative classifier
   - interactive handler

### 为什么不是直接弹窗

因为 ask 之后仍然有很多可能：

- 自动分类器能帮你批
- leader agent 可以替你裁决
- bridge / channel 权限回调可以处理
- 最后才是人类交互弹窗

### 白话解释

这像公司审批流：

- 有些小额支出系统自动通过
- 有些要先走机器人规则
- 有些让主管批
- 最后才到人工审批

不是一上来就打断用户。

## 9. Headless agent 的权限路径和 REPL 不一样

在 [permissions.ts](../src/utils/permissions/permissions.ts#L392) 这里，有专门给 headless/async agent 的逻辑：

- `runPermissionRequestHooksForHeadlessAgent(...)`

为什么要单独搞这条路径？

因为 headless agent 没法随时弹一个交互式权限框。

所以它会：

1. 先让 PermissionRequest hooks 决策
2. 如果 hook 没给结论，再 fallback 到 auto-deny 或别的策略

### 白话解释

有人值班时，你可以问他。  
没人在值班时，系统就不能傻等，必须走预设规则。

## 10. Auto mode 和 classifier 的设计特别值得学

Claude Code 的权限系统还有一层很高级的东西：

- auto mode
- classifier
- speculative classifier check

在 [toolExecution.ts](../src/services/tools/toolExecution.ts#L734) 附近可以看到：

- `startSpeculativeClassifierCheck(...)`

这意味着：

> 在真正需要用户批准之前，系统已经开始偷偷并行跑分类器了

目的就是减少等待时间。

### 这为什么厉害

很多系统是串行的：

1. 发现需要 ask
2. 再启动 classifier
3. 再决定能不能自动批

Claude Code 则提前预判，把 classifier 提前跑起来。

### 白话解释

这像餐厅后厨先预热锅。  
虽然顾客还没点单确认，但一旦确认，马上就能炒，不用从冷锅开始。

## 11. `toolExecution.ts` 不只是“准入”，还负责记录决策来源

在工具执行中，Claude Code 非常重视：

- 这次批准/拒绝到底来自哪里

比如：

- rule
- hook
- classifier
- mode
- user_temporary
- user_permanent
- user_reject
- config

这不仅仅是日志问题，更是产品和 telemetry 问题。

### 为什么重要

因为如果你以后要做：

- auto mode 优化
- 权限 UX 优化
- 安全策略分析
- 企业治理

你必须知道：

> 这次放行到底是规则放的，还是分类器放的，还是人点的

## 12. 并发安全：不是所有工具都能一起跑

Claude Code 在工具编排里非常强调：

- `isConcurrencySafe`

看：

- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts#L86)

它会把工具调用分成两类：

- 并发安全工具
- 必须串行工具

### 为什么这很重要

因为像：

- 读文件
- 搜索

通常可以并发。

但像：

- 编辑文件
- 修改共享状态

就不一定能并发。

### 白话解释

这像多人协作：

- 查资料可以同时查
- 改同一个 Excel 表，最好别几个人同时改

## 13. 流式工具执行为什么有意义

`StreamingToolExecutor` 支持边流边执行工具。

看：

- [StreamingToolExecutor.ts](../src/services/tools/StreamingToolExecutor.ts)

### 可借鉴点

如果你以后做高性能 agent，非常值得借鉴：

- 一边接模型流
- 一边开始执行已经明确的工具
- 并且还能处理中断、错误和回放顺序

这会显著改善 latency。

## 14. 为什么工具执行前后都要有 hooks

Claude Code 不只在权限前有 hook，工具前后都有 hook 体系。

这使得系统具备很强可扩展性，比如：

- 审计
- 安全拦截
- 自动修正输入
- 附加上下文
- 自动打标
- 失败回调

### 可借鉴点

如果你自己做 agent 平台，可以把工具执行生命周期抽成：

1. PreToolUse
2. PermissionRequest
3. ToolExecution
4. PostToolUse
5. FailureHooks

这样平台会非常灵活。

## 15. Safety check 是真正的“硬边界”

在权限逻辑里，有一类很重要的判断：

- `safetyCheck`

代码明确说明：

- 有些安全检查是 bypass-immune
- 即使 bypassPermissions 模式也不能直接跳过

### 为什么这很重要

说明 Claude Code 的设计不是：

> 用户一开最大权限，系统就彻底不管了

而是：

> 有些红线属于系统保底

比如修改敏感目录、危险路径等。

### 白话解释

这像汽车上的安全气囊和刹车系统。  
即使你切到运动模式，也不能把最底线的安全机制拆掉。

## 16. 工具权限系统里最值得学的 8 个设计点

### 1. 权限判断做成决策链，而不是一个开关

这是最重要的。

### 2. 把工具级和内容级权限分开

允许 `Bash` 不等于允许所有 Bash 内容。

### 3. 工具实现自己参与权限判断

因为细粒度风险只有工具自己最清楚。

### 4. UI 处理和底层权限判断分层

`hasPermissionsToUseTool()` 负责“判断”，  
`useCanUseTool()` 负责“怎么交互处理 ask”。

### 5. Headless 路径要单独设计

不能默认所有环境都有交互弹窗。

### 6. 提前启动 classifier，减少等待

这点很工程化，也很值得借鉴。

### 7. 把决策来源打上标签

这对调试、产品、分析都很重要。

### 8. 把安全红线做成 bypass-immune

这是产品从 demo 变成可用系统的关键一步。

## 17. 如果你做自己的 agent，最建议借鉴什么

### A. 工具要有统一协议

最少也要定义：

- name
- input schema
- output schema
- readOnly
- concurrencySafe
- checkPermissions
- call

### B. 权限要做多层次

至少区分：

- mode-based
- rule-based
- content-based
- safety-based
- user-interactive

### C. UI 弹窗不要写死在执行器里

先返回：

- allow
- ask
- deny

再由上层决定怎么处理 ask。

### D. 给 headless agent 单独权限路径

不然一到后台运行就全卡死。

### E. 工具执行生命周期要可插 hook

未来加治理能力时会轻松很多。

## 18. 用最白话的话总结这一整套系统

### 如果只说一句

Claude Code 的工具系统，不是“模型调用外部函数”，而是：

> 一个带协议、带权限、带审批流、带并发控制、带生命周期 hook 的工具运行平台。

### 如果再白话一点

模型像一个新员工。  
工具像公司里的各种系统权限。  
Claude Code 这套架构做的事，就是：

- 员工可以申请用工具
- 系统先查规章
- 再查细则
- 再看是否要经理批准
- 再决定能否自动放行
- 最后记录是谁批准的

它的重点不是“能不能调用工具”，而是“怎么在真实生产环境里放心让 agent 调工具”。

## 19. 一句话最终总结

Claude Code 的 Tool & Permission Architecture，本质上是在做：

> 一套既能让 agent 高效行动，又不至于失控的工程化执行系统。

## 源码证据索引

- [Tool.ts](../src/Tool.ts)
- [toolExecution.ts](../src/services/tools/toolExecution.ts)
- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)
- [StreamingToolExecutor.ts](../src/services/tools/StreamingToolExecutor.ts)
- [permissions.ts](../src/utils/permissions/permissions.ts)
- [useCanUseTool.tsx](../src/hooks/useCanUseTool.tsx)

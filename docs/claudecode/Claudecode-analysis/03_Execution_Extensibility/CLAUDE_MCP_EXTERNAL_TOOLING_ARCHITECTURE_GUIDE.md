# Claude Code MCP / External Tooling Architecture 解读

## 1. 这篇文档在讲什么

Claude Code 里的 MCP，不是“多接了几个外部工具”那么简单。

它实际上是一整套外部能力接入架构，负责：

1. 连接外部 MCP server
2. 把远程工具变成 Claude Code 本地可调用的 Tool
3. 处理认证与重连
4. 处理 elicitation（外部系统向用户要输入）
5. 处理 SDK / 动态配置 / 插件来源的 MCP
6. 让子代理也能安全继承这些能力

如果你想理解 Claude Code 怎么从“本地编码 agent”长成“可连接外部工具生态的平台”，MCP 是最关键的一层之一。

---

## 2. 先给一个总图

MCP 这条链大致是：

1. 系统读取配置、插件、SDK 注入的 MCP server 定义
2. `connectToServer()` 建立连接
3. `fetchToolsForClient()` 拉取 server 暴露的工具
4. 把它们包装成本地 `Tool`
5. 合并进当前 tool pool
6. 当 MCP tool 被调用时，走统一 tool execution 流程
7. 如果 MCP server 需要用户输入，走 elicitation 流
8. 认证过期、连接失败、插件刷新、动态增删时再进行重连和状态同步

关键文件：

- [client.ts](../src/services/mcp/client.ts)
- [print.ts](../src/cli/print.ts)
- [elicitationHandler.ts](../src/services/mcp/elicitationHandler.ts)
- [mcpPluginIntegration.ts](../src/utils/plugins/mcpPluginIntegration.ts)

---

## 3. 先理解一个关键思想：MCP 在 Claude Code 里是“一等工具来源”

Claude Code 并没有把 MCP 视为外挂。

从代码看，MCP 工具在很多地方都是和内建工具并列处理的：

- 都会进入工具池
- 都会被权限系统约束
- 都能被 ToolSearch / 子代理看到
- 都有 UI / SDK / telemetry 接口

这说明 Claude Code 对 MCP 的设计哲学是：

- “外部工具也应成为统一 runtime 的一部分”

而不是：

- “另开一套插件侧通道”

---

## 4. `client.ts`：MCP 客户端总入口

核心文件：

- [client.ts](../src/services/mcp/client.ts)

### 4.1 它负责的不只是连接

它里面可以看到很多职责：

- 构造不同 transport
- 处理 auth
- 处理 OAuth token 刷新
- 处理 session expired
- 拉 tools / prompts / resources
- 包装成 `MCPTool`
- 处理大结果持久化
- 处理认证缓存

所以 `client.ts` 本质上是：

- MCP 连接与能力适配中心

### 4.2 支持的 transport 很多

代码里直接引入了：

- `StdioClientTransport`
- `SSEClientTransport`
- `StreamableHTTPClientTransport`
- `WebSocketTransport`
- `SdkControlClientTransport`

这说明 Claude Code 的 MCP 不是单通道设计，而是：

- 本地进程型
- 远端流式型
- SDK 代理型

都支持。

这非常像平台级架构，而不是“写死一个 MCP 连接器”。

---

## 5. 为什么 MCP 错误要单独分类

在 `client.ts` 里有几个专门错误类型：

- `McpAuthError`
- `McpSessionExpiredError`
- `McpToolCallError`

### 5.1 这说明什么

MCP 错误在 Claude Code 看来不是普通异常。

因为它们携带的是平台语义：

- 是认证坏了
- 还是 session 过期了
- 还是工具本身返回 error result

不同错误需要不同恢复路径。

### 5.2 为什么这很值得借鉴

很多系统把外部工具错误全都包成一个 `"ExternalToolError"`。

这样后面恢复和 UI 处理都很难做。

Claude Code 更成熟的点在于：

- 把错误变成可路由语义

---

## 6. MCP tool 在 Claude Code 里如何变成本地工具

虽然源码分布较散，但主思路很清楚：

1. 连接 MCP server
2. 拉取 server 工具定义
3. 转成本地 `Tool`
4. 工具名通常变成 `mcp__server__tool`

### 6.1 为什么要统一转成 `Tool`

因为这样：

- 后面的权限
- 并发
- transcript
- UI
- 子代理继承

都可以复用原有工具系统。

这比保留一套“MCP 专用调用路径”成熟很多。

### 6.2 `mcpInfo` 设计很重要

在 [Tool.ts](../src/Tool.ts) 里，MCP 工具还会带：

- `mcpInfo: { serverName, toolName }`

这说明 Claude Code 既要：

- 用统一工具名进入本地系统

也要：

- 保留远端真实身份

这是一种“统一内部表示 + 保留外部原语义”的做法。

---

## 7. MCP 工具结果为什么还要考虑持久化和截断

`client.ts` 里有很多和大输出相关的工具函数：

- `persistToolResult`
- `truncateMcpContentIfNeeded`
- `persistBinaryContent`

### 7.1 这说明 MCP 输出被视为高风险大流量来源

因为外部 server 很可能返回：

- 大量文本
- 二进制内容
- 图片
- 很大的 structured content

如果原样塞回上下文：

- token 爆炸
- transcript 变脏
- UI 体验也差

### 7.2 Claude Code 的做法

不是完全不让返回大结果，而是：

- 必要时落盘
- 给模型一个较短预览和说明

这和它整体的 context / tool result budget 策略是一致的。

---

## 8. 认证为什么是 MCP 架构里的大头

### 8.1 `McpAuthError` 不是装饰

很多 MCP server 会涉及：

- OAuth
- claude.ai proxy
- session token

如果这层不做好，MCP 工具再多也不稳。

### 8.2 `createClaudeAiProxyFetch()` 很值得学

它会：

1. 请求前检查并刷新 OAuth token
2. 401 时尝试 `handleOAuth401Error()`
3. 只有 token 真的变了才重试

这个细节很高级，因为它不是盲目双请求重试，而是在区分：

- 真的 token stale
- 还是对端业务上需要重新认证

### 8.3 needs-auth cache 也很实用

代码里有：

- `mcp-needs-auth-cache.json`
- TTL 15 分钟

这表示如果某个 server 刚刚被判定需要认证，不要每次都重新试一遍再失败。

通俗解释：

- 就像系统知道“这扇门刚试过没钥匙”
- 就先别一直撞门

---

## 9. `print.ts` 里的 MCP 层为什么很关键

看：

- [print.ts](../src/cli/print.ts#L1249)

### 9.1 SDK MCP 和动态 MCP 都在这里接上

这里会维护：

- `sdkClients`
- `sdkTools`
- `dynamicMcpState`

说明 Claude Code 允许 MCP 不只来自静态本地配置，还可以来自：

- SDK 注入
- 动态 control message

这非常平台化。

### 9.2 `buildAllTools()` 让 MCP 真正融入主工具池

它最终会把：

- base tools
- sdkTools
- dynamicMcpState.tools

一起合并。

这说明 MCP 不是侧边工具栏，而是真正进入主 agent 可调用工具空间。

---

## 10. elicitation 是 MCP 里最像“产品交互”的部分

这是 MCP 架构里非常值得学习的一部分。

对应：

- [print.ts](../src/cli/print.ts#L1256)
- [elicitationHandler.ts](../src/services/mcp/elicitationHandler.ts)

### 10.1 什么是 elicitation

简单说：

- MCP server 反过来向用户请求输入

例如：

- 需要用户填表单
- 需要用户打开 URL 完成授权
- 需要用户确认额外参数

这和普通工具调用完全不一样。

因为这里不是：

- Claude 调工具

而是：

- 工具运行到一半，又要人参与

### 10.2 Claude Code 怎么处理这个问题

它不是临时弹个框，而是有正式流程：

1. server 发起 `ElicitRequest`
2. 先跑 elicitation hooks
3. hooks 没解决，再进入 UI / SDK 控制流
4. 用户回复后再跑 result hooks
5. 最后回给 MCP server

这说明 Claude Code 把 elicitation 当成：

- 正式协议事件

而不是：

- 某个 MCP 插件自定义弹窗

---

## 11. `registerElicitationHandlers()` 很值得学

在 [print.ts](../src/cli/print.ts#L1263) 里可以看到：

### 11.1 它先排除 SDK MCP

因为 SDK MCP 的 elicitation 走的是另一条控制协议。

这说明 Claude Code 很清楚：

- 同一个能力在不同 transport 上的宿主责任不同

### 11.2 hooks 优先，UI 其次

流程是：

1. 先 `runElicitationHooks(...)`
2. 没有 hook 响应，再 `structuredIO.handleElicitation(...)`
3. 然后再 `runElicitationResultHooks(...)`

这非常成熟，因为它把 elicitation 做成了：

- 可自动化
- 可人工接管
- 可后处理

三层组合。

通俗解释：

- 先看系统规则能不能自动答
- 不行再问人
- 人答完还可以再过一层规则

---

## 12. URL 模式 elicitation 为什么要有 completion notification

在 `print.ts` 和 `elicitationHandler.ts` 里，都能看到：

- `ElicitationCompleteNotificationSchema`

### 12.1 这解决了什么问题

有些 elicitation 不是“用户在 CLI 里填完就结束”，而是：

- 打开浏览器
- 在网页上继续
- server 再异步通知“完成了”

如果没有 completion notification，CLI 端会不知道：

- 这次外部交互到底结束没有

### 12.2 这为什么很重要

因为它说明 Claude Code 不把用户交互限制在：

- 单轮终端输入

而是允许：

- 跨界面、多阶段完成

这很符合真实产品系统。

---

## 13. 插件提供的 MCP server 为什么要“加作用域”

看：

- [mcpPluginIntegration.ts](../src/utils/plugins/mcpPluginIntegration.ts#L341)

### 13.1 `addPluginScopeToServers()`

它会把插件内 MCP server 名字改成：

- `plugin:${pluginName}:${name}`

### 13.2 为什么这招非常好

因为不同插件可能都想定义：

- `slack`
- `browser`
- `jira`

如果不加 scope，冲突会非常严重。

这说明 Claude Code 在插件化 MCP 上不是“先用再说”，而是从一开始就考虑：

- 命名隔离

这是平台设计必备能力。

---

## 14. 插件 MCP 的环境变量解析为什么值得看

`resolvePluginMcpEnvironment()` 非常值得学。

它会处理：

- `${CLAUDE_PLUGIN_ROOT}`
- `${user_config.X}`
- `${VAR}`

而且是按顺序替换：

1. 插件变量
2. 用户配置变量
3. 系统环境变量

### 14.1 为什么这顺序重要

因为它隐含了优先级模型：

- 插件上下文最具体
- 用户配置次之
- 最后才是系统环境

### 14.2 为什么还要逐 server 报错，而不是整体崩

代码里专门做了 per-server try/catch。

理由很现实：

- 一个插件里一个 MCP server 配错
- 不该拖垮所有插件 MCP 加载

这就是很成熟的“局部失败隔离”。

---

## 15. 为什么子代理也能看到 MCP 工具

这个点特别关键。

在 `print.ts` 里，可以看到它把 SDK MCP tools 放进 `appState.mcp.tools`。

这样做的目的是：

- subagents 之后组装工具池时也能看到这些工具

这说明 Claude Code 对外部能力的思路不是：

- 只有主线程能用外部工具

而是：

- 外部能力也应作为 agent runtime 的公共能力池之一

当然，前提仍然受权限和 agent tool filtering 约束。

---

## 16. MCP 在 Claude Code 里到底扮演什么角色

把这些拼起来，你会发现 MCP 在 Claude Code 里有三个角色：

### 16.1 外部工具来源

最直观的角色。

### 16.2 外部交互代理

通过 elicitation，让外部 server 能和用户形成正式交互闭环。

### 16.3 平台扩展总线

通过：

- 配置
- SDK
- 动态控制消息
- 插件

MCP 成为扩展系统能力的统一入口。

这就是为什么 MCP 在 Claude Code 里不是配角。

---

## 17. 最值得借鉴的设计点

### 17.1 外部工具要统一收编进主工具协议

可借鉴点：

- 不要给外部工具另开一套完全独立调用体系

### 17.2 外部认证必须是正式架构的一部分

可借鉴点：

- 401 重试
- token refresh
- needs-auth cache
- session expired 分类

这些都要从一开始设计。

### 17.3 elicitation 要做成协议，不要做成零散弹窗

可借鉴点：

- 请求
- hook
- 用户处理
- completion
- result hook

最好是一条正式链路。

### 17.4 插件化能力必须做命名隔离

可借鉴点：

- 用 scope 解决冲突，而不是靠文档约定不撞名

### 17.5 外部能力也应该能被子代理安全继承

可借鉴点：

- 外部工具如果要进入 agent 平台，最好进入统一工具池和权限系统

### 17.6 一个 server 配错不应拖垮全局

可借鉴点：

- per-server 错误隔离
- 部分成功优先

---

## 18. 难点白话解释

### 18.1 MCP 在 Claude Code 里像什么

白话版：

- 像外接设备总线
- 外面的工具、资源、服务都可以通过这根总线插进 Claude Code

### 18.2 elicitation 到底像什么

白话版：

- 像你让别人帮你办事，办到一半对方反过来问你要个验证码或让你确认信息

### 18.3 为什么 MCP 不只是“多几个工具”

白话版：

- 因为它还有连接、认证、重连、用户输入、插件来源、动态更新这些配套问题

### 18.4 plugin scope 为什么重要

白话版：

- 就像不同公司都可能有一个叫 “slack” 的服务配置
- 不加作用域，名字一下就撞了

### 18.5 needs-auth cache 为什么聪明

白话版：

- 像系统记住“这个门刚试过，没钥匙”
- 先别每次都重复失败

---

## 19. 一句话总结

Claude Code 的 MCP 架构，真正强的地方不在于“接了外部工具”，而在于它把外部工具接入、认证、用户交互、插件集成、动态更新都做成了统一 runtime 的一部分。

这也是为什么它更像一个 agent 平台，而不只是一个本地 CLI。

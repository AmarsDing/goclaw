# API 调用层

## 功能概述

封装 Anthropic SDK 调用 Claude 模型。支持多 Provider（Direct API、AWS Bedrock、GCP Vertex、Azure Foundry）。处理流式传输、指数退避重试、速率限制（429/529）、OAuth 令牌刷新、提示缓存、思考模式、工具 Schema 构建、Beta Headers、Fast Mode、非流式降级。

---

## 从 main 函数入口的实现流程

### 1. API 预连接（init 阶段）

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/utils/apiPreconnect.ts` | `preconnectAnthropicApi()` | `init()` 中调用，预热 TCP 连接以减少首次请求延迟 |

### 2. 客户端创建

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/api/client.ts` | `getAnthropicClient({apiKey, maxRetries, model, source, fetchOverride})` | 根据 provider 创建 Anthropic SDK 客户端实例 |
| `src/utils/model/providers.ts` | `getAPIProvider()` | 判断 API 提供商 (`firstParty` / `bedrock` / `vertex` / `foundry`) |
| `src/utils/model/providers.ts` | `isFirstPartyAnthropicBaseUrl()` | 判断是否第一方 URL |
| `src/utils/proxy.ts` | `getProxyFetchOptions()` | 获取 HTTP 代理配置 |
| `src/utils/http.ts` | `getUserAgent()` | 构建 User-Agent 头 |

### 3. 流式请求（核心路径）

| 文件路径 | 关键函数 | 行号 | 职责 |
|---------|---------|------|------|
| `src/services/api/claude.ts` | `queryModelWithStreaming()` | ~752 | 公开接口，包装 `queryModel()` + VCR 录制 |
| `src/services/api/claude.ts` | `queryModel()` | ~1017 | **核心函数（~2,000 行）**：构建完整请求参数并发起流式调用 |

**`queryModel()` 内部步骤**：

| 步骤 | 操作 | 调用函数 |
|------|------|---------|
| ① | 构建 Beta Headers | `getMergedBetas()` — `src/utils/betas.ts` |
| ② | Advisor 模型配置 | `isAdvisorEnabled()`, `modelSupportsAdvisor()` — `src/utils/advisor.ts` |
| ③ | 工具过滤 & ToolSearch | `isToolSearchEnabled()`, `isDeferredTool()`, `extractDiscoveredToolNames()` |
| ④ | 缓存策略 | `shouldUseGlobalCacheScope()`, `getPromptCachingEnabled()` |
| ⑤ | 构建请求参数 | `buildSystemPromptBlocks()`, `toolToAPISchema()` — `src/utils/api.ts`, `addCacheBreakpoints()`, `configureTaskBudgetParams()` |
| ⑥ | 调用 withRetry | `withRetry(getAnthropicClient, requestFn, retryOptions)` — `src/services/api/withRetry.ts` |
| ⑦ | 发起 HTTP 请求 | `anthropic.beta.messages.create({...params, stream: true}).withResponse()` |
| ⑧ | 消费流式响应 | `for await (part of stream)` — 处理所有 SSE 事件类型 |

**流式事件处理**（`for await (part of stream)` 中）：

| 事件类型 | 处理 | yield 的事件 |
|---------|------|-------------|
| `message_start` | 初始化 usage、partialMessage | — |
| `content_block_start` (text) | 创建文本块 | — |
| `content_block_start` (thinking) | 创建思考块 | — |
| `content_block_start` (tool_use) | 创建工具调用块 | — |
| `content_block_delta` (text_delta) | 累积文本 | `StreamEvent {type: 'stream_delta'}` |
| `content_block_delta` (thinking_delta) | 累积思考 | `StreamEvent {type: 'stream_delta'}` |
| `content_block_delta` (input_json_delta) | 累积工具输入 JSON | `StreamEvent {type: 'stream_delta'}` |
| `content_block_stop` | 完成内容块 | — |
| `message_delta` | 更新 stop_reason、usage | — |
| `message_stop` | 构建完整 AssistantMessage | `yield AssistantMessage` |

### 4. 重试逻辑

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/api/withRetry.ts` | `withRetry(getClient, requestFn, options)` | 异步生成器：管理重试、降级、令牌刷新 |

**重试策略**：

| 错误类型 | 行为 | 最大重试 |
|---------|------|---------|
| 429 Rate Limit | 指数退避（`BASE_DELAY_MS = 500`） | 10 次 |
| 529 Overload | 退避 + 仅前台查询重试 | 3 次 |
| 401 Unauthorized | 刷新 OAuth 令牌 → 重试 | 1 次 |
| 500+ Server Error | 指数退避 | 10 次 |
| 流式失败 | `onStreamingFallback` → 非流式降级 | 1 次 |
| Fast Mode 拒绝 | `handleFastModeRejectedByAPI()` 冷却 | — |

**前台查询源**（允许 529 重试）：
`repl_main_thread`、`sdk`、`agent:*`、`compact`、`hook_agent`、`hook_prompt`、`verification_agent`、`side_question`、`auto_mode`

### 5. 非流式请求

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/api/claude.ts` | `queryModelWithoutStreaming()` | 简单非流式查询 |
| `src/services/api/claude.ts` | `executeNonStreamingRequest()` | 带重试的非流式请求（超时：远程 120s / 本地 300s） |
| `src/services/api/claude.ts` | `queryHaiku()` | 快速 Haiku 查询（标题生成、分类器） |
| `src/services/api/claude.ts` | `queryWithModel()` | 通用模型查询 |
| `src/services/api/claude.ts` | `verifyApiKey()` | API Key 验证 |

### 6. 辅助模块

| 文件路径 | 关键导出 | 职责 |
|---------|---------|------|
| `src/services/api/errors.ts` | `PROMPT_TOO_LONG_ERROR_MESSAGE` | 错误常量 |
| `src/services/api/usage.ts` | 用量追踪 | Token 使用统计 |
| `src/services/api/bootstrap.ts` | `fetchBootstrapData()` | 初始数据获取 |
| `src/services/api/logging.ts` | API 日志 | 请求日志记录 |
| `src/utils/tokens.ts` | `tokenCountWithEstimation()`, `tokenCountFromLastAPIResponse()` | Token 计数 |
| `src/utils/api.ts` | `toolToAPISchema()`, `prependUserContext()`, `appendSystemContext()` | 请求构建辅助 |
| `src/utils/betas.ts` | `getMergedBetas()`, `getModelBetas()` | Beta header 管理 |
| `src/utils/thinking.ts` | `ThinkingConfig` 类型 | 思考模式配置 |
| `src/utils/context.ts` | `getModelMaxOutputTokens()`, `CAPPED_DEFAULT_MAX_TOKENS` | 输出 token 限制 |

---

## API 请求流程图

```
query.ts:queryLoop()
    │ deps.callModel({messages, systemPrompt, tools, signal, options})
    │
    ▼
queryModelWithStreaming()                    ← src/services/api/claude.ts:752
    └── withStreamingVCR() → queryModel()   ← src/services/api/claude.ts:1017
          │
          ├── 构建请求参数
          │     ├── getMergedBetas(model)
          │     ├── 工具过滤 (ToolSearch / deferred)
          │     ├── buildSystemPromptBlocks(systemPrompt)
          │     ├── toolToAPISchema(tools)
          │     ├── addCacheBreakpoints(messages)
          │     └── configureTaskBudgetParams()
          │
          ├── withRetry(getClient, requestFn, retryOptions)
          │     │                            ← src/services/api/withRetry.ts
          │     │
          │     ├── getAnthropicClient()     ← src/services/api/client.ts
          │     │     ├── Direct → new Anthropic(...)
          │     │     ├── Bedrock → new AnthropicBedrock(...)
          │     │     ├── Vertex → new AnthropicVertex(...)
          │     │     └── Foundry → Anthropic + Azure
          │     │
          │     └── requestFn(anthropic):
          │           └── anthropic.beta.messages
          │                 .create({...params, stream: true})
          │                 .withResponse()
          │           → 返回 Stream<BetaRawMessageStreamEvent>
          │
          └── for await (part of stream) {
                switch(part.type) {
                  message_start → 初始化
                  content_block_start → 创建块
                  content_block_delta → yield StreamEvent
                  content_block_stop → 完成块
                  message_delta → 更新 usage/stop_reason
                  message_stop → yield AssistantMessage
                }
              }
```

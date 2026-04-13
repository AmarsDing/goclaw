# SDK 远程桥接（WebSocket / SSE）

网关向客户端推送 **与进程内 Bridge 相同语义** 的事件流：JSON 负载为 `pkg/protocol.EventFrame`（`type` 恒为 `event`）。

## EventFrame 字段

| 字段 | JSON | 说明 |
|------|------|------|
| `type` | string | 固定为协议中的 event 帧类型（见 `pkg/protocol/frames.go` `FrameTypeEvent`）。 |
| `event` | string | 业务事件名（与 `bus.Event.Name` / Bridge 一致）。 |
| `payload` | any | 事件负载（结构因事件而异）。 |
| `seq` | int64 | 可选，顺序号。 |
| `stateVersion` | object | 可选，乐观同步用版本计数。 |

服务端构造：`protocol.NewEvent(eventName, payload)` → `json.Marshal` 后发出。

## Server-Sent Events

- **路径**：`GET /v1/bridge/events`
- **鉴权**：与 MCP 桥一致；若网关配置 `GOCLAW_GATEWAY_TOKEN`，请求需 `Authorization: Bearer <token>`；未配置 token 时行为以网关实现为准（可能拒绝）。
- **响应**：`Content-Type: text/event-stream`，每条事件一行：`data: <JSON>\n\n`
- **心跳**：约 25s 一次注释行 `: ping <unix>\n\n`，客户端应忽略以 `:` 开头的行。
- **客户端**：`pkg/sdk` 的 `SSEClient.StreamEvents` / `StreamEventsWithReconnect` 解析 `data:` 行，将 JSON 交给调用方解码为 `EventFrame` 或自定义结构。

## WebSocket

- 与 UI / 本地 SDK 同源时，路径一般为 **`/ws`**（以网关 `internal/gateway` 注册为准）。
- 帧格式与 Bridge 一致：服务端将 `protocol.EventFrame` 序列化为 JSON 文本帧（具体以 `Server` 与 `pkg/sdk` `Client` 实现为准）。

## 与 Claude Code / IDE 集成注意

- 远程 IDE 仅订阅事件时优先 **SSE**（防火墙、HTTP/2 友好）；需要双向命令时用 **WebSocket**（若网关对命令通道另有路由，以代码为准）。
- 生产环境务必 **TLS** 终端，并保护 gateway token。

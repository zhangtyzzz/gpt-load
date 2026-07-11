# Generic HTTP 与 Hosted MCP 使用指南

Generic HTTP 是 GPT-Load 面向非 LLM 服务的通用多 Key 负载代理。它不理解厂商的
JSON 字段、工具名或协议方法；除显式的凭据替换和用户开启的策略外，代理按 HTTP
语义透明转发。

当前内置 Tavily、Exa、Jina 预设，也可配置其他使用 API Key 的 HTTP 服务。
Tavily/Exa Hosted MCP 只是预填了远程地址与认证 Header 的透明 HTTP 预设，不会让
代理理解 MCP 方法、工具或会话状态。

本文以 `https://gpt-load.example.com` 为部署地址。请替换为你的实际地址，并为
管理端与每个代理分组使用不同的密钥。

> [!NOTE]
> 本指南对应 `v1.6.0-beta.3`。测试部署请备份数据库，并固定精确镜像
> `ghcr.io/zhangtyzzz/gpt-load:v1.6.0-beta.3`，不要依赖会移动的 `beta` 标签。
> 现有 LLM 分组不受影响；Generic HTTP 仅支持 Header 凭据注入。回退到
> `v1.6.0-beta.2` 后数据仍会保留，但该版本不能代理 Generic HTTP 分组；如需完整回滚，
> 请还原升级前的数据库备份。

## 产品边界

默认情况下，Generic HTTP 保留：

- 请求的 method、原始路径、query、body 和端到端 Header；
- 上游响应状态码、响应体、端到端 Header 及多值 Header；
- SSE 等流式响应，不解析或改写业务 payload。

以下内容属于明确的代理策略，而不是默认透明行为：

- **auth**：当前 Generic 分组必须声明 Header 凭据注入。GPT-Load 是多 Key
  负载代理，不是无需上游 Key 的普通反向代理。
- **validation**：可关闭；开启后使用独立探测请求判断 Key 有效、无效或结果未知。
- **failover**：可关闭；只有明确列入 `retry.failover_statuses` 的状态码才进入失败
  策略。自动重放还必须同时满足 `retry.safe_methods`；未配置的状态码原样返回，不
  惩罚 Key，也不重放请求。

代理凭据、配置的上游凭据、Hop-by-Hop Header、安全脱敏和大小限制不属于端到端
透传范围。Generic HTTP 也不会执行模型重定向或 JSON 参数覆盖。

## 预设速查

| 预设              | 默认上游                 | 上游凭据注入                  | 推荐代理路径                    |
| ----------------- | ------------------------ | ----------------------------- | ------------------------------- |
| Tavily HTTP       | `https://api.tavily.com` | `Authorization: Bearer <key>` | 保留 Tavily 原始路径            |
| Exa HTTP          | `https://api.exa.ai`     | `x-api-key: <key>`            | 保留 Exa 原始路径               |
| Jina Reader       | `https://r.jina.ai`      | `Authorization: Bearer <key>` | 保留 Reader 目标 URL 路径       |
| Jina Search       | `https://s.jina.ai`      | `Authorization: Bearer <key>` | 保留搜索 query                  |
| Jina Foundation   | `https://api.jina.ai`    | `Authorization: Bearer <key>` | 保留 `/v1/...` 路径             |
| Tavily Hosted MCP | `https://mcp.tavily.com` | `Authorization: Bearer <key>` | `/proxy/{group}/mcp/`           |
| Exa Hosted MCP    | `https://mcp.exa.ai`     | `x-api-key: <key>`            | `/proxy/{group}/mcp`            |
| 自定义 HTTP       | 用户填写                 | 可配置 Header                 | `/proxy/{group}/{上游原始路径}` |

预设只填充普通分组字段和声明式策略，不解锁隐藏运行时逻辑。可通过认证后的
`GET /api/channel-catalog` 查看当前版本实际提供的预设。

## 创建多 Key 分组

1. 在**密钥管理**中创建 Generic HTTP 分组，选择预设或“自定义 HTTP”。
2. 设置分组名、上游地址和仅用于访问此代理分组的**代理密钥**。
3. 检查 auth Header 名称与前缀。例如 Bearer 的前缀必须包含末尾空格：
   `Bearer `。
4. 保存后进入分组 Key 管理，按每行一个 Key 导入 Tavily、Exa、Jina 等厂商 Key。
5. 使用隔离凭据测试，再把客户端的厂商域名改为 GPT-Load 代理地址。

请求会在可用 Key 中轮询。无效、禁用或处于冷却期的 Key 不会参与选择；一次失败
切换也不会再次选择该请求已经尝试过的 Key。

客户端可以用以下方式提交**代理密钥**：

```text
X-Gpt-Load-Key: <GPT_LOAD_PROXY_KEY>        # 推荐
Authorization: Bearer <GPT_LOAD_PROXY_KEY>
X-Api-Key: <GPT_LOAD_PROXY_KEY>
X-Goog-Api-Key: <GPT_LOAD_PROXY_KEY>
```

代理密钥必须使用 Header。推荐专用的 `X-Gpt-Load-Key`，尤其当上游业务本身也需要
`Authorization` 或 `X-Api-Key` 时，可避免代理认证与端到端认证冲突。专用的
`X-Gpt-Load-Key` 属于控制面 Header，无论其值是否匹配都会在转发前移除；其他兼容
认证 Header 仅在实际匹配代理密钥时移除，未消费的端到端认证 Header 会继续透传。
随后代理再按 auth 策略写入配置的 Header，并注入本次选择的厂商 Key；控制台和文档
不会生成携带真实 Key 的 URL。

## Tavily HTTP

创建 `tavily` 分组并选择 **Tavily HTTP**，导入多个 Tavily Key。Search 请求示例：

```bash
curl https://gpt-load.example.com/proxy/tavily/search \
  -H "Authorization: Bearer ${GPT_LOAD_PROXY_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"query":"MCP Streamable HTTP","max_results":5}'
```

Extract、Usage 等接口沿用同一规则，即只替换厂商 origin 和凭据，method、路径、query
与 body 保持不变：

```bash
curl https://gpt-load.example.com/proxy/tavily/usage \
  -H "Authorization: Bearer ${GPT_LOAD_PROXY_KEY}"
```

## Exa HTTP

创建 `exa` 分组并选择 **Exa HTTP**。可以保留 Exa 客户端原生的 `X-Api-Key`
Header，但值应换成 GPT-Load 代理密钥：

```bash
curl https://gpt-load.example.com/proxy/exa/search \
  -H "X-Api-Key: ${GPT_LOAD_PROXY_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"query":"MCP Streamable HTTP"}'
```

GPT-Load 移除入站 `X-Api-Key` 后，按 Exa 预设写入
`x-api-key: <本次选择的 Exa Key>`。客户端不会获得上游 Key。

## Jina Reader、Search 与 Foundation

建议为三类 Jina 接口分别建组。即使共用一批 Jina Key，独立分组也能分别管理上游、
统计、限额与失败策略。

### Jina Reader

```bash
curl "https://gpt-load.example.com/proxy/jina-reader/https://example.com/article" \
  -H "Authorization: Bearer ${GPT_LOAD_PROXY_KEY}"
```

目标 URL 保留在 Reader 原始路径中，GPT-Load 只去掉 `/proxy/jina-reader` 前缀并拼接
到 `https://r.jina.ai`。

### Jina Search

```bash
curl --get "https://gpt-load.example.com/proxy/jina-search/" \
  -H "Authorization: Bearer ${GPT_LOAD_PROXY_KEY}" \
  --data-urlencode "q=MCP Streamable HTTP"
```

### Jina Foundation

```bash
curl https://gpt-load.example.com/proxy/jina-foundation/v1/embeddings \
  -H "Authorization: Bearer ${GPT_LOAD_PROXY_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"jina-embeddings-v3","input":["hello"]}'
```

使用其他 Jina Foundation 接口时，继续保留厂商文档给出的 method、路径与 payload。

## Tavily 与 Exa Hosted MCP

Hosted MCP 预设面向 MCP Streamable HTTP：

```text
Tavily: https://gpt-load.example.com/proxy/tavily-mcp/mcp/
Exa:    https://gpt-load.example.com/proxy/exa-mcp/mcp
```

通用客户端配置示例：

```json
{
  "mcpServers": {
    "tavily": {
      "type": "streamable-http",
      "url": "https://gpt-load.example.com/proxy/tavily-mcp/mcp/",
      "headers": {
        "X-Gpt-Load-Key": "${GPT_LOAD_PROXY_KEY}"
      }
    },
    "exa": {
      "type": "streamable-http",
      "url": "https://gpt-load.example.com/proxy/exa-mcp/mcp",
      "headers": {
        "X-Gpt-Load-Key": "${GPT_LOAD_PROXY_KEY}"
      }
    }
  }
}
```

不同客户端对 `type`、环境变量替换和 Header 字段的语法不同，应以客户端当前文档为
准。无论具体配置格式如何，客户端都应直接连接 GPT-Load 的 Streamable HTTP URL，
并在每个请求中携带代理密钥。

`v1.6.0-beta.3` 的 Hosted MCP 支持范围是 **stateless Streamable HTTP**。真实 Tavily
验证中服务端明确报告 `stateless`，initialize、工具发现和工具调用均可通过本代理完成。
代理不会保存或解释 `Mcp-Session-Id`，每个请求仍独立参与 Key 轮询。

多 Tavily/Exa Key 的推荐配置是：创建一个普通 Hosted MCP 分组，把同一厂商的所有 Key
导入该分组，然后让客户端直接使用该分组的代理地址。无需为每把 Key 建子分组。

如果某个 MCP 上游要求后续请求固定到创建会话的认证凭据或具体节点，本版本不承诺兼容。
只有拿到真实有状态服务、明确失败模式和端到端测试后，项目才会重新评估通用会话路由；
不会仅因为协议存在可选会话字段就在代理核心中提前增加状态机。届时会先考虑复用现有
Key 亲和、仅支持普通单上游分组的“响应学习 Session ID → Key”方案；只有真实场景还
要求聚合组或多上游固定，才继续评估完整路由映射。

## Generic HTTP 聚合组

Generic HTTP 可以使用聚合组，但所有子组的**规范化 `channel_config` 必须完全一致**。
这包括 auth、validation、stream、retry、限制和 `preset_id`。父聚合组
的 `channel_config` 不应手填，它由子组配置派生。

子组仍可拥有不同的 Key、上游地址和权重。聚合组适合需要隔离资源池、网络出口、地区
或权重的 stateless 服务；仅为同一厂商增加更多 Key 时，优先放进一个普通分组。选中的
子组无可用 Key 或安全重试失败时，代理会排除本次已尝试 Key，并继续检查健康的兄弟子组。

已加入聚合组的子组不能直接修改会改变规范化配置的关键字段。需要调整时，先从父组
移除该子组，完成修改并确保所有候选子组配置一致，再重新加入。

## 自定义 HTTP 配置

在预设中选择**自定义 HTTP**，然后配置：

| 字段                   | 作用与边界                                                                                                   |
| ---------------------- | ------------------------------------------------------------------------------------------------------------ |
| 上游基础地址           | 绝对 `http(s)` URL；不能包含用户名、密码、Query 或 fragment                                                   |
| auth                   | 必填；固定为 Header 注入，可配置 Header 名称与值前缀                                                         |
| validation             | 可关闭；GET/HEAD/POST 探测、独立地址、Header/Body 和状态分类                                                 |
| stream_mode            | `auto`、`never` 或 `always`；长连接在 `auto` 下必须发送 `Accept: text/event-stream`，否则应选择 `always`     |
| retry                  | 允许自动重放的方法，以及显式进入失败/健康策略的 HTTP 状态码                                                  |
| max_request_body_bytes | 默认 16 MiB，范围 1 字节–64 MiB                                                                              |
| max_error_body_bytes   | 默认 64 KiB，范围 1 字节–1 MiB；限制需缓冲、脱敏的错误响应                                                   |

`auto` 仍会根据上游响应媒体类型启用增量 flush，但响应头到达前已经选定请求超时策略；因此，仅靠
响应 `Content-Type` 不能把一个普通请求升级成无总时限的长连接。MCP/SSE 客户端应发送标准
`Accept: text/event-stream`，不能控制客户端请求头时则明确选择 `always`。

后端会拒绝未知字段、非法 Header、未知配置版本和 URL 中的凭据/Query/fragment，避免拼写
错误被静默接受。当前完整 schema 为：

```json
{
  "version": 1,
  "preset_id": "custom",
  "auth": {
    "placement": "header",
    "name": "Authorization",
    "prefix": "Bearer "
  },
  "validation": {
    "enabled": true,
    "base_url": "",
    "method": "GET",
    "path": "/account",
    "headers": { "Accept": "application/json" },
    "body": null,
    "valid_statuses": [200],
    "invalid_statuses": [401, 403]
  },
  "stream_mode": "auto",
  "retry": {
    "safe_methods": ["GET", "HEAD"],
    "failover_statuses": []
  },
  "max_request_body_bytes": 16777216,
  "max_error_body_bytes": 65536
}
```

关闭 validation 时，相关探测字段会被规范化为空。开启后，只有
`valid_statuses`/`invalid_statuses` 中明确列出的结果才会改变校验结论；网络错误、
未列出的状态码和无法安全读取的响应都是“结果未知”，不会惩罚 Key。

## 失败切换的安全边界

Generic HTTP 将“上游返回某个状态”与“请求失败”分开：

- 不在 `failover_statuses` 中的 HTTP 状态，无论 2xx、4xx 还是 5xx，都按透明结果
  返回；代理不执行 Key 健康动作，也不自动重放。
- 只有列入 `failover_statuses` 的 300–599 状态才进入共享错误/Key 健康策略。即使
  当前 method 不在 `safe_methods` 中，配置的状态仍可触发冷却、失败计数或禁用；
  但不会因此自动换 Key 重放。
- `safe_methods` 是**所有自动重放**的统一方法白名单，同时限制传输错误和已配置的
  failover status。只有 method 在该列表、失败类型允许重试、共享错误策略要求换
  Key 且未达到次数上限时，代理才会重放；默认白名单是 GET、HEAD。
- 传输错误不会直接惩罚 Key。POST、PUT、PATCH、DELETE 等不在默认白名单的方法遇到
  连接中断或配置的 failover status 时都不自动重放，因为上游可能已经接受请求。
- `max_retries` 只是次数上限，不能把未声明状态变成失败，也不能绕过
  `safe_methods`。

预设中的认证/额度状态码只代表当前已审查策略。自定义服务只应加入能够确认需要
failover 的状态码；不要把所有 4xx/5xx 一次性加入。

当前预设的 `failover_statuses` 为：Tavily `401, 429, 432, 433`，Exa
`401, 402, 429`，Jina `401, 403, 429`。这些值仍需与共享错误策略共同生效。

## Header、凭据与响应安全

- 推荐用 `X-Gpt-Load-Key` 携带代理密钥。该专用控制面 Header 始终在转发前移除；
  `Authorization`、`X-Api-Key` 等兼容认证 Header 只在实际完成代理认证时移除，未
  消费的端到端 Header 会继续透传。Legacy LLM 渠道仍会移除全部已知认证 Header，
  再由渠道注入当前上游 Key。
- auth Header 由代理管理，普通 Header 规则不能覆盖它，也不能与它同名。
- `Host`、`Content-Length`、Hop-by-Hop Header 和 `Last-Event-Id` 等固定传输 Header
  不能作为 auth、validation 或 Header 规则目标。
- Header 名称必须是合法 HTTP token；Header 值和凭据前缀禁止 CR/LF。
- 上游地址与 validation base URL 不接受 Query；业务 Query 应由客户端请求携带并原样转发。
- Generic HTTP 不跟随上游重定向，防止凭据被带到其他主机。
- auth 只允许 Header 注入；不要把真实 Key 放入 URL 或 Query，以免进入上游、边缘代理或外部访问日志。
- 上游非成功响应在透传前会做限额和已知凭据脱敏；完整 Key 不应出现在请求日志、
  响应、导出或错误信息中。
- 不要把真实 Key 直接写入普通 Header 规则。确需额外厂商认证 Header 时，使用
  `${API_KEY}` 并确认该 Header 不与 auth 冲突。

## stdio 与旧 HTTP+SSE 边界

Tavily/Exa 的本地 `npx`/stdio MCP 包通常由本地子进程直接访问厂商 API，不会自动
读取 GPT-Load Hosted MCP URL，因此仅替换 Key 不能获得 GPT-Load 轮询。

- 客户端原生支持 Streamable HTTP：直接连接本文的 GPT-Load URL。
- 客户端只支持 stdio：需要经过该客户端验证的 stdio ↔ Streamable HTTP 桥接器；
  必须实测 Header、连续调用、流式响应和关闭流程。
- Hosted MCP 预设不把 legacy HTTP+SSE 或 stdio 转换成 Streamable HTTP。旧客户端
  和旧上游若必须使用 HTTP+SSE，只能按它们的真实端点做自定义透明代理并单独验证；
  当前预设不承诺兼容或协议转换。

## 上线检查清单

- 使用隔离数据创建分组，导入至少两个非生产 Key。
- 连续发两次 HTTP 请求，确认 Key 轮询且完整 Key 不进入日志。
- 分别验证一个未配置状态码和一个 `failover_statuses` 状态码：前者透明返回，后者
  才进入错误/Key 健康策略。
- 对同一个 failover status 分别发送白名单内 GET 和白名单外 POST：确认 GET 在错误
  策略允许时可换 Key，POST 仍执行健康动作但不重放。
- 模拟 GET 与 POST 的传输中断，确认同样只有 `safe_methods` 内的方法会重放，且传输
  错误本身不惩罚 Key。
- Hosted MCP 至少执行 initialize、工具发现、连续两次工具调用和关闭；确认客户端
  与上游均按 stateless Streamable HTTP 工作，并且每次请求仍可独立轮询 Key。
- 使用聚合组时，验证所有子组规范化 `channel_config` 完全一致，并确认首选子组无
  可用 Key 或安全失败时能够切换到健康兄弟子组。
- 检查反向代理/CDN 是否保留所需端到端 Header、多值 Header、SSE flush 和原始路径。
- 生产客户端只使用分组代理密钥，不使用管理端 `AUTH_KEY`。

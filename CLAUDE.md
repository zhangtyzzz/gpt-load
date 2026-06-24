# GPT-Load

高性能 AI API 代理网关，支持多渠道、多 Key 轮询、故障转移、Key 亲和性。

## 技术栈

- 后端: Go + Gin + GORM
- 前端: Vue 3 + Naive UI + TypeScript
- 数据库: SQLite (默认) / MySQL / PostgreSQL
- 缓存: Redis (可选) / 内存

## 项目结构

```
internal/
  keypool/        # Key 管理 (provider, affinity, validator)
  proxy/          # 代理服务器核心逻辑
  channel/        # 渠道适配 (openai, anthropic, gemini)
  services/       # 业务服务 (group, key, log)
  handler/        # HTTP 处理器
  models/         # 数据模型
  store/          # 缓存抽象 (redis, memory)
  config/         # 配置管理
web/              # 前端 Vue 项目
```

## 核心概念

- **Group**: 分组，包含多个 Key 和一个渠道类型
- **Key**: API Key，属于某个 Group
- **Channel**: 渠道类型 (openai, anthropic, gemini)
- **Aggregate Group**: 聚合分组，包含多个子分组

## 请求流程

```
请求 → 认证 → 选择分组 → 选择 Key → 转发上游 → 返回响应
                        ↑
                   失败时重试，换 Key
```

## 开发

```bash
# 启动
cp .env.example .env
cd web && npm install && npm run build && cd ..
go run .

# 测试
go test ./internal/...
go build .
```

## 注意事项

- Store 接口支持 Redis 和 Memory 两种实现
- 正则使用 `sync.Map` 缓存编译结果
- Group 配置变更后需调用 `groupManager.Invalidate()` 刷新缓存
- 前端使用 i18n，修改文案需同步更新 `zh-CN.ts`、`en-US.ts`、`ja-JP.ts`

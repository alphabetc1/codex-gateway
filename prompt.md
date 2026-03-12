# Codex Prompt

复制下面整段给 Codex 即可。

```text
你是资深 Go 后端/网络工程师，请在当前仓库中从零实现一个可部署到外网 VPS 的新项目 `claude-gateway`。

项目目标：
在一台外网 VPS 上运行一个“标准显式 forward proxy”中转服务，让本地开发机上的 Claude Code、Codex 以及其他 LLM CLI / API 流量先发到 VPS，再由 VPS 转发到真实目标域名。

先阅读这些参考文件，理解 client 侧常见配置方式，但不要照着它们的 server 形态实现：
- `/sgl-workspace/tools/cc-switch/README.md`
- `/sgl-workspace/tools/cc-switch/src-tauri/src/proxy/providers/claude.rs`
- `/sgl-workspace/tools/cc-switch/src-tauri/src/proxy/providers/codex.rs`
- `/sgl-workspace/tools/cc-switch/src/components/providers/forms/hooks/useBaseUrlState.ts`
- `/sgl-workspace/tools/cc-switch/src/components/providers/forms/hooks/useCodexConfigState.ts`

重要边界：
- 这是标准显式代理，不是厂商 API gateway
- 不要实现 Claude/OpenAI/Gemini 专用业务路由
- 不要注入或代管上游厂商 API key
- 不要把客户端协议翻译成另一套协议
- 客户端自己的厂商 API key 保留在客户端；代理只负责网络转发、访问控制、审计和安全限制
- `cc-switch` 的 server 是应用层 API 中转思路，只能参考其 client 配置生态

技术选型：
- 使用 Go，优先标准库，依赖尽量少
- 产出单二进制服务，适合 Docker 部署
- 如果仓库是空的，直接初始化合理的 Go module

交付优先级分两层。除非被真实阻塞，否则先完整实现 P0，再尽量做 P1。

P0 必须做：
1. 核心代理能力
- 支持普通 HTTP 请求转发
- 支持 HTTPS `CONNECT host:port` 隧道
- 正确处理 HTTP absolute-form
- 正确处理 CONNECT authority-form
- 支持长连接、流式响应、SSE；不要把整个响应读入内存
- 正确处理 hop-by-hop headers
- CONNECT 成功时返回 `200 Connection Established`
- 对普通请求和 CONNECT 都统计双向字节数
- CONNECT 隧道正确处理双向拷贝、半关闭和连接回收，避免 goroutine 泄漏或死锁

2. 安全与访问控制
- 使用 `Proxy-Authorization: Basic ...` 做鉴权
- 认证失败返回 `407 Proxy Authentication Required`
- 同时返回 `Proxy-Authenticate: Basic realm="claude-gateway"`
- 支持多个账号
- 优先支持 bcrypt 哈希口令；不要把明文口令作为唯一推荐方案
- 支持可选源 IP allowlist（CIDR）
- 实现每个源 IP 最大并发连接数限制，超限返回 `429 Too Many Requests`
- 实现出口目标限制：
  - 目标端口 allowlist，默认仅允许 `443`，可配置放开
  - 目标域名 / 域后缀 allowlist，支持精确域名和后缀匹配
- 默认拒绝访问私有/保留地址作为上游目标
- 在 DNS 解析后的 IP 上再次校验，防止域名绕过 SSRF 防护
- 严禁在日志里记录敏感 header 或 body，尤其不要记录 `Authorization`、`Proxy-Authorization`、API key、Cookie

3. 传输安全
- 由于 `Proxy-Authorization: Basic` 会携带可复用凭据，必须保证链路安全
- P0 必须选择并实现一种安全默认方案，并在 README / compose 中体现：
  - 方案 A：代理监听本身支持 TLS
  - 方案 B：代理只监听私网地址，通过 WireGuard / SSH 隧道 / 反向代理私网暴露来使用
- 不要提供“公网明文 HTTP + Basic Auth”作为默认部署示例

4. 健壮性与配置
- 配置项至少包含：
  - proxy listen address / port
  - admin listen address / port
  - server read header timeout
  - server idle timeout
  - upstream dial timeout
  - upstream TLS handshake timeout
  - upstream response header timeout
  - tunnel idle timeout
  - max header bytes
  - source allowlist
  - destination domain / suffix allowlist
  - destination port allowlist
  - max concurrent connections per source IP
  - auth users source
- 启动时做严格配置校验，对不安全或明显错误的配置快速失败
- proxy server 和 admin server 都支持优雅关闭
- 不要用会误伤 SSE / 流式响应 / CONNECT 隧道的通用 `WriteTimeout` 实现方式
- 错误分类至少包括：
  - auth failed
  - source ip denied
  - concurrency limited
  - destination denied
  - upstream dial failed
  - upstream timeout
  - bad request

5. 可观测性
- 提供单独的 admin HTTP server
- admin server 默认仅监听 `127.0.0.1` 或私网地址；如果允许公网访问，必须明确风险
- 提供 `/healthz`，返回 `200 ok`
- 日志必须是结构化 JSON
- 区分 app log 和 access / audit log
- 审计日志至少包含：
  - request_id 或 conn_id
  - 时间
  - 源 IP
  - 认证用户名（如果可安全记录）
  - 目标 host:port
  - 解析后的目标 IP（如果可获得）
  - 请求方法或 CONNECT
  - 代理返回码
  - 上游状态码（如果适用）
  - 上行字节数
  - 下行字节数
  - 耗时
  - 错误类别
  - 关闭原因 / 失败原因

6. 部署与文档
- 提供 `Dockerfile`
- 提供 `docker-compose.yml`
- 提供 `.env.example`
- 提供 `README.md`，至少包括：
  - 项目简介
  - 架构说明
  - 配置项说明
  - 本地运行方式
  - Docker / Docker Compose 启动方式
  - `curl` 验证示例
  - 如何给 Claude Code / Codex / 其他 CLI 配置显式代理的说明
  - TLS / 私网部署建议
  - 目标域名 allowlist 与 SSRF 防护说明
  - 已知限制与安全建议
- 配置以环境变量优先

7. 测试与验收
- 写关键测试，至少覆盖：
  - Basic Auth 解析与鉴权
  - bcrypt 口令校验
  - 源 IP allowlist
  - 每 IP 并发限制
  - 目标端口 / 域名 allowlist
  - 保留地址 / 私有地址拦截（含 DNS 解析后校验）
  - HTTP 转发
  - absolute-form HTTP 请求
  - HTTPS CONNECT 隧道
  - authority-form CONNECT 请求
  - CONNECT 鉴权失败时返回 `407`
  - hop-by-hop headers 不被错误转发
  - CONNECT 半关闭 / 双向拷贝不死锁
  - 敏感 header 不落日志
- 测试尽量本地自洽，优先 `httptest`，不要依赖外部网络
- 验收标准：
  - `go test ./...` 通过
  - 可以成功构建镜像
  - `docker compose up -d --build` 后服务可启动
  - 未带认证时返回 `407`
  - 超过每 IP 并发阈值时稳定返回 `429`
  - 命中目标限制时返回明确错误，建议 `403`
  - `/healthz` 可用
  - 日志是 JSON，且不包含敏感 header/body

P1 可选做：
- `/metrics`（Prometheus 格式）
- 配置热重载或 `SIGHUP` 重载
- 更细粒度限速（令牌桶 / 每用户限制）
- 更丰富的健康信息，如当前配置摘要、启动时间、版本号
- 更完整的审计字段，如 TLS SNI、连接关闭方向
- 出口 DNS 解析缓存
- 更完善的 graceful drain 与活动连接跟踪
- systemd service 示例
- GitHub Actions CI

项目边界：
- v1 不做数据库
- v1 不做管理后台
- v1 不做计费、多租户、复杂限流系统
- v1 不做 HTTPS MITM 解密；HTTPS 只做 CONNECT 隧道
- v1 不做厂商特定协议转换
- v1 不做“客户端发往官方域名，代理改发到第三方 relay 域名”的上游重写系统
- 不要为了“架构漂亮”引入过重框架

建议目录结构（可微调，但保持清晰）：
- `cmd/claude-gateway/main.go`
- `internal/config`
- `internal/proxy`
- `internal/auth`
- `internal/limiter`
- `internal/admin`
- `internal/logging`
- `internal/netutil`

执行方式：
- 先写一个简短计划，然后直接创建代码
- 代码风格偏 production-ready，少做无意义抽象
- 如果某些客户端兼容性无法完全确认，在 README 中明确写出假设与限制，不要硬编
- 如果发现“只配 host 和 apiKey”与“显式代理”概念混淆，请坚持标准显式代理设计，并在 README 中说明：
  - 推荐接入方式是 `HTTP_PROXY` / `HTTPS_PROXY` 或客户端显式代理配置
  - v1 的路由依据应是目标 `host:port` 与代理协议本身，而不是业务 API path

最后输出简洁总结：做了什么、怎么验证、有哪些已知限制。
```

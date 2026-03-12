<div align="center">

# Claude Gateway

轻量、可部署、偏生产使用的显式 Forward Proxy，面向 Claude Code、Codex 及其他支持标准代理的 LLM CLI / SDK。

[English](./README_EN.md)

</div>

## 是什么

`claude-gateway` 是一个标准显式代理，不是厂商 API Gateway。

它做的事情：

- 转发普通 HTTP 请求
- 支持 HTTPS `CONNECT` 隧道
- 做 Basic Auth、源 IP 白名单、目标域名/端口白名单
- 做 DNS 解析后的私网/保留地址拦截
- 输出结构化 JSON 审计日志

它不做的事情：

- 不改写 Anthropic / OpenAI / Gemini 协议
- 不托管上游 API Key
- 不做业务 API path 路由
- 不做 HTTPS MITM

## 适用场景

- 在 VPS 上部署一个受控代理，让本地 Claude Code / Codex 通过 `HTTP_PROXY` / `HTTPS_PROXY` 出网
- 限定只允许访问少量模型域名，例如 `.anthropic.com`、`.openai.com`
- 需要基本审计能力，但不想引入重型网关

## 核心特性

| 能力 | 说明 |
| --- | --- |
| 显式代理 | 支持 HTTP forwarding 与 HTTPS `CONNECT` |
| 安全控制 | Basic Auth、源 IP allowlist、每 IP 并发限制 |
| 出口约束 | 目标域名 / 后缀 / 端口 allowlist |
| SSRF 防护 | DNS 解析后再次校验，默认拒绝私网/保留地址 |
| 可观测性 | JSON app log / audit log、`/healthz`、可选 `/metrics` |
| 部署友好 | 单二进制、Dockerfile、Compose、环境变量配置 |

## 设计原则

- 路由依据是目标 `host:port` 和代理协议本身，不是业务 API path
- 默认部署方式偏保守：建议仅监听 `127.0.0.1`，再通过 SSH / WireGuard / 私网入口访问
- 默认拒绝“公网明文 HTTP + Basic Auth”这类高风险配置

## 快速开始

1. 生成 bcrypt 密码哈希

```bash
docker run --rm caddy:2-alpine caddy hash-password --plaintext 'change-me'
```

2. 创建用户文件 `config/users.txt`

```text
alice:$2a$...
```

3. 复制配置

```bash
cp .env.example .env
```

4. 启动

```bash
docker compose up -d --build
```

## 最小配置

常用配置都在 [.env.example](./.env.example)。

重点变量：

- `PROXY_LISTEN_ADDR` / `PROXY_LISTEN_PORT`
- `ADMIN_LISTEN_ADDR` / `ADMIN_LISTEN_PORT`
- `AUTH_USERS_FILE`
- `DEST_PORT_ALLOWLIST`
- `DEST_HOST_ALLOWLIST`
- `DEST_SUFFIX_ALLOWLIST`
- `SOURCE_ALLOWLIST_CIDRS`
- `MAX_CONNS_PER_IP`
- `PROXY_TLS_ENABLED`

默认推荐：

- 代理监听 `127.0.0.1:8080`
- 管理端监听 `127.0.0.1:9090`
- 客户端通过 SSH 隧道访问 VPS 上的代理

## 客户端接入

推荐接法是标准显式代理：

```bash
export HTTP_PROXY=http://alice:change-me@127.0.0.1:8080
export HTTPS_PROXY=http://alice:change-me@127.0.0.1:8080
```

如果代理运行在远端 VPS，推荐先打隧道：

```bash
ssh -N -L 8080:127.0.0.1:8080 user@your-vps
```

然后本地 CLI 再走 `127.0.0.1:8080`。

不要把 `ANTHROPIC_BASE_URL`、`OPENAI_BASE_URL` 之类的厂商 API 地址直接改成这个服务，除非客户端本身明确支持把它当作普通 HTTP 代理使用。

## 验证

未带认证时应返回 `407`：

```bash
curl -i --proxy http://127.0.0.1:8080 https://api.openai.com/v1/models
```

带认证时：

```bash
curl -i \
  --proxy http://alice:change-me@127.0.0.1:8080 \
  https://api.openai.com/v1/models
```

命中目标限制时通常返回 `403`，超过每 IP 并发限制时返回 `429`。

## 运行与部署

- 本地运行：`go run ./cmd/claude-gateway`
- Docker 镜像：[Dockerfile](./Dockerfile)
- Compose 部署：[docker-compose.yml](./docker-compose.yml)

这是一个单二进制服务，适合直接跑在 Linux VPS 或容器中。

## 安全说明

- 默认不信任 `X-Forwarded-For`
- 默认不允许访问私网/保留地址
- 不记录 `Authorization`、`Proxy-Authorization`、Cookie 或 body
- 支持代理监听 TLS，但更推荐“私网监听 + 隧道接入”

## 当前边界

- 不做配置热重载
- 不做 DNS 缓存
- 不做数据库、管理后台、多租户
- 不做 CONNECT 内部流量解析

## 目录

```text
cmd/claude-gateway
internal/auth
internal/config
internal/netutil
internal/proxy
internal/admin
```


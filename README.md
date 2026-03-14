<div align="center">

# Codex Gateway

🚪 一个给 Codex、Claude Code 这类 CLI 使用的轻量代理。把出网流量统一收口到你自己的 VPS，再用 Basic Auth、目标域名白名单和审计日志做简洁但实用的控制。

[English](./README_EN.md)

</div>

## ⚡ Quick Start

推荐默认架构：VPS 上运行代理，client 端通过本地入口接入；真正的出网、鉴权、目标约束和审计统一留在 VPS 侧。

### 推荐：🤖 让 LLM 代你部署

1. `git clone` 这个仓库
2. 把 [SEND_THIS_TO_LLM.md](./SEND_THIS_TO_LLM.md) 这个文件直接发给 LLM
3. 回答它追问的少量配置问题
4. 它会自己读取仓库里的部署示例，在当前机器上完成服务端部署，并把客户端需要的配置返回给你

### 架构图

![Codex Gateway 架构图](./docs/architecture-cyberpunk-zh.svg)

两种 client 接入方式对应同一套拓扑：LLM CLI 只连接本地代理入口，VPS 侧负责真正的代理转发和控制。

### 1. VPS 上部署服务端

```bash
cp deploy/vps.example.yaml deploy/vps.yaml
```

先改这几项：

- `users[0].password`
- 如果你不想用默认账号，再改 `users[0].username`
- 如果要放行额外域名，再改 `runtime.dest_suffix_allowlist`

执行部署：

```bash
go run ./cmd/codex-gateway deploy vps
systemctl --user status codex-gateway.service --no-pager
```

这会生成 `.env`、`config/users.txt`、本地二进制，并安装对应的 `systemd --user` 服务。

### 2. Client 端选择一种接入方式

两种方式都可以，区别只在于你是手动管理本地 tunnel 和代理环境变量，还是把它们固化成本地脚本。

#### 方式 A：手动打通 tunnel 并设置代理环境变量

先打通到 VPS 的本地隧道：

```bash
ssh -NT \
  -L 127.0.0.1:8080:127.0.0.1:8080 \
  <ssh.user>@<ssh.host>
```

然后在当前 shell 里设置代理环境变量：

```bash
export HTTP_PROXY=http://<proxy.username>:<proxy.password>@127.0.0.1:8080
export HTTPS_PROXY="$HTTP_PROXY"
```

用这种方式时，直接在当前 shell 里启动 client，不需要运行 `deploy client`。

#### 方式 B：生成本地 tunnel + wrapper

如果你想把 SSH 隧道、代理环境变量和启动命令固化成本地脚本，就用这一种：

```bash
cp deploy/client.example.yaml deploy/client.yaml
```

先改这几项：

- `ssh.user`
- `ssh.host`
- `proxy.password` 改成与服务端一致的密码
- 如果你改了用户名，再把 `proxy.username` 一起改掉

执行部署：

```bash
go run ./cmd/codex-gateway deploy client
```

如果只想生成文件，不立即 build / restart：

```bash
go run ./cmd/codex-gateway deploy vps --write-only
go run ./cmd/codex-gateway deploy client --write-only
```

### 3. 开始使用

根据你选择的接入方式启动：

- 方式 A：在已经设置代理环境变量的 shell 里直接运行 `codex`
- 方式 B：通过本地 wrapper 启动 `~/.local/bin/codex-gateway-proxy codex`

## ✨ 核心特性

- 标准显式代理：HTTP forwarding + HTTPS `CONNECT`
- 安全控制：Basic Auth、源 IP allowlist、每 IP 并发限制
- 出口约束：目标 host / suffix / port allowlist
- SSRF 防护：DNS 解析后二次校验，默认拒绝私网和保留地址
- 可观测性：JSON 日志、`/healthz`、可选 `/metrics`
- 部署友好：单二进制、Docker、Compose、YAML 一键部署

## 🔍 和普通 SSH / SOCKS 代理有什么不同

如果你只需要“把流量绕到 VPS 出去”，普通 SSH 隧道或 SOCKS 代理通常已经够用。`codex-gateway` 额外提供的是一层面向 LLM CLI 的代理控制面：

- SSH / SOCKS 负责通道；`codex-gateway` 负责按请求做 Basic Auth、源 IP、并发限制
- 普通代理一般只会转发；`codex-gateway` 还能限制目标 host / suffix / port，只放行你允许的模型服务域名
- 普通代理通常不会在 DNS 解析后再次校验目标地址；`codex-gateway` 默认拒绝解析到私网和保留地址的目标，降低 SSRF 风险
- 普通 SSH 登录日志不等于代理审计；`codex-gateway` 会记录代理用户名、目标地址、状态码、字节数和耗时
- 通过 client wrapper 可以把代理环境变量只注入给指定命令，例如 `codex`，而不是全局污染当前机器上的其他流量

换句话说：SSH 给你的是“安全地到达 VPS”，`codex-gateway` 给你的是“带策略、审计和最小放行原则的出网网关”。

## 🧭 设计原则

- 这是显式代理，不是厂商 API Gateway
- 默认只监听 `127.0.0.1`，推荐通过 SSH / WireGuard / 私网入口访问
- 不做协议改写，不托管上游 API Key，不做 HTTPS MITM
- 默认配置偏保守，先最小放行，再按需扩容

## ⚙️ 完整配置

- 环境变量方式：[.env.example](./.env.example)
- 服务端一键部署 YAML：[deploy/vps.example.yaml](./deploy/vps.example.yaml)
- 客户端一键部署 YAML：[deploy/client.example.yaml](./deploy/client.example.yaml)
- Docker / Compose：[docker-compose.yml](./docker-compose.yml)

<div align="center">

# Codex Gateway

🚪 面向 Codex、Claude Code 等 CLI 的轻量显式代理。把出网统一收口到你的 VPS，并在出口侧做 Basic Auth、目标白名单和审计。

[English](./README_EN.md)

</div>

## ⚡ Quick Start

推荐架构：代理核心跑在 VPS，client 只连本地入口；出网、鉴权、目标限制和审计都留在 VPS。

当前有两种 mode：

- `proxy mode`：标准显式代理用法，适合 `Claude Code/Codex CLI` 和其他支持 `HTTP(S)_PROXY` 的 CLI
- `claude mode`：类似 `cc-gateway` 的集中 OAuth 方案，当前仅支持`Claude Code`

### 适合场景

- 只想让 `codex`、`claude code` 这类命令走代理，不影响机器上的其他程序
- 想把模型访问统一收口到自己的 VPS，并加上白名单和审计
- 想保留 SSH 隧道接入，但比普通 SSH / SOCKS 多一层出口控制
- 想给 `Claude Code` 增加一个带集中 OAuth 的本地入口，同时尽量不改现有显式代理架构

### 推荐：🤖 让 LLM 代你部署

1. `git clone` 这个仓库
2. 把 [SEND_THIS_TO_LLM.md](./SEND_THIS_TO_LLM.md) 这个文件直接发给 LLM
3. 回答它追问的少量配置问题
4. 它会读取仓库里的部署示例，完成服务端部署，并返回客户端配置

### 架构图

![Codex Gateway 架构图](./docs/architecture-cyberpunk-zh.svg)

两条链路共用同一个 VPS 出口控制核心：

- `proxy mode`：CLI 只看到本地 `HTTP_PROXY` / `HTTPS_PROXY`
- `claude mode`：`Claude Code` 只看到本地 `ANTHROPIC_BASE_URL`，本地 `claude-client` 对接 VPS 上的 Claude OAuth broker，请求再通过现有显式代理出网

### 1. VPS 上部署服务端

```bash
cp deploy/vps.example.yaml deploy/vps.yaml
```

先改：

- `users[0].password`
- 不想用默认账号，再改 `users[0].username`
- 要放行额外域名，再改 `runtime.dest_suffix_allowlist`

示例已经包含常见模型服务域名：

- `.claude.ai`
- `.claude.com`
- `.anthropic.com`
- `.openai.com`
- `.openrouter.ai`
- `.chatgpt.com`
- `.github.com`
- `.githubusercontent.com`
- `.githubcopilot.com`
- `.ghcr.io`

默认示例还会通过 `runtime.dest_host_allowlist` 放行精确主机 `storage.googleapis.com`，用于兼容 Claude Code 仍在迁移中的 legacy 下载路径；同时默认放行 Github 常见 API / 下载域族、远程 GitHub MCP 的 `.githubcopilot.com`，以及本地 Docker 方式安装 GitHub MCP 时会用到的 `.ghcr.io`，避免用户还要手动补白名单。

不建议直接把白名单放空或改成近似全放开。更稳的做法是按产品域族放行，比如 Claude 用 `.anthropic.com`、`.claude.com`、`.claude.ai`，再补必要的精确 host。

如果 `Claude Code` 提示无法连接 `platform.claude.com`，说明你当前部署的白名单里还没有放行 `.claude.com`。把它加入 `runtime.dest_suffix_allowlist` 后重新执行部署。

如果你要启用 `claude mode` 的集中 OAuth，再额外改：

- `claude_oauth.enabled: true`
- `claude_oauth.refresh_token`

启用后，VPS 会在 admin listener 上提供一个 Claude OAuth broker。它集中持有 `refresh_token`，本地 `claude-client` 通过 admin tunnel 拉取短期 `access_token`；这部分用法可以看作是对 `cc-gateway` 集中 OAuth 思路的简化复用。

执行部署：

```bash
go run ./cmd/codex-gateway deploy vps
systemctl --user status codex-gateway.service --no-pager
```

如果 VPS 上没有可用的 `systemd --user`，把 `service_scope: system` 写进 `deploy/vps.yaml`，再以 root 重新执行。

这会生成 `.env`、`config/users.txt`、二进制和对应的 `systemd` 服务。

如果你把这个代理给 `codex`、`claude code` 这类会并发开很多 HTTPS 隧道的 CLI 使用，不要把 `runtime.max_conns_per_ip` 设得太低。经验值建议从 `128` 起步；`16` 这类通用代理级别的限制很容易触发 `429`，然后在客户端重试时表现成“特别慢”。

### 2. Client 端选择 mode

先复制示例：

```bash
cp deploy/client.example.yaml deploy/client.yaml
```

至少先改：

- `ssh.user`
- `ssh.host`
- `proxy.password`
- 如果你改了服务端用户名，再把 `proxy.username` 一起改掉

然后根据要启用的 mode 选择：

- `mode: proxy`：旧用法，只生成 tunnel + proxy env + `codex-gateway-proxy`
- `mode: claude`：只生成 Claude rewrite 相关组件
- `mode: both`：同时生成两套入口

`claude mode` 会额外生成：

- admin tunnel service
- 本地 `codex-gateway claude-client` service
- `claude.env`
- `claude-client.yaml`
- `codex-gateway-claude` wrapper

如果你要接多台 VPS，`proxy mode` 可以在 `deploy/client.yaml` 里配置 `endpoints` 做 client 侧 failover。`claude mode` 当前只支持单个 endpoint。

执行部署：

```bash
go run ./cmd/codex-gateway deploy client
```

如果本机不适合 `systemd --user`，也可以改成 `service_scope: system` 后以 root 安装。

如果只想生成文件，不立即 build / restart：

```bash
go run ./cmd/codex-gateway deploy vps --write-only
go run ./cmd/codex-gateway deploy client --write-only
```

默认会写到 `~/.config/codex-gateway/`，其中 `claude mode` 相关文件通常是：

- `~/.config/codex-gateway/claude.env`
- `~/.config/codex-gateway/claude-client.yaml`

### 3. 手动接入

#### 方式 A：手动使用 `proxy mode`

先打通到 VPS 的本地隧道：

```bash
ssh -NT \
  -L 127.0.0.1:8080:127.0.0.1:8080 \
  <ssh.user>@<ssh.host>
```

然后在当前 shell 设置代理环境变量：

```bash
export HTTP_PROXY=http://<proxy.username>:<proxy.password>@127.0.0.1:8080
export HTTPS_PROXY="$HTTP_PROXY"
```

这种方式下，直接在当前 shell 启动 client，无需运行 `deploy client`。

#### 方式 B：手动使用 `claude mode`

`claude mode` 需要两条本地 tunnel：

```bash
ssh -NT \
  -L 127.0.0.1:8080:127.0.0.1:8080 \
  -L 127.0.0.1:19090:127.0.0.1:9090 \
  <ssh.user>@<ssh.host>
```

然后准备 `claude-client.yaml`，或者先用 `deploy client --write-only` 生成一份，再手工启动：

```bash
codex-gateway claude-client -config ~/.config/codex-gateway/claude-client.yaml
```

最后给 `Claude Code` 注入本地入口：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:11443
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
export CLAUDE_CODE_OAUTH_TOKEN=gateway-managed
claude
```

### 4. 动态更新白名单

如果只想热更新白名单、目标端口、源地址白名单或代理认证用户，不想重启代理进程，可以用 `SIGHUP`：

1. 修改 `deploy/vps.yaml`
2. 只重写服务端生成文件：

```bash
go run ./cmd/codex-gateway deploy vps --write-only
```

3. 给正在运行的服务发送 `SIGHUP`：

```bash
systemctl --user kill -s HUP codex-gateway.service
```

如果你用的是 system scope：

```bash
sudo systemctl kill -s HUP codex-gateway.service
```

如果你是手工启动二进制：

```bash
kill -HUP <pid>
```

`SIGHUP` 会重新加载 `.env`、`AUTH_USERS_FILE`、`SOURCE_ALLOWLIST_CIDRS`、`DEST_PORT_ALLOWLIST`、`DEST_HOST_ALLOWLIST`、`DEST_SUFFIX_ALLOWLIST` 和 `ALLOW_PRIVATE_DESTINATIONS`。监听地址、TLS、超时、日志、metrics 等非运行时配置仍然需要 restart 才会生效。

### 5. 开始使用

按 mode 启动：

- `proxy mode`：在已经设置代理环境变量的 shell 里直接运行 `codex`
- `proxy mode` wrapper：通过 `~/.local/bin/codex-gateway-proxy codex`
- `proxy mode` 多入口：`~/.local/bin/codex-gateway-proxy --endpoint backup codex`
- `claude mode` wrapper：通过 `~/.local/bin/codex-gateway-claude claude`
- `claude mode` 手工 sidecar：先启动 `codex-gateway claude-client -config ...`，再运行 `claude`

## 🧬 Claude Mode

这个 mode 是可选的，而且是和现有 `proxy mode` 隔离实现的。旧的显式代理路径不需要改；只有在你明确启用 `claude mode` 时，client 才会多出一个本地 `claude-client`。

它的工作方式是：

- `Claude Code` 请求先到 client 本地 `claude-client`
- `claude-client` 在本地完成 Claude Code 所需的请求 rewrite
- `claude-client` 通过 admin tunnel 从 VPS OAuth broker 拉短期 token
- 真正的上游请求仍然通过本地 proxy tunnel 进入现有 `codex-gateway` 显式代理，再出网到 `api.anthropic.com`

可以把它理解为：`claude mode` 在不改现有显式代理数据面的前提下，提供了一个类似 `cc-gateway` 的集中 OAuth 入口，并顺带处理 Claude Code 需要的那部分 rewrite。

本地 `claude-client` 还提供：

- `/_health`
- `/_verify`

如果你已经习惯了现有 `proxy mode`，可以完全不启用这个能力；如果你需要 `Claude Code` 的集中 OAuth，再切到 `mode: claude` 或 `mode: both`。

## ✨ 核心特性

- 标准显式代理：HTTP forwarding + HTTPS `CONNECT`
- 安全控制：Basic Auth、源 IP allowlist、每 IP 并发限制
- 出口约束：目标 host / suffix / port allowlist
- SSRF 防护：DNS 解析后二次校验，默认拒绝私网和保留地址
- 可观测性：JSON 日志、`/healthz`、可选 `/metrics`
- 多入口接入：`proxy mode` 支持 client 侧多 VPS failover，可按入口切换
- Claude 专用 mode：本地轻量 reverse proxy、集中 OAuth、请求 rewrite
- 部署友好：单二进制、Docker、Compose、YAML 一键部署

## 🔍 和普通 SSH / SOCKS 代理有什么不同

如果你只想把流量绕到 VPS，普通 SSH 隧道或 SOCKS 代理通常就够了。`codex-gateway` 补的是面向 LLM CLI 的控制面：

- SSH / SOCKS 提供通道；`codex-gateway` 增加 Basic Auth、源 IP 和并发控制
- 普通代理主要做转发；`codex-gateway` 还能限制 host / suffix / port，只放行指定模型域名
- 普通代理通常不做 DNS 结果复检；`codex-gateway` 默认拒绝解析到私网或保留地址的目标
- SSH 登录日志不是代理审计；`codex-gateway` 记录用户名、目标、状态码、字节数和耗时
- 内置 client wrapper，可让只有 `codex` 这类指定命令走代理；无需全局设置代理
- 对 `Claude Code`，还可以额外加一层本地 `claude-client`，把集中 OAuth 收进可控链路

换句话说：SSH 解决“怎么到 VPS”；`codex-gateway` 解决“放行什么、如何审计”；`claude mode` 进一步补上了 `Claude Code` 的集中 OAuth。

## 🧭 设计原则

- `proxy mode` 是显式代理，不是通用厂商 API Gateway
- 默认监听 `127.0.0.1`，推荐通过 SSH / WireGuard / 私网入口访问
- 不做 HTTPS MITM
- `claude mode` 只对 `Claude Code` 做本地 reverse proxy 和 rewrite，VPS 只集中管理 OAuth refresh token
- 默认配置保守，先最小放行，再按需扩容

## 🙏 引用与致谢

`claude mode` 的集中 OAuth 设计参考了 [`motiful/cc-gateway`](https://github.com/motiful/cc-gateway)。这里的实现保持了 `codex-gateway` 原有的显式代理数据面，只把 Claude 相关入口做成可选的本地 sidecar。

## ⚙️ 完整配置

- 环境变量方式：[.env.example](./.env.example)
- 服务端一键部署 YAML：[deploy/vps.example.yaml](./deploy/vps.example.yaml)
- 客户端一键部署 YAML：[deploy/client.example.yaml](./deploy/client.example.yaml)
- Docker / Compose：[docker-compose.yml](./docker-compose.yml)

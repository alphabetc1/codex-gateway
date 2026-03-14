<div align="center">

# Codex Gateway

🚪 A lightweight explicit proxy for Claude Code, Codex, and other proxy-capable LLM CLIs. Run it on your own VPS and keep egress centralized under simple, practical control with Basic Auth, destination allowlists, and audit logs.

[简体中文](./README.md)

</div>

## 🤖 Let An LLM Deploy It

If you are using an LLM / agent that can read files, edit files, and run terminal commands, the easiest path is to skip manual YAML editing:

1. `git clone` this repo
2. Send [SEND_THIS_TO_LLM.md](./SEND_THIS_TO_LLM.md) directly to the LLM
3. Answer the small set of follow-up questions it asks
4. Let it read the deploy examples, finish the server deployment on the current machine, and return the client-side config you need

If you already want an agent to carry out the deployment, this path is usually simpler than the manual quick start below.

## ⚡ Quick Start

Recommended default setup: run the proxy on a VPS, then let the client connect through one local entrypoint while egress, auth, destination policy, and audit stay on the VPS.

### Architecture

![Codex Gateway architecture](./docs/architecture-cyberpunk-en.svg)

Both client-side entry modes use the same topology: the LLM CLI only talks to a local proxy endpoint, and the VPS handles the actual proxying and controls.

### 1. Deploy the server on the VPS

```bash
cp deploy/vps.example.yaml deploy/vps.yaml
```

Start with these fields:

- `users[0].password`
- If you do not want the default username, also change `users[0].username`
- If you need extra destinations, extend `runtime.dest_suffix_allowlist`

The default sample already includes the common model service domains:

- `.anthropic.com`
- `.openai.com`
- `.openrouter.ai`
- `.chatgpt.com`

Run the deploy:

```bash
go run ./cmd/codex-gateway deploy vps
systemctl --user status codex-gateway.service --no-pager
```

This writes `.env`, `config/users.txt`, the local binary, and the matching `systemd --user` service.

### 2. Choose One Client-Side Entry Mode

Both modes work. The difference is whether you manage the local tunnel and proxy env vars yourself, or generate local helper scripts for them.

#### Mode A: Open The Tunnel And Set Proxy Env Vars Manually

First open a local tunnel to the VPS:

```bash
ssh -NT \
  -L 127.0.0.1:8080:127.0.0.1:8080 \
  <ssh.user>@<ssh.host>
```

Then set the proxy env vars in your current shell:

```bash
export HTTP_PROXY=http://<proxy.username>:<proxy.password>@127.0.0.1:8080
export HTTPS_PROXY="$HTTP_PROXY"
```

With this mode, start the client directly from that shell; you do not need to run `deploy client`.

#### Mode B: Generate A Local Tunnel + Wrapper

Use this path if you want the SSH tunnel, proxy env vars, and launch command written into local helper files:

```bash
cp deploy/client.example.yaml deploy/client.yaml
```

Start with these fields:

- `ssh.user`
- `ssh.host`
- `proxy.password` to match the server-side password
- If you changed the username, change `proxy.username` too

Run the deploy:

```bash
go run ./cmd/codex-gateway deploy client
```

If you only want to render files without building or restarting services:

```bash
go run ./cmd/codex-gateway deploy vps --write-only
go run ./cmd/codex-gateway deploy client --write-only
```

### 3. Use It

Start according to the mode you chose:

- Mode A: run `codex` directly from the shell where the proxy env vars are set
- Mode B: run it through the local wrapper with `~/.local/bin/codex-gateway-proxy codex`

## ✨ Core Features

- Standard explicit proxy: HTTP forwarding + HTTPS `CONNECT`
- Access control: Basic Auth, source IP allowlists, per-IP concurrency limits
- Egress control: destination host / suffix / port allowlists
- SSRF protection: re-checks DNS results and blocks private or reserved IPs by default
- Observability: JSON logs, `/healthz`, optional `/metrics`
- Simple deployment: single binary, Docker, Compose, and YAML-based one-click deploy

## 🔍 What This Adds Beyond A Plain SSH / SOCKS Proxy

If all you need is “send traffic out through a VPS,” a plain SSH tunnel or SOCKS proxy is often enough. `codex-gateway` adds an LLM-CLI-oriented control layer on top:

- SSH / SOCKS gives you transport; `codex-gateway` adds per-request Basic Auth, source IP checks, and concurrency limits
- A plain proxy usually just forwards traffic; `codex-gateway` can restrict destination host / suffix / port so only approved model-service domains are reachable
- A plain proxy usually does not re-check DNS results; `codex-gateway` blocks destinations that resolve to private or reserved IPs by default to reduce SSRF risk
- SSH login logs are not proxy audit logs; `codex-gateway` records proxy username, destination, statuses, byte counts, and durations
- The client wrapper lets you inject proxy env vars into a specific command such as `codex` instead of setting a machine-wide global proxy

In short: SSH gets traffic safely to the VPS, while `codex-gateway` turns that VPS into an egress gateway with policy, audit, and conservative defaults.

## 🧭 Design Principles

- This is an explicit proxy, not a vendor API gateway
- The safe default is `127.0.0.1` plus SSH / WireGuard / private ingress
- No protocol rewriting, no upstream API key hosting, no HTTPS MITM
- Defaults stay conservative; open up only what you actually need

## ⚙️ Full Config

- Env-based config: [.env.example](./.env.example)
- Server one-click deploy YAML: [deploy/vps.example.yaml](./deploy/vps.example.yaml)
- Client one-click deploy YAML: [deploy/client.example.yaml](./deploy/client.example.yaml)
- Docker / Compose: [docker-compose.yml](./docker-compose.yml)

Most commonly changed fields:

- `users`
- `DEST_SUFFIX_ALLOWLIST` / `runtime.dest_suffix_allowlist`
- `DEST_HOST_ALLOWLIST`
- `DEST_PORT_ALLOWLIST`
- `SOURCE_ALLOWLIST_CIDRS`
- `PROXY_TLS_ENABLED`

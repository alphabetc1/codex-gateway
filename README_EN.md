<div align="center">

# Claude Gateway

A lightweight, production-oriented explicit forward proxy for Claude Code, Codex, and other LLM CLIs or SDKs that support standard proxy settings.

[简体中文](./README.md)

</div>

## What It Is

`claude-gateway` is a standard explicit proxy, not a vendor API gateway.

What it does:

- Forwards regular HTTP requests
- Supports HTTPS `CONNECT` tunnels
- Enforces Basic Auth, source IP allowlists, destination host/port allowlists
- Re-checks DNS results and blocks private or reserved upstream IPs by default
- Emits structured JSON audit logs

What it does not do:

- No Anthropic / OpenAI / Gemini protocol translation
- No upstream API key management
- No business-path routing
- No HTTPS MITM

## Good Fit For

- Running a controlled egress proxy on a VPS for Claude Code / Codex via `HTTP_PROXY` and `HTTPS_PROXY`
- Restricting outbound access to a small set of model domains such as `.anthropic.com`, `.openai.com`, and `.chatgpt.com`
- Adding basic auditing and access control without introducing a heavy gateway stack

## Features

| Capability | Notes |
| --- | --- |
| Explicit proxy | HTTP forwarding and HTTPS `CONNECT` |
| Access control | Basic Auth, source IP allowlist, per-IP concurrency limit |
| Egress policy | Destination host / suffix / port allowlists |
| SSRF protection | DNS resolution is checked again before dialing |
| Observability | JSON app logs / audit logs, `/healthz`, optional `/metrics` |
| Deployment | Single binary, Dockerfile, Compose, env-based config |

## Design

- Routing is based on proxy semantics and destination `host:port`, not business API paths
- The default deployment model is conservative: bind to `127.0.0.1` and reach the proxy through SSH, WireGuard, or another trusted private path
- Public plain HTTP with reusable Basic credentials is rejected by default

## Quick Start

1. Generate a bcrypt password hash

```bash
docker run --rm caddy:2-alpine caddy hash-password --plaintext 'change-me'
```

2. Create `config/users.txt`

```text
alice:$2a$...
```

3. Copy the env template

```bash
cp .env.example .env
```

4. Start the service

```bash
docker compose up -d --build
```

## Minimal Config

See [.env.example](./.env.example) for the full template.

Important variables:

- `PROXY_LISTEN_ADDR` / `PROXY_LISTEN_PORT`
- `ADMIN_LISTEN_ADDR` / `ADMIN_LISTEN_PORT`
- `AUTH_USERS_FILE`
- `DEST_PORT_ALLOWLIST`
- `DEST_HOST_ALLOWLIST`
- `DEST_SUFFIX_ALLOWLIST`
- `SOURCE_ALLOWLIST_CIDRS`
- `MAX_CONNS_PER_IP`
- `PROXY_TLS_ENABLED`

Recommended defaults:

- Proxy on `127.0.0.1:8080`
- Admin on `127.0.0.1:9090`
- Clients connect through an SSH tunnel

## Client Integration

Use standard explicit proxy settings:

```bash
export HTTP_PROXY=http://alice:change-me@127.0.0.1:8080
export HTTPS_PROXY=http://alice:change-me@127.0.0.1:8080
export ALL_PROXY=http://alice:change-me@127.0.0.1:8080
```

For a remote VPS, tunnel it first:

```bash
ssh -N -L 8080:127.0.0.1:8080 user@your-vps
```

Then point local CLI tools at `127.0.0.1:8080`.

The default sample allowlist includes `.chatgpt.com` so Codex-style clients that access the `chatgpt.com` apex or its subdomains work out of the box.

Do not replace `ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL`, or similar vendor API endpoints with this service unless the client is explicitly using it as a normal HTTP proxy.

## Validation

Without auth, the proxy should return `407`:

```bash
curl -i --proxy http://127.0.0.1:8080 https://api.openai.com/v1/models
```

With auth:

```bash
curl -i \
  --proxy http://alice:change-me@127.0.0.1:8080 \
  https://api.openai.com/v1/models
```

Blocked destinations typically return `403`, and per-IP concurrency violations return `429`.

## Run and Deploy

- Local run: `go run ./cmd/claude-gateway`
- Docker image: [Dockerfile](./Dockerfile)
- Compose deployment: [docker-compose.yml](./docker-compose.yml)

This is a single-binary service intended for Linux VPS or container deployment.

## Security Notes

- `X-Forwarded-For` is not trusted by default
- Private and reserved upstream IPs are denied by default
- `Authorization`, `Proxy-Authorization`, cookies, and bodies are not logged
- TLS on the proxy listener is supported, but private-only access is usually the safer default

## Current Scope

- No config hot reload
- No DNS cache
- No database, admin panel, or multi-tenant layer
- No inspection of traffic inside CONNECT tunnels

## Layout

```text
cmd/claude-gateway
internal/auth
internal/config
internal/netutil
internal/proxy
internal/admin
```

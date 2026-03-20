# Codex Gateway Implementation Plan

## 1. Goal and Scope

### 1.1 Goal
Build a production-oriented explicit HTTP forward proxy for deployment on a VPS. Local developer machines will send Claude Code, Codex, and other LLM CLI/API traffic to this proxy, and the proxy will forward traffic to the real upstream domains.

### 1.2 Core Positioning
- This is a standard explicit proxy.
- This is not an Anthropic/OpenAI/Gemini API compatibility gateway.
- The proxy does not own upstream vendor API keys.
- The proxy does not translate one vendor protocol into another.
- Routing decisions are based on proxy protocol semantics and destination `host:port`, not business API paths.

### 1.3 P0 vs P1

#### P0 Must-Have
- HTTP forwarding
- HTTPS CONNECT tunneling
- Basic proxy auth
- Source IP allowlist
- Per-source-IP concurrency limit
- Destination domain/suffix allowlist
- Destination port allowlist
- Private/reserved IP blocking after DNS resolution
- JSON structured logging and audit logging
- `/healthz`
- Dockerfile, docker-compose, `.env.example`
- Core tests

#### P1 Nice-to-Have
- `/metrics`
- Config reload
- DNS cache
- More granular rate limiting
- CI workflow
- Systemd unit examples

## 2. Product Decisions

### 2.1 Security Default
Do not ship a default deployment example that exposes `http://public-ip:port` with Basic auth over the public internet.

Recommended default in v1:
- Support direct TLS on the proxy listener.
- Also allow a private-only deployment mode:
  - proxy listens on private address only
  - user reaches it through WireGuard, SSH tunnel, or a trusted private reverse proxy

### 2.2 Outbound Restriction Strategy
This service must not become a generic open proxy. P0 should enforce:
- Destination port allowlist, default `443`
- Destination hostname exact match allowlist
- Destination suffix allowlist, for example `.claude.ai`, `.claude.com`, `.anthropic.com`, `.openai.com`
- Resolution-time IP blocking for private/reserved IPs

Optional future expansion:
- Separate allowlists for HTTP forwarding vs CONNECT
- Per-user destination policies

### 2.3 Credential Storage
Prefer bcrypt-hashed credentials.

Recommended config shape:
- `AUTH_USERS_FILE=/path/to/users.txt`
- file format: one `username:bcrypt_hash` per line

Optional fallback:
- `AUTH_USERS=user1:$2y$...,user2:$2y$...`

Avoid making plaintext passwords the primary documented approach.

## 3. High-Level Architecture

### 3.1 Processes and Servers
Run a single binary containing:
- proxy server
- admin server

The process should:
- load config
- validate config
- construct shared dependencies
- start proxy server
- start admin server
- wait for signal
- gracefully shut down both servers

### 3.2 Main Components
- `config`: load and validate environment-driven config
- `auth`: parse Proxy-Authorization and validate users
- `limiter`: per-source-IP connection/concurrency tracking
- `netutil`: IP extraction, CIDR matching, reserved-range checks, DNS resolution helpers
- `proxy`: request handling, CONNECT handling, forwarding, bytes accounting
- `admin`: `/healthz`, maybe `/metrics`
- `logging`: JSON logger and audit event helpers

### 3.3 Data Flow

#### HTTP Request Flow
1. Accept TCP connection
2. Parse HTTP request
3. Extract source IP
4. Apply source IP allowlist
5. Apply per-IP concurrency tracking
6. Authenticate `Proxy-Authorization`
7. Validate request form and destination
8. Resolve destination if needed for policy checks
9. Reject if destination not allowed
10. Build outbound request to upstream
11. Remove hop-by-hop headers
12. Stream upstream response back to client
13. Record byte counts, duration, status, result
14. Emit audit log
15. Release concurrency slot

#### CONNECT Flow
1. Accept request `CONNECT host:port`
2. Extract source IP
3. Apply source IP allowlist
4. Apply per-IP concurrency tracking
5. Authenticate `Proxy-Authorization`
6. Parse and validate destination `host:port`
7. Resolve destination host and enforce reserved-IP blocking
8. Dial upstream TCP
9. Return `200 Connection Established`
10. Bidirectionally copy bytes client<->upstream
11. Handle half-close where possible
12. Detect timeout / EOF / reset / shutdown
13. Record byte counts and close reason
14. Emit audit log
15. Release concurrency slot

## 4. Repository Layout

Suggested initial tree:

```text
cmd/codex-gateway/main.go
internal/admin/admin.go
internal/auth/basic.go
internal/auth/store.go
internal/config/config.go
internal/config/validate.go
internal/limiter/concurrency.go
internal/logging/logger.go
internal/logging/audit.go
internal/netutil/ip.go
internal/netutil/dns.go
internal/netutil/allowlist.go
internal/proxy/server.go
internal/proxy/handler.go
internal/proxy/http_forward.go
internal/proxy/connect.go
internal/proxy/hopheaders.go
internal/proxy/copy.go
internal/proxy/context.go
internal/proxy/errors.go
internal/proxy/policy.go
internal/proxy/bytes.go
internal/version/version.go
```

If it helps clarity, split tests alongside packages rather than creating a separate test directory.

## 5. Configuration Design

## 5.1 Environment Variables

Recommended P0 config keys:
- `PROXY_LISTEN_ADDR`
- `PROXY_LISTEN_PORT`
- `ADMIN_LISTEN_ADDR`
- `ADMIN_LISTEN_PORT`
- `PROXY_TLS_ENABLED`
- `PROXY_TLS_CERT_FILE`
- `PROXY_TLS_KEY_FILE`
- `AUTH_USERS_FILE`
- `AUTH_USERS`
- `SOURCE_ALLOWLIST_CIDRS`
- `DEST_PORT_ALLOWLIST`
- `DEST_HOST_ALLOWLIST`
- `DEST_SUFFIX_ALLOWLIST`
- `ALLOW_PRIVATE_DESTINATIONS`
- `MAX_CONNS_PER_IP`
- `SERVER_READ_HEADER_TIMEOUT`
- `SERVER_IDLE_TIMEOUT`
- `UPSTREAM_DIAL_TIMEOUT`
- `UPSTREAM_TLS_HANDSHAKE_TIMEOUT`
- `UPSTREAM_RESPONSE_HEADER_TIMEOUT`
- `TUNNEL_IDLE_TIMEOUT`
- `MAX_HEADER_BYTES`
- `LOG_LEVEL`
- `LOG_FORMAT`
- `ACCESS_LOG_ENABLED`
- `METRICS_ENABLED`

### 5.2 Parsing Conventions
- Comma-separated values for lists
- Durations accepted in Go duration syntax, e.g. `10s`, `1m`
- Booleans accept `true/false`

### 5.3 Validation Rules
- Proxy listener address/port must be valid
- Admin listener address/port must be valid
- If `PROXY_TLS_ENABLED=true`, cert and key must both exist
- At least one credential source must be configured
- `MAX_CONNS_PER_IP` must be positive
- Port allowlist must contain valid TCP ports
- If both host allowlist and suffix allowlist are empty, fail closed unless an explicit override exists
- If admin binds publicly, emit warning or fail unless explicitly acknowledged
- Refuse insecure sample mode where public listen + no TLS + auth over internet is implied

## 6. Authentication Design

### 6.1 Supported Format
Use `Proxy-Authorization: Basic base64(username:password)`.

### 6.2 Behavior
- Missing header: return `407`
- Invalid scheme: return `407`
- Invalid base64: return `407`
- Unknown user: return `407`
- Password mismatch: return `407`
- Always include `Proxy-Authenticate: Basic realm="codex-gateway"`

### 6.3 Auth Store
Implement:
- `UserStore` interface
- file-backed hashed user store
- env-backed hashed user store

Methods:
- `Authenticate(username, password string) (ok bool, err error)`
- `HasUser(username string) bool`

### 6.4 Logging Rules
Never log:
- raw `Proxy-Authorization`
- decoded password
- raw Authorization-like headers

Safe to log:
- auth result category
- username only after successful parse, and only if policy allows

## 7. Source IP Handling

### 7.1 Source IP Extraction
For v1, use the actual TCP remote address from `r.RemoteAddr`.

Do not trust:
- `X-Forwarded-For`
- `Forwarded`

unless a future trusted reverse proxy mode is explicitly added.

### 7.2 Source CIDR Allowlist
Implement a matcher over parsed `netip.Prefix`.

Behavior:
- empty allowlist means allow all
- non-empty allowlist means default deny

## 8. Destination Policy and SSRF Protection

### 8.1 Destination Parsing

#### HTTP Forward
Parse the absolute-form request URL:
- scheme required
- host required
- extract effective port:
  - `80` for `http`
  - `443` for `https`
  - explicit port if present

#### CONNECT
Parse `host:port` strictly.

Reject:
- malformed host
- missing port
- invalid port

### 8.2 Host Allowlist
Two matchers:
- exact host match
- suffix match

Suffix matching rule:
- `.openai.com` matches `api.openai.com`
- exact `openai.com` should not accidentally match `badopenai.com`

### 8.3 Port Allowlist
Default:
- only `443`

Potential future:
- allow `80` for simple HTTP checks

### 8.4 DNS Resolution and Reserved Range Checks
Resolution policy:
1. Resolve destination host to one or more IPs
2. Reject if any selected upstream IP is private/reserved and private destinations are not explicitly allowed

Reserved/private sets should include:
- loopback
- RFC1918 private ranges
- link-local
- CGNAT
- multicast
- unspecified
- documentation/test networks
- IPv6 loopback, ULA, link-local, multicast

### 8.5 Resolution Timing
Apply checks:
- before CONNECT dialing
- before HTTP transport dialing

Implementation options:
- custom `DialContext` that receives resolved IPs
- explicit pre-resolution plus dial by resolved IP while preserving TLS ServerName

Recommended approach:
- pre-resolve host
- pick one allowed IP
- dial allowed IP directly
- preserve host header and TLS SNI using original hostname

## 9. Concurrency Limiter

### 9.1 Requirement
At least enforce max concurrent connections per source IP.

### 9.2 Suggested Design
Use a small in-memory structure:
- map `source_ip -> active_count`
- guarded by mutex

API:
- `Acquire(ip string) bool`
- `Release(ip string)`
- `Current(ip string) int`

Use `defer Release` immediately after successful acquire.

### 9.3 Semantics
Count:
- one HTTP request as one active slot
- one CONNECT tunnel as one active slot for the full tunnel lifetime

This is enough for P0.

## 10. Proxy Server Design

### 10.1 Listener
Use `http.Server` for request parsing and lifecycle management.

For TLS mode:
- either `ListenAndServeTLS`
- or custom `tls.Listener`

### 10.2 Handler Entry Point
Single proxy handler:
- inspect request method
- dispatch to `handleConnect` or `handleForwardHTTP`

### 10.3 Middleware Order
Recommended order:
1. create request/connection context and request ID
2. extract source IP
3. source allowlist check
4. concurrency acquire
5. auth
6. destination parsing and policy checks
7. request execution
8. audit logging
9. concurrency release

## 11. HTTP Forwarding Design

### 11.1 Request Shape
Explicit proxy clients send:
- `GET http://example.com/path HTTP/1.1`

Need to rebuild outbound upstream request:
- method preserved
- URL rewritten for upstream transport
- request body streamed

### 11.2 Header Handling
Remove hop-by-hop headers such as:
- `Connection`
- `Proxy-Connection`
- `Keep-Alive`
- `TE`
- `Trailer`
- `Transfer-Encoding`
- `Upgrade`
- `Proxy-Authenticate`
- `Proxy-Authorization`

Also remove any headers named by the `Connection` header tokens.

### 11.3 Upstream Transport
Create a custom `http.Transport` with:
- custom dialer
- TLS handshake timeout
- response header timeout
- idle connection timeout
- keepalive
- proxy disabled for upstream calls

Important:
- do not accidentally chain to system proxy environment variables
- transport for upstream should be direct from the VPS

### 11.4 Streaming
Use streaming copy:
- upstream response body copied directly to downstream response writer
- do not buffer entire body

If `Flush` is available, flush progressively for SSE-like behavior.

## 12. CONNECT Tunnel Design

### 12.1 Basic Flow
Use `http.Hijacker`:
- hijack client connection
- dial upstream TCP
- write `HTTP/1.1 200 Connection Established\r\n\r\n`
- start bidirectional copy

### 12.2 Copy Strategy
Use two goroutines:
- client -> upstream
- upstream -> client

Need:
- byte counting wrappers
- half-close if supported via `CloseWrite`
- context/cancel coordination
- idle timeout handling

### 12.3 Tunnel Idle Timeout
Set connection deadlines that refresh on successful reads/writes, or implement deadline management in the copy loop.

Need to avoid:
- timing out active tunnels
- leaving idle tunnels forever

## 13. Timeout Strategy

### 13.1 Server Timeouts
Recommended:
- `ReadHeaderTimeout` enabled
- `IdleTimeout` enabled
- avoid generic `WriteTimeout` for tunnel/stream safety

### 13.2 Upstream HTTP Timeouts
- dial timeout
- TLS handshake timeout
- response header timeout

### 13.3 CONNECT Tunnel Timeout
- idle timeout only
- no absolute timeout in P0 unless explicitly configured

## 14. Logging and Audit Design

### 14.1 Logger
Use JSON logger. Standard library `log/slog` is sufficient.

Two channels of logs:
- application log
- access/audit log

### 14.2 Audit Schema
Suggested fields:
- `ts`
- `level`
- `log_type=access`
- `request_id`
- `conn_id`
- `source_ip`
- `username`
- `method`
- `target_host`
- `target_port`
- `resolved_ip`
- `proxy_status`
- `upstream_status`
- `bytes_up`
- `bytes_down`
- `duration_ms`
- `result`
- `error_category`
- `close_reason`

### 14.3 Redaction Rules
Never log:
- header values for auth/cookie/api-key-like fields
- request body
- response body

If logging headers at debug level in the future, redact by denylist.

## 15. Admin Server

### 15.1 P0 Endpoints
- `GET /healthz`

Response:
- `200 OK`
- simple JSON or plain text acceptable

Suggested JSON:

```json
{"status":"ok"}
```

### 15.2 P1 Endpoint
- `GET /metrics`

### 15.3 Binding Strategy
Default:
- `127.0.0.1`

If configured publicly:
- document risk loudly
- optionally add independent auth later

## 16. Docker and Deployment

### 16.1 Dockerfile
Use multi-stage build:
1. build stage using official Go image
2. minimal runtime image

Runtime image options:
- `distroless`
- `alpine` if debugging convenience matters more

Prefer:
- non-root user where practical

### 16.2 Docker Compose
Need:
- environment variables
- volume mounts for certs and credential file
- exposed ports
- restart policy

Two example profiles can help:
- TLS direct listener
- private-only listener

### 16.3 .env.example
Include:
- safe placeholders
- commented examples for allowlists
- explicit note that auth values should be bcrypt hashes

## 17. README Structure

Recommended sections:
1. What it is
2. Why explicit proxy, not API gateway
3. Features
4. Security model
5. Quick start
6. Configuration reference
7. Credential generation example
8. Docker deployment
9. TLS deployment
10. Private-network deployment
11. Example client configuration
12. Curl verification
13. Limitations

### 17.1 Client Configuration Notes
Document:
- generic `HTTP_PROXY` / `HTTPS_PROXY`
- tools that support explicit proxy config directly
- if some tools prefer `base_url`, explain that this project intentionally does not emulate vendor API endpoints

## 18. Error Model

Suggested proxy-facing statuses:
- `400 Bad Request`
  - malformed absolute-form request
  - malformed CONNECT authority
- `403 Forbidden`
  - source IP denied
  - destination denied
- `407 Proxy Authentication Required`
  - auth failed
- `429 Too Many Requests`
  - per-IP concurrency exceeded
- `502 Bad Gateway`
  - upstream dial failure
  - DNS resolution failure if surfaced as upstream issue
- `504 Gateway Timeout`
  - upstream timeout

Audit logs should carry richer internal categories than the HTTP status alone.

## 19. Testing Strategy

### 19.1 Unit Tests
- auth header parsing
- bcrypt validation
- CIDR allowlist matching
- destination host/suffix matching
- destination port matching
- private/reserved IP classification
- config validation
- hop-by-hop header stripping
- concurrency limiter acquire/release

### 19.2 Integration Tests
Use `httptest` and local listeners.

#### HTTP Forward Tests
- authenticated request succeeds
- unauthenticated request returns `407`
- malformed absolute-form returns `400`
- allowed domain passes
- disallowed domain returns `403`
- disallowed port returns `403`
- sensitive headers do not appear in logs
- streaming response is relayed without full buffering

#### CONNECT Tests
- authenticated CONNECT succeeds
- unauthenticated CONNECT returns `407`
- malformed authority returns `400`
- disallowed host/port returns `403`
- bidirectional data passes through tunnel
- half-close does not deadlock
- idle tunnel timeout closes tunnel correctly

#### Concurrency Tests
- first connection acquires slot
- second over-limit connection gets `429`
- slot released after normal completion
- slot released after upstream failure

### 19.3 Logging Tests
Capture logs in-memory and assert:
- JSON shape exists
- audit fields present
- auth headers not leaked
- body content not leaked

## 20. Implementation Phases

### Phase 0: Bootstrap
- initialize `go.mod`
- add basic package layout
- choose logger approach
- add version variable

### Phase 1: Config and Validation
- implement config struct
- parse env vars
- validate config
- add tests for invalid configs

### Phase 2: Auth and Limiter
- implement basic auth parser
- implement bcrypt user store
- implement concurrency limiter
- add tests

### Phase 3: Destination Policy
- implement host/suffix allowlist
- implement port allowlist
- implement reserved/private IP classification
- implement DNS resolution helpers
- add tests

### Phase 4: HTTP Forward Proxy
- implement handler dispatch
- implement HTTP forward path
- implement header stripping
- implement transport and streaming response relay
- add integration tests

### Phase 5: CONNECT Tunnel
- implement hijack flow
- implement upstream dial
- implement bidirectional copy with accounting
- implement idle timeout and close reasons
- add integration tests

### Phase 6: Logging and Admin
- implement app logger
- implement audit logger
- implement `/healthz`
- add logging tests

### Phase 7: Deployment Artifacts
- write Dockerfile
- write docker-compose.yml
- write `.env.example`
- document credential generation
- write README

### Phase 8: Verification
- run `go test ./...`
- build binary
- build image
- verify compose startup path
- perform local curl checks

## 21. Recommended First Commit Shape

If implementing incrementally, a practical order is:
1. config + auth + limiter + tests
2. policy + DNS + tests
3. HTTP forward + tests
4. CONNECT + tests
5. logging + admin
6. Docker + README

## 22. Known Risks and Mitigations

### Risk: Accidental Open Proxy
Mitigation:
- fail closed without destination allowlist
- reserved/private IP block
- auth required

### Risk: Basic Auth Over Cleartext
Mitigation:
- TLS mode or private-only deployment as default

### Risk: CONNECT Deadlocks or Leaks
Mitigation:
- explicit half-close handling
- deadline strategy
- integration tests around EOF and idle timeout

### Risk: Streaming Broken by Timeouts
Mitigation:
- avoid generic write timeout
- separate tunnel idle timeout from request header timeout

### Risk: DNS Rebinding / Hostname Bypass
Mitigation:
- resolve before dial
- enforce IP policy on resolved IPs

## 23. Definition of Done

The implementation is done when:
- all P0 items in the original implementation brief are implemented
- tests pass locally with `go test ./...`
- Docker image builds
- compose startup works with documented config
- unauthenticated requests reliably return `407`
- over-limit source IPs reliably return `429`
- disallowed destinations reliably return `403`
- logs are structured JSON without sensitive header/body leakage
- README makes the explicit-proxy model and security assumptions unambiguous

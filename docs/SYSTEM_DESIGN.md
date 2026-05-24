# Alpine WebAdmin — System Design Document

**Version**: 1.0  
**Status**: Architecture Complete — Awaiting Implementation Approval  
**Target**: Intel Atom x5-E8000, 2–4 GB RAM, 4 GB eMMC, Alpine Linux (musl, OpenRC)

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Repository & Package Structure](#2-repository--package-structure)
3. [Internal Service Boundaries](#3-internal-service-boundaries)
4. [IPC Protocol Design](#4-ipc-protocol-design)
5. [Authentication Flow](#5-authentication-flow)
6. [Session Lifecycle](#6-session-lifecycle)
7. [WebSocket Lifecycle](#7-websocket-lifecycle)
8. [Telemetry Architecture](#8-telemetry-architecture)
9. [Config Transaction Architecture](#9-config-transaction-architecture)
10. [Persistence Architecture](#10-persistence-architecture)
11. [Logging Architecture](#11-logging-architecture)
12. [Memory Management Strategy](#12-memory-management-strategy)
13. [Concurrency Model](#13-concurrency-model)
14. [Goroutine Lifecycle Rules](#14-goroutine-lifecycle-rules)
15. [Error Handling Conventions](#15-error-handling-conventions)
16. [Security Hardening Strategy](#16-security-hardening-strategy)
17. [Build Pipeline](#17-build-pipeline)
18. [Release Strategy](#18-release-strategy)
19. [Upgrade Strategy](#19-upgrade-strategy)
20. [Recovery-Mode Strategy](#20-recovery-mode-strategy)

Appendices:
- [A. API Route Map](#appendix-a-api-route-map)
- [B. IPC Message Schemas](#appendix-b-ipc-message-schemas)
- [C. Threat Model](#appendix-c-threat-model)
- [D. Performance Considerations](#appendix-d-performance-considerations)
- [E. Resource Budgeting Estimates](#appendix-e-resource-budgeting-estimates)

---

## 1. Executive Summary

Alpine WebAdmin is a single-binary (split into two cooperating static binaries) web administration platform for headless Alpine Linux servers on severely resource-constrained x86 hardware. The system is built entirely in Go with `CGO_ENABLED=0`, uses only the Go standard library, embeds an Alpine.js frontend via `go:embed`, and communicates between an unprivileged web server and a privileged root helper via Unix domain sockets authenticated with `SO_PEERCRED`.

**Key non-functional targets:**

| Metric | Target |
|--------|--------|
| Idle RSS | ≤ 15 MB |
| Peak RSS | ≤ 30 MB |
| Binary size (each) | ≤ 5 MB |
| Telemetry latency | ≤ 2 s |
| HTTP cold-start response | ≤ 10 ms |
| WebSocket fan-out | 10 concurrent clients |
| Go version | 1.22+ |

---

## 2. Repository & Package Structure

### File Tree

```
alpine-webadmin/
├── cmd/
│   ├── webadmin/
│   │   └── main.go              # Entrypoint: flags, init, graceful shutdown
│   └── roothelper/
│       └── main.go              # Entrypoint: bind socket, SO_PEERCRED, dispatch
├── pkg/
│   ├── config/
│   │   ├── config.go            # Config struct, validation, defaults
│   │   └── schema.json          # JSON Schema for config validation
│   ├── ipc/
│   │   ├── types.go             # Envelope, Request, Response structs
│   │   ├── codec.go             # Length-prefixed JSON encode/decode
│   │   ├── client.go            # webadmin → roothelper IPC client
│   │   └── server.go            # roothelper IPC accept loop
│   ├── proc/
│   │   ├── stat.go              # /proc/stat parser
│   │   ├── meminfo.go           # /proc/meminfo parser
│   │   ├── loadavg.go           # /proc/loadavg parser
│   │   ├── diskstats.go         # /proc/diskstats parser
│   │   ├── netdev.go            # /proc/net/dev parser
│   │   ├── uptime.go            # /proc/uptime parser
│   │   ├── thermal.go           # /sys/class/thermal parser
│   │   └── mount.go             # /proc/mounts + statfs
│   ├── telemetry/
│   │   ├── collector.go         # Ticker-driven collection goroutine
│   │   ├── snapshot.go          # Snapshot struct definitions
│   │   └── hub.go               # Broadcast hub for WebSocket conns
│   ├── auth/
│   │   ├── session.go           # Session struct, cookie generation
│   │   ├── store.go             # In-memory map + TTL sweeper
│   │   └── password.go          # bcrypt verify + hash generation
│   ├── log/
│   │   └── logger.go            # Leveled JSON stderr logger
│   ├── ws/
│   │   ├── frame.go             # RFC 6455 frame parser/serializer
│   │   ├── conn.go              # Conn type, read/write goroutines
│   │   └── upgrade.go           # Hijack net/http for WS upgrade
│   ├── security/
│   │   ├── csrf.go              # Double-submit cookie validation
│   │   ├── ratelimit.go         # Per-IP token bucket
│   │   └── sanitize.go          # Path and input sanitization
│   └── version/
│       └── version.go           # Build-time ldflags version string
├── internal/
│   └── frontend/
│       ├── embed.go             # //go:embed directive
│       └── assets/
│           ├── index.html
│           ├── alpine.min.js
│           ├── app.css
│           └── app.js
├── init/
│   ├── openrc/
│   │   ├── webadmin             # OpenRC init script
│   │   └── roothelper           # OpenRC init script
│   └── apk/
│       └── APKBUILD             # Alpine package manifest
├── build/
│   └── build.sh                 # Release build script
├── docs/
│   ├── SYSTEM_DESIGN.md         # This document
│   ├── architecture.md          # Component overview
│   ├── security.md              # Deep-dive security model
│   ├── ipc.md                   # Deep-dive IPC protocol
│   ├── telemetry.md             # Deep-dive telemetry
│   ├── memory.md                # Deep-dive memory management
│   ├── concurrency.md           # Deep-dive concurrency model
│   ├── errors.md                # Deep-dive error handling
│   ├── build.md                 # Deep-dive build & release
│   ├── config.md                # Deep-dive config transactions
│   ├── persistence.md           # Deep-dive diskless persistence
│   └── recovery.md              # Deep-dive recovery mode
├── testdata/
│   └── proc/                    # Fixture /proc snapshots for unit tests
├── go.mod
├── go.sum
└── README.md
```

### Package Visibility Rules

| Package | Visibility | Consumers |
|---------|------------|-----------|
| `cmd/*` | — | none (main only) |
| `pkg/*` | public | any internal package or external test |
| `internal/frontend` | internal | `cmd/webadmin` only |
| `testdata/` | — | tests only |

---

## 3. Internal Service Boundaries

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Browser                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                     │
│  │  index.html │  │ Alpine.js   │  │   app.js    │                     │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                     │
│         └──────────────────┴──────────────────┘                           │
│                          │ HTTPS (or HTTP behind proxy)                │
└──────────────────────────┼───────────────────────────────────────────────┘
                           │
┌──────────────────────────▼───────────────────────────────────────────────┐
│ webadmin (UID: webadmin, GID: webadmin)                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐                 │
│  │ HTTP Router  │──▶│   REST API   │──▶│   IPC Client │                 │
│  │  (net/http)  │   │  Handlers    │   │              │                 │
│  └──────────────┘   └──────────────┘   └──────┬───────┘                 │
│         │                                      │                        │
│  ┌──────▼──────┐   ┌──────────────┐           │                        │
│  │   Static    │   │  WebSocket   │           │ Unix socket             │
│  │   Assets    │   │   Server     │◄──────────┘                        │
│  └─────────────┘   └──────────────┘                                    │
└──────────────────────────┬───────────────────────────────────────────────┘
                           │
┌──────────────────────────▼───────────────────────────────────────────────┐
│ roothelper (UID: 0, GID: root)                                         │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐               │
│  │ Unix Socket  │──▶│ SO_PEERCRED  │──▶│  Dispatcher  │               │
│  │   Listener   │   │   Validate   │   │              │               │
│  └──────────────┘   └──────────────┘   └──────┬───────┘               │
│                                               │                        │
│  ┌──────────────┐   ┌──────────────┐   ┌──────▼───────┐              │
│  │   Logger     │   │   Auditor    │   │   Executor   │              │
│  │  (pkg/log)   │   │ (structured) │   │ (abs paths)  │              │
│  └──────────────┘   └──────────────┘   └──────────────┘              │
└────────────────────────────────────────────────────────────────────────┘
```

### Boundary Contracts

| Boundary | Caller | Callee | Contract |
|----------|--------|--------|----------|
| Browser → webadmin | Alpine.js fetch | `net/http` | HTTP 1.1; JSON request/response; cookie auth |
| Browser ↔ webadmin | WebSocket | `pkg/ws` | RFC 6455 text frames; JSON telemetry |
| webadmin → roothelper | `pkg/ipc.Client` | `pkg/ipc.Server` | Length-prefixed JSON over `AF_UNIX`; `SO_PEERCRED` enforced |
| webadmin → filesystem | `pkg/proc` | `/proc`, `/sys` | Read-only; no caching beyond single collection tick |
| roothelper → OS | `pkg/ipc` executor | `/sbin/rc-service`, etc. | Exact absolute paths; no PATH resolution; no shell |

---

## 4. IPC Protocol Design

### Transport

- **Type**: `AF_UNIX` `SOCK_STREAM`
- **Path**: `/run/webadmin/ipc.sock` (configurable)
- **Permissions**: `0660`, owner `root`, group `webadmin`
- **Connection Model**: One request/response per connection. `roothelper` closes after writing response. This prevents head-of-line blocking and simplifies error isolation.

### Framing

```
┌─────────────┬──────────────────────────────┐
│  Length     │         Payload              │
│  uint32 BE  │     JSON (UTF-8)             │
│  4 bytes    │     Length bytes             │
└─────────────┴──────────────────────────────┘
```

- Max message size: **65,536 bytes**
- Zero-length → connection closed signal

### Envelope

```go
type Envelope struct {
    Version     int             `json:"v"`
    RequestID   string          `json:"rid"`
    MessageType string          `json:"type"`
    Payload     json.RawMessage `json:"payload"`
}
```

### Capability Matrix

| MessageType | Direction | Description | Response |
|-------------|-----------|-------------|----------|
| `cap.service.list` | C→S | List all OpenRC services | `response.ok` + `ServiceListData` |
| `cap.service.status` | C→S | Single service status | `response.ok` + `ServiceStatusData` |
| `cap.service.control` | C→S | start/stop/restart | `response.ok` + `ServiceStatusData` |
| `cap.system.info` | C→S | Hostname, kernel, uptime | `response.ok` + `SystemInfoData` |
| `cap.package.list` | C→S | Installed APK packages | `response.ok` + `PackageListData` |
| `response.ok` | S→C | Success | Payload varies by request |
| `response.error` | S→C | Failure | `ResponseError{Code, Message}` |

### Error Codes

- `InvalidRequest` — malformed JSON or missing fields
- `CapabilityDenied` — not in allowlist
- `ServiceNotFound` — OpenRC does not know service name
- `ActionNotAllowed` — action not in {start, stop, restart, status}
- `ExecutionFailed` — binary exited non-zero
- `InternalError` — unexpected roothelper failure

See `docs/ipc.md` for full wire examples and Go pseudocode.

---

## 5. Authentication Flow

```
[Browser — First Visit]
         │
         ▼
[GET /] ──► [No session cookie]
         │
         ▼
[Redirect 302 → /#/login]
         │
         ▼
[User submits credentials]
         │
         ▼
[POST /api/login]
         │
         ▼
[Rate limit check (IP)]
         │
         ▼
[bcrypt(password) == stored_hash?]
    Yes │            │ No
      ▼              ▼
[Generate SID]    [HTTP 401 + log]
[Set __Host-SID]
[Set __Host-CSRF]
         │
         ▼
[HTTP 204]
         │
         ▼
[Frontend stores CSRF token]
[Subsequent requests include
 X-CSRF-Token header]
```

### Session Cookie Specification

| Attribute | Value |
|-----------|-------|
| Name | `__Host-SID` |
| Value | 64 hex chars (32 bytes from `crypto/rand`) |
| Secure | true |
| HttpOnly | true |
| SameSite | Strict |
| Path | / |
| Max-Age | `session_ttl` seconds (default 3600) |

---

## 6. Session Lifecycle

```
[Login]
  │
  ▼
[Session created in memory map]
  key=SID, value=Session{User, Created, Expires}
  │
  ▼
[Active use]
  Each HTTP request: lookup SID in map, check Expires > now()
  │
  ▼
[Logout or Expiry]
  Logout: explicit DELETE /api/session → remove from map
  Expiry: background sweeper removes every 5 min
  │
  ▼
[Session removed]
  Client receives 401 on next request → redirect to login
```

### Session Store Implementation

```go
type Store struct {
    mu       sync.RWMutex
    sessions map[string]Session
}

type Session struct {
    Username string
    Created  time.Time
    Expires  time.Time
}
```

- No persistent backing. Sessions are lost on restart. This is acceptable for an admin tool.
- Max sessions bounded by `session_ttl / typical_login_interval`. With TTL=1h and 1 admin, max is 1.

---

## 7. WebSocket Lifecycle

```
[Client Request]
  GET /ws
  Connection: Upgrade
  Upgrade: websocket
  Sec-WebSocket-Key: ...
         │
         ▼
[Server — Hijack net.Conn]
  Validate: session cookie present & valid
  Validate: WS max connections not exceeded
         │
         ▼
[HTTP 101 Switching Protocols]
         │
         ▼
[Register conn in Hub]
         │
         ▼
[Spawn Read Goroutine]
  Loop: Read frame
    Ping → send Pong
    Close → signal shutdown
    Text/Binary → ignored (server is push-only)
         │
         ▼
[Spawn Write Goroutine]
  Loop: select on sendCh / shutdownCh
    Write frame to net.Conn
    Set write deadline per frame
         │
         ▼
[Telemetry arrives from Collector]
  Hub.Broadcast(snapshotJSON)
    For each conn: non-blocking send to conn.sendCh
    If sendCh full: drop message (slow consumer)
         │
         ▼
[Client disconnect or timeout]
  Read goroutine detects EOF / close frame
  Unregister from Hub
  Signal write goroutine shutdown
  WaitGroup waits
  Close net.Conn
```

### Connection Budget

| Resource | Limit |
|----------|-------|
| Max concurrent WS | 10 |
| WS read timeout | 60 s |
| WS write timeout | 10 s |
| Per-conn send buffer | 256 messages |
| Max frame size | 65,536 bytes |

---

## 8. Telemetry Architecture

### Collection Pipeline

```
[Ticker 2s]
  │
  ▼
[Collector Goroutine]
  ├─► Read /proc/stat      → CPU jiffies (aggregate + per-core)
  ├─► Read /proc/meminfo   → Memory KB
  ├─► Read /proc/loadavg   → Load floats
  ├─► Read /proc/diskstats → I/O counters (delta vs previous)
  ├─► Read /proc/net/dev   → Interface counters
  ├─► Read /proc/uptime    → Uptime seconds
  ├─► Read /sys/class/thermal → Temperature (optional)
  └─► Read /proc/mounts + statfs → Filesystem usage
  │
  ▼
[Compute deltas (CPU, Disk)]
[Assemble Snapshot struct]
  │
  ▼
[JSON encode → pooled buffer]
  │
  ▼
[Hub.Broadcast]
  │
  ▼
[Fan-out to N WebSocket conns]
```

### Frontend Binding

```javascript
// app.js pseudocode
const ws = new WebSocket(`wss://${location.host}/ws`);
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  if (msg.type === 'telemetry') {
    alpineApp.telemetry = msg.payload;
  }
};
```

---

## 9. Config Transaction Architecture

### Design Principle

Configuration is **read-mostly, write-rarely**. There is no hot-reload. All config changes require a service restart. This eliminates race conditions, partial-state bugs, and simplifies validation.

### Sources of Truth

| File | Purpose | Managed By |
|------|---------|------------|
| `/etc/webadmin/config.json` | Runtime parameters | Admin (manually or via future UI) |
| `/etc/webadmin/services.json` | Service allowlist | Admin |
| `/etc/webadmin/passwd` | bcrypt hash of admin password | Install script / `passwd` helper |

### Config Transaction Flow (Future UI)

```
[Admin submits config change via UI]
         │
         ▼
[POST /api/config — validated by webadmin]
         │
         ▼
[webadmin writes to temp file /etc/webadmin/config.json.tmp]
[Atomic rename to /etc/webadmin/config.json]
         │
         ▼
[Return 202 Accepted + message: "Restart required"]
         │
         ▼
[Admin clicks "Restart" or runs rc-service webadmin restart]
         │
         ▼
[OpenRC restarts webadmin; new config loaded]
```

### Validation Rules

- `listen` must be a valid `host:port` or `:port`
- `ipc_socket` must be an absolute path under `/run` or `/tmp` (warn if `/tmp`)
- `session_ttl` ∈ [300, 86400]
- `ws_max_conns` ∈ [1, 100]
- `rate_limit_rps` ∈ [1, 1000]
- `allowed_helpers` must contain only absolute paths to existing executables

---

## 10. Persistence Architecture

### Alpine Diskless Mode Compatibility

Alpine can run entirely from RAM (`lbu` / `apk overlay`). Writes to the root filesystem may be ephemeral unless committed. The system must tolerate this.

### Persistence Strategy

| Data | Persistence | Reason |
|------|-------------|--------|
| Config files (`/etc/webadmin/*`) | Persistent (on disk or overlay) | Required for boot-time behavior |
| Session store | Ephemeral (memory) | Acceptable loss on restart |
| Telemetry history | Ephemeral (none) | No TSDB; only live snapshot |
| Logs | Ephemeral (stderr → syslog) | Syslog may write to tmpfs ring buffer |
| Audit events | Ephemeral (stderr) | If persistent audit required, admin configures syslog to remote or disk |

### Diskless Boot Sequence

```
[Boot from ISO/USB]
  │
  ▼
[overlayfs mounted]
  │
  ▼
[OpenRC starts roothelper]
  │
  ▼
[OpenRC starts webadmin]
  │
  ▼
[System operational]
  │
  ▼
[Admin changes config via UI]
  │
  ▼
[Config written to overlay]
  │
  ▼
[Admin runs lbu commit (if diskless)]
  │
  ▼
[Changes persisted to boot media]
```

The application itself never assumes its writes survive reboot. It reads config at startup and operates entirely in memory thereafter.

---

## 11. Logging Architecture

### Backend Logger (`pkg/log`)

- **Output**: `os.Stderr` only
- **Format**: Newline-delimited JSON
- **Fields**: `ts` (RFC3339), `lvl`, `msg`, `pkg`, `fn`, `rid` (request ID)

### Log Levels

| Level | Use |
|-------|-----|
| `DEBUG` | proc parsing details, WS frame traces |
| `INFO` | HTTP requests, successful IPC calls, session creation |
| `WARN` | slow WS consumer, /proc file missing, thermal zone absent |
| `ERROR` | IPC failure, auth failure, parse failure, execution failure |

### Request Correlation

Every HTTP request and IPC request carries a `request_id` (UUID v4). All log lines include this ID for end-to-end tracing.

### OpenRC Integration

```sh
# webadmin init script captures stderr
command="/usr/sbin/webadmin"
command_args="-config /etc/webadmin/config.json"
# stderr goes to OpenRC logger, which writes to /var/log/webadmin/current
# when using supervise-daemon or logger pipe
```

### No Log Rotation in Application

Rotation is delegated to the host: `logrotate`, `svlogd`, or `newsyslog` (Alpine/BusyBox).

---

## 12. Memory Management Strategy

### Runtime Tuning

```sh
export GOGC=50          # GC at 50% heap growth
export GOMEMLIMIT=40MiB # Soft limit; runtime GCs more aggressively before this
```

### Allocation Hotspots & Mitigations

| Hotspot | Mitigation |
|---------|------------|
| JSON telemetry (broadcast to N clients) | Encode once into `sync.Pool` buffer; reuse slice for all fan-outs |
| /proc file reads | `bufio.NewReaderSize(4096)`; parse line-by-line into pre-allocated structs |
| WS frame assembly | `sync.Pool` for `[]byte` of `len(payload)+14` (max header) |
| HTTP static assets | `go:embed` → serve directly from embedded FS; no per-request allocation |
| Session map | Bounded by TTL; sweeper every 5 min |

### Goroutine Memory Budget

| Component | Goroutines | Stack estimate |
|-----------|------------|----------------|
| HTTP server | ~1 per req + accept loop | 2 KB each (Go runtime) |
| Telemetry collector | 1 | 2 KB |
| WS Hub + conns | 1 + 2×N (N≤10) | 2 KB each |
| Session sweeper | 1 | 2 KB |
| Rate limiter sweeper | 1 | 2 KB |
| RootHelper | 1 + 1 per request | 2 KB each |
| **Total peak** | ~35 | **~70 KB stacks** |

### Heap Budget Estimate

| Area | Estimate |
|------|----------|
| Code + static data | ~8 MB |
| Session store (1 admin) | ~1 KB |
| Telemetry snapshot | ~4 KB |
| WS buffers (10 conns × 256 × 4 KB) | ~10 MB (allocated on demand, not pre-allocated) |
| /proc parsers + pools | ~1 MB |
| HTTP runtime + mux | ~2 MB |
| IPC runtime | ~500 KB |
| **Idle total** | **~12–14 MB** |
| **Peak total** | **~20–25 MB** |

Both targets (15 MB idle, 30 MB peak) are achievable with margin.

---

## 13. Concurrency Model

### Core Tenet

**Share memory by communicating; do not communicate by sharing memory.**

All shared state is guarded by `sync.RWMutex` or channels. No `sync/atomic` except for counters in hot paths where profiling proves necessary.

### Ownership Map

| State | Owner | Access Pattern |
|-------|-------|----------------|
| Session map | `pkg/auth.Store` | `RWMutex`; read-heavy |
| WS Hub conn set | `pkg/telemetry.Hub` | `RWMutex`; write on connect/disconnect, read on broadcast |
| Rate limiter buckets | `pkg/security.Limiter` | `Mutex` per bucket; coarse lock acceptable (per-IP isolation) |
| Telemetry snapshot | `pkg/telemetry.Collector` | Single goroutine writes; Hub reads immutable copy via channel |
| IPC client | `pkg/ipc.Client` | No shared state; new conn per call |
| Config | `pkg/config.Config` | Immutable after startup; no lock needed |

### Channel Topology

```
[Telemetry Collector]
         │
         │ snapshotCh chan []byte
         ▼
    [Hub broadcaster]
         │
         │ fan-out to N conn.sendCh
         ▼
[Conn 1 sendCh]  [Conn 2 sendCh] ... [Conn N sendCh]
```

### No Locks in Request Handlers

HTTP handlers must not hold locks while making IPC calls or I/O. Pattern:

```go
func (h *Handler) ServeHTTP(w, r) {
    // 1. Read-only lock to fetch session
    // 2. Unlock
    // 3. Make IPC call (may block)
    // 4. Write response
}
```

---

## 14. Goroutine Lifecycle Rules

### Rule 1: Every Goroutine Must Be Stoppable

All long-lived goroutines accept a `<-chan struct{}` (shutdown signal). On signal, they drain and exit.

### Rule 2: No Leaked Goroutines on Shutdown

`cmd/webadmin/main.go` uses `sync.WaitGroup` to track:
- Telemetry collector
- Session sweeper
- Rate limiter sweeper
- WS Hub broadcaster

### Rule 3: No Goroutines in Library Constructors

Packages (`pkg/*`) do not spawn goroutines in `New()` or `Init()`. The caller (`cmd/*`) explicitly starts and stops all goroutines.

### Rule 4: Conn Goroutines Are Tightly Bound

Each WebSocket connection spawns exactly 2 goroutines (read + write). Both exit before `Hub` removes the conn from its map. A `WaitGroup` per conn ensures this.

### Rule 5: Short-Lived IPC Goroutines

`roothelper` spawns one goroutine per accepted connection. It must exit after writing the response and closing the conn. No persistent handlers.

### Lifecycle Diagram: webadmin Goroutines

```
main()
  │
  ├──► HTTP Server (net/http) ──► per-request goroutines
  │
  ├──► Telemetry Collector ──► shutdownCh
  │
  ├──► Session Sweeper ──► shutdownCh
  │
  ├──► Rate Limiter Sweeper ──► shutdownCh
  │
  └──► WS Hub Broadcaster ──► shutdownCh

On SIGTERM:
  1. Close HTTP listener (with timeout)
  2. Signal all shutdownCh
  3. WaitGroup.Wait()
  4. Exit 0
```

---

## 15. Error Handling Conventions

### Philosophy

**Fail fast, log context, degrade gracefully.**

### Error Categories

| Category | Behavior | Example |
|----------|----------|---------|
| **Fatal** | Log + exit immediately | Config invalid, cannot bind socket |
| **Request** | Log + return HTTP error to client | Auth failed, IPC timeout |
| **Operational** | Log + continue (degraded) | `/proc` file unreadable, thermal missing |
| **Internal** | Log + generic 500 to client | Unexpected panic recovered by HTTP middleware |

### Error Wrapping

All errors use `fmt.Errorf` with `%w` for chaining:

```go
if err != nil {
    return fmt.Errorf("proc.parseStat: %w", err)
}
```

### HTTP Error Mapping

```go
var errorCodes = map[string]int{
    "InvalidRequest":   400,
    "Unauthorized":     401,
    "Forbidden":        403,
    "NotFound":         404,
    "RateLimited":      429,
    "CapabilityDenied": 503,
    "InternalError":    500,
}
```

### Panic Recovery

`net/http` server has built-in panic recovery per handler. Additional middleware logs recovered panics with stack traces and returns HTTP 500.

### Error Response Format

```json
{
  "error": {
    "code": "RateLimited",
    "message": "Too many requests from this IP",
    "request_id": "abc-123"
  }
}
```

---

## 16. Security Hardening Strategy

### Defense in Depth Layers

```
Layer 1: Network
  └─► Bind localhost or TLS; reverse proxy; firewall VLAN

Layer 2: Transport
  └─► HTTPS/TLS; Secure HttpOnly SameSite=Strict cookies

Layer 3: Application
  └─► Rate limiting; CSRF tokens; input validation; path sanitization

Layer 4: Authentication
  └─► bcrypt passwords; session TTL; in-memory only

Layer 5: Authorization
  └─► Capability matrix enforced in roothelper

Layer 6: Privilege Separation
  └─► webadmin unprivileged; roothelper via Unix socket + SO_PEERCRED

Layer 7: Execution
  └─► Absolute binary paths; no shell; allowlist only

Layer 8: Audit
  └─► All privileged ops logged with request ID, capability, target
```

### Hardening Checklist

- [ ] Build with `CGO_ENABLED=0` (no libc attack surface)
- [ ] Strip debug info (`-ldflags="-s -w"`)
- [ ] Disable Go runtime profiling endpoints in production (`prod` build tag)
- [ ] File permissions: binaries `0750 root:root`; configs `0640 root:webadmin`
- [ ] Unix socket `0660 root:webadmin`
- [ ] `webadmin` user has `nologin` shell and no supplementary groups
- [ ] `roothelper` drops nothing but validates everything
- [ ] No `PATH` lookups; all commands absolute
- [ ] No `sh -c`; all `exec.Cmd` with explicit `Path` and `Args`
- [ ] Max body sizes enforced on all handlers
- [ ] WebSocket max frame size enforced
- [ ] All inputs validated against regex allowlists

---

## 17. Build Pipeline

### Local Development

```sh
# Quick build
go build -o bin/webadmin ./cmd/webadmin
go build -o bin/roothelper ./cmd/roothelper

# Test
CGO_ENABLED=0 go test ./...

# Vet + fmt
go vet ./...
gofmt -w .
```

### CI/CD Pipeline (GitHub Actions / GitLab CI)

```
[Push to main/PR]
    │
    ▼
[Lint] ──► go vet, staticcheck, gofmt
    │
    ▼
[Test] ──► go test -race -cover ./...
    │
    ▼
[Build] ──► CGO_ENABLED=0 GOOS=linux GOARCH=amd64
            go build -trimpath -ldflags="-s -w -X pkg/version.Version=$TAG"
    │
    ▼
[Package] ──► Tarball + APKBUILD → .apk
    │
    ▼
[Release] ──► GitHub Release with .apk + checksums
```

### Build Tags

| Tag | Effect |
|-----|--------|
| `prod` | Disables `/_debug/*` endpoints; sets min log level to INFO |
| `debug` | Enables pprof and memstats endpoints (localhost-only) |

---

## 18. Release Strategy

### Versioning

Semantic Versioning: `MAJOR.MINOR.PATCH`

- **MAJOR**: Breaking config or IPC protocol changes
- **MINOR**: New capabilities, UI features, non-breaking additions
- **PATCH**: Bug fixes, security patches, performance improvements

### Release Artifacts

| Artifact | Description |
|----------|-------------|
| `alpine-webadmin-$VERSION.apk` | Alpine package (installs both binaries + init scripts) |
| `webadmin-$VERSION-linux-amd64` | Standalone binary (manual install) |
| `roothelper-$VERSION-linux-amd64` | Standalone binary (manual install) |
| `checksums.txt` | SHA-256 of all artifacts |
| `SOURCE.tar.gz` | Source code snapshot |

### Release Gates

1. All tests pass
2. `go vet` clean
3. Binary size ≤ 5 MB each
4. Memory acceptance test pass (idle ≤ 15 MB RSS for 60 s)
5. Security scan (govulncheck) clean

---

## 19. Upgrade Strategy

### In-Place Upgrade (Binary Swap)

```sh
# Via apk
apk add --upgrade alpine-webadmin
rc-service webadmin restart
rc-service roothelper restart
```

### Zero-Downtime Upgrade (Manual)

```sh
# 1. Drop new binaries as .new suffix
cp webadmin /usr/sbin/webadmin.new
cp roothelper /usr/sbin/roothelper.new

# 2. Atomic rename (minimal race window)
mv /usr/sbin/webadmin.new /usr/sbin/webadmin
mv /usr/sbin/roothelper.new /usr/sbin/roothelper

# 3. Restart (OpenRC handles graceful stop/start)
rc-service roothelper restart
rc-service webadmin restart
```

### Config Migration

- Config files are marked as `config` in APKBUILD → preserved on upgrade
- New versions must be backward-compatible with old config schemas
- If breaking config change required: bump MAJOR version, document migration

---

## 20. Recovery-Mode Strategy

### Problem Statement

If `webadmin` or `roothelper` fails to start (bad config, permission issue, etc.), the admin may lose remote access to a headless system.

### Recovery Mechanisms

#### 1. Config Validation on Startup

Both binaries validate config before binding sockets. If invalid:
- Print structured error to stderr
- Exit with non-zero code
- OpenRC logs the failure to `/var/log/webadmin/current`

#### 2. RootHelper Independent Start

`roothelper` has no dependency on `webadmin` being online. If `webadmin` crashes, `roothelper` continues running and accepts IPC connections once `webadmin` restarts.

#### 3. Local Console Fallback

If web server is inaccessible:
- Admin uses physical console or serial console
- Alpines's standard `setup-alpine` and `lbu` tools remain available
- `roothelper` is not required for basic system administration

#### 4. Recovery Endpoint (Future)

A minimal unauthenticated `/health` endpoint on `webadmin` returns:

```json
{
  "status": "up",
  "version": "0.1.0",
  "roothelper_connected": true
}
```

This allows load balancers and monitoring to detect failure without exposing sensitive data.

#### 5. Safe Mode Init Script

The OpenRC init script has a `checkconfig` command:

```sh
rc-service webadmin checkconfig  # Validates config, prints errors, exits
```

#### 6. Backup Config on Write

When config is changed via API:
- Original config copied to `/etc/webadmin/config.json.bak`
- If restart fails, admin can `mv config.json.bak config.json` via console

---

## Appendix A: API Route Map

### Unauthenticated Routes

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Serve `index.html` |
| `GET` | `/assets/*` | Embedded static files |
| `POST` | `/api/login` | Authenticate, set session cookie |
| `GET` | `/health` | Health check (unauthenticated) |

### Authenticated Routes (require valid `__Host-SID`)

| Method | Path | Description |
|--------|------|-------------|
| `DELETE` | `/api/logout` | Destroy session |
| `GET` | `/api/session` | Get current session info |
| `GET` | `/api/telemetry` | Latest telemetry snapshot (HTTP fallback) |
| `GET` | `/ws` | WebSocket upgrade for live telemetry |

### Authenticated + IPC Routes (forwarded to roothelper)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/services` | List OpenRC services |
| `GET` | `/api/services/:name` | Service status |
| `POST` | `/api/services/:name/:action` | Start/stop/restart |
| `GET` | `/api/system` | System info |
| `GET` | `/api/packages` | Installed APK packages |

### Admin Routes (future)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/config` | Update config (requires restart) |
| `POST` | `/api/password` | Change admin password |

### Middleware Stack (per request)

```
Request
  │
  ▼
[RequestLogger]      — log method, path, IP, request_id
  │
  ▼
[RateLimiter]        — 429 if exceeded
  │
  ▼
[SecurityHeaders]    — CSP, X-Frame-Options, HSTS
  │
  ▼
[CSRFCheck]          — 403 for mutating requests
  │
  ▼
[AuthCheck]          — 401 if no valid session
  │
  ▼
[Handler]
  │
  ▼
[Response]
```

---

## Appendix B: IPC Message Schemas

### Request Payloads

```go
type ServiceListReq struct{}

type ServiceStatusReq struct {
    Name string `json:"name"`
}

type ServiceControlReq struct {
    Name   string `json:"name"`   // validated against /etc/webadmin/services.json
    Action string `json:"action"` // "start" | "stop" | "restart" | "status"
}

type SystemInfoReq struct{}

type PackageListReq struct{}
```

### OK Response Payloads

```go
type ServiceListData struct {
    Services []ServiceEntry `json:"services"`
}

type ServiceEntry struct {
    Name     string `json:"name"`
    Status   string `json:"status"`   // "started" | "stopped" | "crashed"
    Runlevel string `json:"runlevel,omitempty"`
}

type ServiceStatusData struct {
    Name   string `json:"name"`
    Status string `json:"status"`
    PID    int    `json:"pid,omitempty"`
}

type SystemInfoData struct {
    Hostname   string `json:"hostname"`
    OS         string `json:"os"`
    Kernel     string `json:"kernel"`
    UptimeSec  int64  `json:"uptime_sec"`
    TotalMemKB int64  `json:"total_mem_kb"`
    CpuCount   int    `json:"cpu_count"`
}

type PackageListData struct {
    Packages []string `json:"packages"`
}
```

### Error Response Payload

```go
type ResponseError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

---

## Appendix C: Threat Model

### STRIDE Analysis

| Threat | Category | Mitigation |
|--------|----------|------------|
| Spoofing (impersonate roothelper) | S | `SO_PEERCRED` UID validation; socket `0660` permissions |
| Tampering (modify config) | T | Config files `0640 root:webadmin`; atomic writes; validation |
| Repudiation (deny action) | R | All privileged ops logged with request_id |
| Information Disclosure | I | No response body logging; no PII in logs; TLS/Secure cookies |
| Denial of Service | D | Rate limiting; WS conn cap; max body/frame sizes; GOGC tuning |
| Elevation of Privilege | E | No shell execution; absolute paths only; capability allowlist |

### Attack Scenarios

1. **Attacker gains webadmin shell**: Cannot escalate to root because `roothelper` validates UID and only executes pre-approved absolute binaries.
2. **Attacker steals session cookie**: Cookie is `HttpOnly` `SameSite=Strict`; XSS cannot exfiltrate it. If stolen via other means, attacker must also pass CSRF check and IP rate limits.
3. **Attacker floods WebSocket**: Conn cap (10) prevents exhaustion. Per-conn send buffer limits prevent memory blow-up.
4. **Attacker manipulates Unix socket**: Requires `webadmin` group membership or root access. Socket `0660` prevents unprivileged access.

---

## Appendix D: Performance Considerations

### HTTP

- `net/http` with `MaxHeaderBytes=1<<20` and custom `ReadTimeout`/`WriteTimeout`
- Static assets served via `embed.FS` + `http.ServeContentFS` (zero-copy when possible)
- No `http.FileServer` directory traversal risk; assets are embedded

### WebSocket

- Server is **push-only** for telemetry. No request/response over WS.
- Binary frames not used; all telemetry is text JSON.
- No message fragmentation support needed (telemetry fits in single frame).

### IPC

- Unix socket is local and kernel-optimized; latency < 1 ms per request.
- One connection per request means no head-of-line blocking.
- JSON encoding/decoding is acceptable for small messages (< 1 KB).

### /proc Parsing

- `/proc` is a virtual filesystem; reads do not hit disk.
- Avoid `ioutil.ReadFile` (allocates entire file). Use `bufio.Scanner` or manual line reading.
- Parse integers with custom fast path or `strconv.ParseUint`.

---

## Appendix E: Resource Budgeting Estimates

### Binary Sizes

| Component | Stripped Size | Estimate |
|-----------|---------------|----------|
| `webadmin` | ~4.5 MB | std lib + embed + ws + http |
| `roothelper` | ~2.5 MB | std lib + ipc + exec |
| **Combined** | **~7 MB** | Well under 4 GB eMMC |

### RAM Budget (Idle)

| Component | Size |
|-----------|------|
| Go runtime + heap | ~6 MB |
| Code segment (both binaries not resident) | ~4 MB |
| Session store | < 1 KB |
| Config + caches | ~500 KB |
| Telemetry snapshot | ~4 KB |
| HTTP idle overhead | ~2 MB |
| WS idle (no clients) | ~0 |
| IPC idle (no active calls) | ~0 |
| Goroutine stacks (few) | ~20 KB |
| **Total Idle** | **~12.5 MB** |

### RAM Budget (Peak: 10 WS clients + active API)

| Component | Size |
|-----------|------|
| Go runtime + heap | ~10 MB |
| Code segment | ~4 MB |
| Session store | < 1 KB |
| Telemetry buffers (pooled, 4 KB reused) | ~4 KB |
| WS send buffers (10 × 256 × 4 KB max) | ~10 MB (allocated on demand) |
| HTTP active requests | ~2 MB |
| IPC active calls | ~1 MB |
| Goroutine stacks (~35) | ~70 KB |
| **Total Peak** | **~27 MB** |

**Conclusion**: Targets of ≤ 15 MB idle and ≤ 30 MB peak are achievable with comfortable margin.

---

*End of System Design Document. Awaiting approval before proceeding to implementation.*

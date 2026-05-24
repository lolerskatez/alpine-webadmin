# Alpine WebAdmin — Technical Architecture

## 1. Overview

Alpine WebAdmin is a lightweight, single-binary web administration platform for resource-constrained Alpine Linux servers. It consists of two statically-linked Go binaries communicating over a Unix domain socket.

- **Binary 1: `webadmin`** — Unprivileged HTTP/WebSocket server + frontend.
- **Binary 2: `roothelper`** — Privileged daemon exposing a capability API over a Unix socket.

## 2. Constraints & Targets

| Parameter | Target |
|-----------|--------|
| Language | Go (CGO_ENABLED=0) |
| RAM Idle | ≤ 15 MB |
| RAM Peak | ≤ 30 MB |
| Storage | ≤ 4 MB binary footprint |
| Frameworks | None (std library only) |
| Frontend | Alpine.js (embedded) |
| Runtime | No Node.js on target |

## 3. Directory Structure

```
.
├── cmd/
│   ├── webadmin/          # Unprivileged web server entrypoint
│   └── roothelper/         # Privileged helper daemon entrypoint
├── pkg/
│   ├── config/             # JSON config parsing & validation
│   ├── ipc/                # IPC message types & wire format
│   ├── proc/               # /proc and /sys parsers
│   ├── telemetry/          # Metric collection & broadcast
│   ├── auth/               # Session cookie management
│   ├── log/                # Structured JSON logging
│   ├── ws/                 # Minimal RFC 6455 WebSocket implementation
│   └── security/           # Hardening utilities (CSRF, rate limiter)
├── internal/
│   └── frontend/           # go:embed'd HTML/JS/CSS assets
├── init/
│   ├── openrc/
│   │   ├── webadmin        # OpenRC init script
│   │   └── roothelper      # OpenRC init script
│   └── apk/
│       └── APKBUILD        # Alpine package manifest
└── docs/                   # Architecture & security documentation
```

## 4. Package Map

| Package | Visibility | Responsibility |
|---------|------------|----------------|
| `cmd/webadmin` | — | `main()`: parse flags, init listeners, mount handlers, graceful shutdown. |
| `cmd/roothelper` | — | `main()`: bind Unix socket, enforce SO_PEERCRED, dispatch capability RPCs. |
| `pkg/config` | public | Load `/etc/webadmin/config.json`; validate paths and permissions; hot-reload not supported. |
| `pkg/ipc` | public | Message structs, envelope format, codec, and request/response correlation IDs. |
| `pkg/proc` | public | Parsers for `/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `/proc/diskstats`, `/proc/net/dev`, `/proc/uptime`. Returns flat structs; no allocations on hot path. |
| `pkg/telemetry` | public | Ticker-driven collector using `pkg/proc`; maintains a ring buffer of last N samples; broadcasts latest snapshot to WebSocket hub. |
| `pkg/auth` | public | Secure session cookie issuance (`__Host-SID`), in-memory session store with TTL sweeper, bcrypt password check. |
| `pkg/log` | public | Leveled JSON logger (`debug/info/warn/error`) with caller context. Writes to stderr; consumed by OpenRC/syslog. |
| `pkg/ws` | public | Minimal RFC 6455 frame parser/serializer over `net.Conn`. Upgrade from `net/http` via `Hijacker`. Single writer goroutine per connection; read goroutine dispatches close/ping. |
| `pkg/security` | public | CSRF double-submit cookie validation, per-IP rate limiter (token bucket, in-memory), safe path sanitizer. |
| `internal/frontend` | internal | `//go:embed` directive for `index.html`, Alpine.js, CSS. Served via `net/http` `ServeContent` or direct `io.CopyN`. |

## 5. Component Architecture

```
┌──────────────────────────────────────┐
│  Browser                              │
│  (Alpine.js + fetch/WebSocket)        │
└──────────┬───────────────────────────┘
           │ HTTPS (optional reverse proxy)
           ▼
┌──────────────────────────────────────┐
│  webadmin (unprivileged user)         │
│  • net/http static file server      │
│  • /api/* REST handlers             │
│  • /ws  WebSocket telemetry stream  │
│  • /ipc Unix socket client          │
└──────────┬───────────────────────────┘
           │ Unix domain socket (SO_PEERCRED)
           ▼
┌──────────────────────────────────────┐
│  roothelper (root)                    │
│  • rc-service / rc-status           │
│  • /sbin/apk (read-only info)       │
│  • /bin/kill (signals)              │
│  • /etc shadow (never read)           │
└──────────────────────────────────────┘
```

## 6. Request Flow

1. Browser requests `index.html` → served from `go:embed` by `net/http.FileServer` equivalent.
2. Frontend establishes WebSocket to `/ws`.
3. Telemetry goroutine pushes `/proc` snapshots to WebSocket hub every 2 s.
4. Frontend calls REST API (e.g., `POST /api/services/nginx/restart`).
5. `webadmin` validates session + CSRF, forwards via IPC to `roothelper`.
6. `roothelper` validates caller UID, executes exact absolute binary path, returns status.
7. `webadmin` returns JSON response to browser.

## 7. Lifecycle Diagrams

### WebAdmin Server Lifecycle

```
[Start]
  │
  ▼
[Load Config] ──error──► [Fatal Log + Exit]
  │
  ▼
[Init Logger]
  │
  ▼
[Init Session Store]
  │
  ▼
[Init IPC Client] ──error──► [Fatal Log + Exit]
  │
  ▼
[Start Telemetry Collector]
  │
  ▼
[Mount HTTP Handlers]
  │
  ▼
[Listen TCP :8443]
  │
  ▼
[Wait SIGTERM/SIGINT]
  │
  ▼
[Shutdown HTTP (timeout 5s)]
  │
  ▼
[Close IPC Connection]
  │
  ▼
[Stop Telemetry Collector]
  │
  ▼
[Exit 0]
```

### RootHelper Lifecycle

```
[Start]
  │
  ▼
[Open Unix Socket /run/webadmin/ipc.sock]
  │
  ▼
[Set Permissions 0660, chgrp webadmin]
  │
  ▼
[Listen] ◄──────────────────────────────┐
  │                                     │
  ▼                                     │
[Accept]                                │
  │                                     │
  ▼                                     │
[SO_PEERCRED: UID == webadmin?] ──no──► [Log + Close + Listen]
  │ yes                                 │
  ▼                                     │
[Read IPC Request]                      │
  │                                     │
  ▼                                     │
[Validate Command Whitelist] ──no──────► [Error Response + Close + Listen]
  │ yes                                 │
  ▼                                     │
[Execute via exec.Cmd (absolute path)]   │
  │                                     │
  ▼                                     │
[Write IPC Response]                    │
  │                                     │
  ▼                                     │
[Close Client Conn] ────────────────────┘
```

### WebSocket Lifecycle

```
[HTTP Upgrade Request]
  │
  ▼
[Hijack net.Conn]
  │
  ▼
[Send HTTP 101 Switching Protocols]
  │
  ▼
[Register conn in Hub (channel map)]
  │
  ▼
[Spawn Read Goroutine]    [Spawn Write Goroutine]
  │                              │
  │◄───── ping/pong ─────────────┤
  │                              │
  ├─── text frame (telemetry) ──►│
  │                              │
  ▼                              ▼
[Close received]           [Hub broadcast ends]
  │                              │
  ▼                              ▼
[Unregister conn]          [Flush + Close Conn]
  │
  ▼
[WaitGroup Done]
```

## 8. Memory Safety Considerations

- **No CGO**: Entirely Go memory management; no unsafe C interop.
- **Bounded Concurrency**: HTTP server uses `MaxHeaderBytes` and request body limit middleware. Max WebSocket connections capped (configurable, default 10).
- **Reusable Buffers**: Telemetry collector uses `sync.Pool` for JSON encode buffers. `pkg/proc` parsers read into fixed-size `bufio.Reader` buffers.
- **No Shell Parsing**: All external invocations use `exec.Cmd` with explicit `Path` and `Args`; no `sh -c`.
- **Input Validation**: All API inputs validated against strict regex allowlists before IPC serialization.
- **Unix Socket Permissions**: Socket file created with `0660`, owned by root, group `webadmin`. `roothelper` refuses connections with UID mismatch.
- **Session Storage**: In-memory map with TTL to prevent unbounded growth. No persistent session database.

## 9. Configuration Management Strategy

- **Source**: Single JSON file at `/etc/webadmin/config.json`.
- **Schema**:
  - `listen` (string): TCP bind address, default `":8443"`.
  - `tls_cert`, `tls_key` (strings): Optional TLS. If omitted, plain HTTP (expect reverse proxy).
  - `ipc_socket` (string): Unix socket path, default `/run/webadmin/ipc.sock`.
  - `session_ttl` (int): Seconds, default 3600.
  - `ws_max_conns` (int): Max concurrent WebSocket clients, default 10.
  - `rate_limit_rps` (int): Per-IP requests/second, default 20.
  - `allowed_helpers` ([]string): Absolute paths `roothelper` may execute. Must include `/sbin/rc-service`, `/sbin/rc-status`, etc.
- **Behavior**: Loaded once at startup. Invalid configs cause immediate fatal exit with structured error log.

## 10. Frontend Asset Embedding Strategy

- `internal/frontend/assets/` contains:
  - `index.html`
  - `alpine.min.js` (v3.x, ~15 KB)
  - `app.css`
  - `app.js`
- `//go:embed all:assets` into `internal/frontend/embed.go` as `embed.FS`.
- `net/http` handlers strip the `assets/` prefix and serve with `http.ServeContentFS`.
- All frontend routing is hash-based (`/#/services`) to avoid server-side routing complexity.
- ETag support via `http.ServeContentFS` natively; no extra caching layer.

## 11. Logging Architecture

- **Backend**: `pkg/log` emits newline-delimited JSON to `os.Stderr`.
- **Fields**: `ts` (RFC3339), `lvl`, `msg`, `pkg`, `fn`, plus contextual request fields.
- **Integration**: OpenRC captures stderr to `/var/log/webadmin/current` when using ` supervise-daemon` or `logger` pipe.
- **Levels**: `DEBUG` disabled at compile time via build tag `-tags prod` (optional constant optimization).
- **Request Logging**: Single line per HTTP request with status, duration, method, path, IP. No response body logging.

## 12. Build & Artifact Strategy

- `CGO_ENABLED=0`
- `GOOS=linux GOARCH=amd64`
- `-ldflags="-s -w"` for stripped binaries
- Separate `go build` for `cmd/webadmin` and `cmd/roothelper`
- Total artifact size target: < 10 MB combined

## 13. Alpine Linux Compatibility Notes

- **Init System**: OpenRC, not systemd. Init scripts use `start-stop-daemon` or `supervise-daemon`.
- **musl libc**: CGO_ENABLED=0 avoids any libc dependency entirely.
- **User/Group**: Package creates `webadmin` user (`adduser -S -D -H webadmin`).
- **Filesystem**: `/run` is tmpfs. Socket path must be created in init script before daemon start.
- **BusyBox**: Some utilities are BusyBox applets. Always use absolute paths to full binaries (e.g., `/sbin/rc-service`) rather than relying on `$PATH`.
- **APK Packaging**: `APKBUILD` defines `install=` triggers to create user, directories, and set permissions.

---

*This document is implementation-ready. All downstream design decisions in `ipc.md`, `security.md`, `telemetry.md`, `memory.md`, and `deployment.md` are subordinate to this architecture.*

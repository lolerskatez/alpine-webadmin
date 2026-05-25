# Alpine WebAdmin — Final Architecture Review

## System Overview

Alpine WebAdmin is a two-process administrative system for Alpine Linux. It provides a web-based management interface for services, packages, users, storage, network, logs, and system power control. The architecture prioritizes security, minimal resource usage, and operational resilience.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           User Browser                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                 │
│  │  HTTPS/WSS   │  │   SSE Logs   │  │   Static     │                 │
│  │  Telemetry   │  │   Stream     │  │   Assets     │                 │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘                 │
│         │               │               │                              │
└─────────┼───────────────┼───────────────┼────────────────────────────┘
          │               │               │
          ▼               ▼               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  webadmin (unprivileged)                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────┐  │
│  │ HTTP Server  │  │  WebSocket   │  │   Telemetry  │  │   IPC    │  │
│  │  :8443 TLS   │  │    /ws       │  │  Collector   │  │  Client  │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └────┬─────┘  │
│         │               │               │               │             │
│         └───────────────┴───────────────┘               │             │
│                     Session Store                         │             │
│                   (in-memory, TTL)                        │             │
└────────────────────────────┬────────────────────────────┘             │
                             │ Unix domain socket                          │
                             ▼                                             │
┌─────────────────────────────────────────────────────────────────────────┐
│  roothelper (root)                                                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────┐  │
│  │  IPC Server  │  │  OpenRC Mgr  │  │   APK Mgr    │  │ ConfigTx │  │
│  │    Unix      │  │  rc-service  │  │    apk cmd   │  │   lbu    │  │
│  │   Socket     │  │  rc-update   │  │   (mutex)    │  │  backup  │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  └──────────┘  │
│                                                                        │
│  Security: SO_PEERCRED, command whitelist, audit log, timeouts       │
└─────────────────────────────────────────────────────────────────────────┘
```

## Component Breakdown

### webadmin (Unprivileged)

| Concern | Implementation | Rationale |
|---------|---------------|-----------|
| **HTTP Server** | `net/http` with custom mux | Minimal, no external router deps |
| **TLS** | `crypto/tls` | Native Go, no CGO |
| **WebSocket** | Custom `pkg/ws` (RFC 6455) | ~2KB, no gorilla/websocket dependency |
| **Sessions** | In-memory map + TTL sweeper | No database dependency |
| **Auth** | bcrypt + CSRF double-submit | Industry standard, minimal code |
| **Rate Limiting** | Per-IP failed login tracker | Prevents brute force |
| **IPC** | Unix domain socket client | Zero network exposure for roothelper |
| **Telemetry** | `/proc` parsers + WebSocket hub | Real-time, low overhead |
| **Frontend** | Embedded `embed.FS` | Single binary, no external assets |

### roothelper (Root)

| Concern | Implementation | Rationale |
|---------|---------------|-----------|
| **IPC Server** | Unix socket + `SO_PEERCRED` | Only webadmin UID can connect |
| **OpenRC** | `pkg/openrc` wrappers | Safe service control |
| **APK** | `pkg/apk` with global mutex | Prevents concurrent package ops |
| **ConfigTx** | Staging + backup + rollback | Safe config changes with recovery |
| **Audit** | Structured JSON logging | Accountability |
| **Timeouts** | Context with deadline | Prevents hung operations |

## Data Flow

### Authentication Flow

```
User ──POST /api/login──> webadmin
                             │
                             ▼
                    ┌──────────────┐
                    │ bcrypt.Compare
                    │ Generate SID
                    │ Set __Host-SID
                    │ Set __Host-CSRF
                    └──────┬───────┘
                           │
                           ▼
User <──204 No Content─────┘

All subsequent requests:
User ──GET /api/services──> webadmin
                              │
                              ▼
                    ┌──────────────┐
                    │ Read __Host-SID
                    │ Lookup session
                    │ Verify CSRF token
                    │ Verify role
                    └──────┬───────┘
                           │
                           ▼
                        IPC Call
                           │
                           ▼
                        roothelper
```

### Telemetry Flow

```
Collector (2s ticker)
    │
    ├── /proc/stat ──> CPU %
    ├── /proc/meminfo ──> Memory
    ├── /proc/loadavg ──> Load
    ├── /proc/diskstats ──> Disk deltas
    ├── /proc/net/dev ──> Network
    ├── /proc/mounts + statfs ──> Storage
    └── /sys/class/thermal ──> Temperature
    │
    ▼
JSON encode ──> Hub.Broadcast()
    │
    ▼
WebSocket /ws ──> Browser (Alpine.js)
```

## Security Boundaries

| Boundary | Mechanism | Trust Level |
|----------|-----------|-------------|
| Browser ↔ webadmin | TLS 1.2+ + Session Cookie | Untrusted → Authenticated |
| webadmin ↔ roothelper | Unix socket + SO_PEERCRED | Trusted (same host) |
| roothelper ↔ System | Subprocess + validation | Root privilege |
| User input ↔ Commands | Whitelist validation | Sanitized |

## Performance Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| Binary size (webadmin) | ~15MB | With embedded assets, stripped |
| Binary size (roothelper) | ~8MB | Stripped |
| Idle RSS (webadmin) | ~20MB | In-memory sessions + goroutines |
| Idle RSS (roothelper) | ~10MB | Minimal state |
| Telemetry CPU | <1% | 2s intervals, efficient parsers |
| Frontend payload | ~27KB gzipped | Single HTML + CSS + JS + Alpine |
| WebSocket latency | <10ms | Local Unix socket IPC |
| Max concurrent WS | 50 (configurable) | Hub connection cap |

## Scalability Limits

| Resource | Limit | Reason |
|----------|-------|--------|
| Concurrent sessions | ~10,000 | In-memory map, ~64B per session |
| WebSocket clients | Configurable (default 50) | File descriptors, goroutines |
| Package operations | 1 at a time | Global APK mutex |
| API request size | 1MB | `MaxHeaderBytes` + body limits |
| Log retention | Disk space | No built-in rotation (use logrotate) |

## Failure Modes

| Failure | Impact | Mitigation |
|---------|--------|------------|
| webadmin crash | No web UI | OpenRC auto-restart; roothelper unaffected |
| roothelper crash | No mutating ops | Read-only telemetry still works; auto-restart |
| IPC timeout | API 503 | Context deadline; client retry |
| APK lock contention | API 429 | Queue size limit; client polling |
| WS disconnect | No live telemetry | Auto-reconnect after 3s |
| Disk full | Log loss | Logrotate + monitoring |
| Session store full | Rejection | TTL sweeper + max sessions |

## Code Quality Metrics

| Metric | Value |
|--------|-------|
| Go packages | 12 |
| Go source files | ~50 |
| Total Go LOC | ~6,000 |
| Test files | ~20 |
| Test coverage | ~75% (estimated) |
| External dependencies | 1 (`golang.org/x/crypto`) |
| CGO required | No |
| Frontend dependencies | 1 (Alpine.js, embedded) |

## Design Decisions & Trade-offs

### Why Two Processes?

**Option**: Single-process with setuid
**Rejected**: Setuid is complex, error-prone, and hard to audit
**Chosen**: Split architecture with IPC
**Benefit**: webadmin runs unprivileged; roothelper has minimal attack surface via Unix socket

### Why Custom WebSocket?

**Option**: gorilla/websocket
**Rejected**: ~100KB dependency, more features than needed
**Chosen**: Custom `pkg/ws` (~2KB)
**Benefit**: Smaller binary, no external dep, sufficient for text/binary frames

### Why In-Memory Sessions?

**Option**: Redis / SQLite / file
**Rejected**: Additional dependency, persistence risk on diskless
**Chosen**: In-memory map with TTL
**Benefit**: Zero deps, fast, sessions lost on restart (acceptable for admin tool)

### Why Alpine.js (Not React/Vue)?

**Option**: React + Vite build pipeline
**Rejected**: ~130KB+ JS, build complexity, SPA routing issues
**Chosen**: Alpine.js embedded
**Benefit**: ~39KB runtime, no build step, minimal DOM churn, works without JS (degrades gracefully to login)

### Why No Database?

**Option**: SQLite for users/sessions
**Rejected**: Diskless Alpine systems may lose data; additional backup complexity
**Chosen**: System users (`/etc/passwd`) + in-memory sessions
**Benefit**: Uses existing Alpine user management; no new backup target

## Future Architecture Evolution

### Short Term (v1.x)

- [ ] Role granularity (`RoleOperator`)
- [ ] Session binding to IP/User-Agent
- [ ] WebSocket origin validation
- [ ] Prometheus metrics endpoint
- [ ] Health check endpoint for roothelper

### Medium Term (v2.x)

- [ ] Clustering support (shared session store via Redis)
- [ ] REST API versioning (`/api/v1/`)
- [ ] Plugin architecture for custom commands
- [ ] Audit log streaming to remote SIEM
- [ ] mTLS for IPC (defense in depth)

### Long Term (v3.x)

- [ ] WebAssembly frontend (even smaller runtime)
- [ ] eBPF-based telemetry (lower overhead)
- [ ] Declarative configuration (GitOps)
- [ ] Multi-node orchestration

## Compliance Notes

| Standard | Status | Notes |
|----------|--------|-------|
| CIS Alpine Linux | Partial | Review `/etc` permissions |
| OWASP Top 10 | Mitigated | CSRF, XSS, injection covered |
| GDPR | N/A | No personal data processing |
| SOC 2 | Requires audit | Logging and access controls in place |

## Approval

This architecture review represents the final design for Alpine WebAdmin v1.0.

- **Security**: Reviewed and hardened per `security-review.md`
- **Performance**: Profiled and optimized; see benchmark results
- **Reliability**: Graceful degradation paths documented
- **Operability**: Deployment, upgrade, and recovery guides complete

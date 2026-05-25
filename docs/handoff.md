# Alpine WebAdmin — Session Handoff (May 2026)

## Project Status

Alpine WebAdmin is a two-process Go web administration tool for Alpine Linux, providing service control, package management, user management, storage/network monitoring, log streaming, and system power control. The architecture uses `webadmin` (unprivileged HTTPS/WebSocket server) + `roothelper` (root-privileged IPC daemon). All frontend assets are embedded.

## What Was Completed in This Session

### Frontend UI System (Full)
- `internal/frontend/assets/index.html` — Single HTML template with all 9 features (dashboard, services, packages, network, storage, users, logs, sessions, system)
- `internal/frontend/assets/app.css` — Dark-mode-first dense operational UI (~18KB raw, ~4KB gzipped)
- `internal/frontend/assets/app.js` — Alpine.js `app()` component with WebSocket telemetry, SSE log streaming, package operation polling, alert system, tab lazy-loading (~15KB)
- `Makefile` — Added `deps` target to auto-download `alpine.min.js` from jsdelivr
- `docs/frontend.md` — Full frontend documentation

### Backend API Routes (All Present in `cmd/webadmin/main.go`)
- `/api/services` + `/{name}/{start,stop,restart}` + `/enable` + `/disable`
- `/api/packages` + `/search` + `/info` + `/install` + `/remove` + `/update` + `/upgrade` + `/op`
- `/api/network` (raw `/proc/net/dev`)
- `/api/storage` (raw `df`)
- `/api/users` + `/{user}/password`
- `/api/sessions` + `/{id}` (DELETE)
- `/api/reboot`, `/api/shutdown`
- `/api/logs` (SSE `dmesg -w`)
- `/api/alerts` (placeholder)
- `/ws` (WebSocket telemetry)

### Production Hardening
- Telemetry collector: reused `prevDisks` slice capacity to reduce allocations
- Security headers: CSP, HSTS (TLS-only), X-Frame-Options, X-XSS-Protection, Referrer-Policy
- Auth hardening: `CreateWithLimit`, `CountByUser`, `RevokeAllForUser` in `pkg/auth/auth.go`
- Goroutine leak detection: `pkg/testutil/leak.go` + applied to all `pkg/auth/auth_test.go` tests
- WebSocket stress tests: `pkg/ws/stress_test.go` (50 clients × 100 messages, rapid open/close)
- IPC fuzz tests: `pkg/ipc/fuzz_test.go` (envelope round-trip, read/write, large payload, malformed length, concurrency)
- Telemetry benchmarks: `pkg/telemetry/collector_bench_test.go`

### Documentation
- `docs/security-review.md` — Comprehensive security review with hardening checklist
- `docs/deployment-guide.md` — Production install, OpenRC services, TLS, firewall, logrotate
- `docs/disaster-recovery.md` — 10 recovery scenarios, backup procedures, incident playbook
- `docs/architecture-review.md` — Full system architecture, design decisions, future evolution

## Known Lint / Minor Issues
- `meta[name=theme-color]` in `index.html` line 7 — progressive enhancement for mobile browser chrome; Firefox ignores it harmlessly. Not a functional issue.

## File Inventory (Key)

| File | Purpose | Last Action |
|------|---------|-------------|
| `cmd/webadmin/main.go` | HTTP server, routes, middleware | Added security headers |
| `cmd/roothelper/main.go` | IPC server, OpenRC/APK/ConfigTx dispatch | Integrated OpenRC + APK |
| `pkg/auth/auth.go` | Sessions, bcrypt, CSRF, rate limiting | Added session limits |
| `pkg/auth/auth_test.go` | Auth tests | Added leak checks + limit tests |
| `pkg/ipc/ipc.go` | IPC protocol types | Extended for OpenRC + APK |
| `pkg/ipc/fuzz_test.go` | IPC fuzz testing | Created |
| `pkg/ipc/client.go` | IPC client | Unchanged |
| `pkg/ipc/server.go` | IPC server (SO_PEERCRED) | Unchanged |
| `pkg/openrc/openrc.go` | OpenRC service management | Unchanged |
| `pkg/openrc/openrc_test.go` | OpenRC tests | Unchanged |
| `pkg/apk/apk.go` | APK package manager (mutex) | Unchanged |
| `pkg/apk/apk_test.go` | APK tests | Unchanged |
| `pkg/apk/parser.go` | APK output parsers | Unchanged |
| `pkg/configtx/*.go` | Config transaction engine | Unchanged |
| `pkg/telemetry/collector.go` | `/proc` telemetry collector | Reused slice capacity |
| `pkg/telemetry/collector_bench_test.go` | Benchmarks | Created |
| `pkg/telemetry/hub.go` | WebSocket broadcast hub | Unchanged |
| `pkg/proc/*.go` | `/proc` parsers | Unchanged (already optimized) |
| `pkg/ws/*.go` | Custom WebSocket implementation | Unchanged |
| `pkg/ws/stress_test.go` | WebSocket stress tests | Created |
| `pkg/security/*.go` | CSRF, client IP, rate limiting | Unchanged |
| `pkg/log/*.go` | Structured logger | Unchanged |
| `internal/frontend/embed.go` | `//go:embed all:assets` | Unchanged |
| `internal/frontend/assets/index.html` | Frontend HTML | Complete |
| `internal/frontend/assets/app.css` | Frontend CSS | Complete |
| `internal/frontend/assets/app.js` | Frontend JS | Complete |
| `internal/frontend/assets/alpine.min.js` | Alpine.js runtime (placeholder) | Fetch via `make deps` |
| `Makefile` | Build targets | Added `deps` target |
| `go.mod` | Module definition | Unchanged |

## Open Items / Next Steps (Suggested)

### High Priority
1. **Real build + test** — Run `make deps && make build && make test` to verify everything compiles
2. **Alpine.js embed** — `make deps` downloads Alpine.js; verify `alpine.min.js` is >1000 bytes after download
3. **Config validation** — `cmd/webadmin/main.go` references `loadPasswordHash()` reading `/etc/webadmin/passwd`; verify this path matches deployment guide

### Medium Priority
4. **WebSocket origin validation** — Security review recommends adding `Origin` header check in `cmd/webadmin/main.go` WebSocket handler
5. **WebSocket frame size limit** — Add max frame size check in `pkg/ws/frame.go` or `conn.go`
6. **Session binding** — Optionally bind sessions to IP/User-Agent hash for anti-hijacking
7. **Prometheus metrics** — Add `/metrics` endpoint if desired

### Low Priority
8. **Light mode toggle** — Add `data-theme="light"` to CSS and toggle in UI
9. **i18n** — Extract strings from `app.js` and `index.html` to JSON locale files
10. **APK progress streaming** — Current polling works; consider WebSocket push for package ops

## Architecture Reminder

```
Browser ──HTTPS/WSS──> webadmin (unprivileged, :8443)
                              │
                              └── IPC (Unix socket)
                                    │
                                    ▼
                              roothelper (root)
                                    │
                                    ├── OpenRC (rc-service, rc-update)
                                    ├── APK (apk add/del)
                                    └── ConfigTx (staging + lbu)
```

## Quick Commands

```bash
# Build everything
make deps && make build

# Run tests (skip stress tests)
go test ./... -short

# Run all tests including stress
make test

# Production build
make build-release

# Verify frontend assets embedded
go list -f '{{.EmbedFiles}}' ./internal/frontend
```

## Contacts / References

- Security review: `docs/security-review.md`
- Deployment guide: `docs/deployment-guide.md`
- Disaster recovery: `docs/disaster-recovery.md`
- Architecture review: `docs/architecture-review.md`
- Frontend docs: `docs/frontend.md`

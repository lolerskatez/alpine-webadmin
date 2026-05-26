# Alpine WebAdmin — Session Handoff (May 2026)

## Testbench Fixes — Late-May 2026 Session

The following issues were discovered while running on a real Alpine testbench and were fixed. Many of these changes require both binaries (`webadmin` + `roothelper`) **and** the deployed `/etc/webadmin/config.json` to be updated.

### Package Manager
- **Search returned empty descriptions / versions** — `pkg/apk/apk.go` `Search()` now uses `apk search -v`, allows empty query (returns full index), and cross-references the installed list to populate `Installed: true`. `pkg/apk/parser.go` `parseSearch()` extracts version from the `name-version` head.
- **64 KiB IPC cap rejected full package listing** — `pkg/ipc/ipc.go` `MaxMessageSize` bumped from 64 KiB → **8 MiB** (Alpine has ~10k packages). Test `pkg/ipc/ipc_test.go::TestReadMessageTooLarge` updated to use `MaxMessageSize+1`.
- **5-second IPC timeout too short for full listing** — `cmd/webadmin/main.go` introduced `forwardIPCWithTimeout`; `/api/packages` and `/api/packages/search` now use a 60 s deadline. `cmd/roothelper/main.go` `handlePackageSearch` deadline bumped 10 s → 55 s.
- **Packages UI** — Replaced the single "Installed" button with a 3-way filter (All / Available / Installed) and added client-side pagination (50/page) with a Load More button and `Showing X of Y` counter. Default view is **All**.

### System / Power
- **Reboot & Shutdown returned `ErrCapabilityDenied`** — Roothelper now uses `broker.resolveAllowed(candidates...)` to pick the first whitelisted path that exists (tries `/sbin/reboot`, `/bin/reboot`, etc.). `etc/config.json` now includes `/sbin/reboot`, `/sbin/poweroff`, `/bin/reboot`, `/bin/poweroff`.
- **System Information card empty** — Frontend was binding to `telemetry.*`, but WebSocket telemetry only carries `hostname`. Added `sysInfo` state + `loadSystemInfo()` that fetches `/api/system`; card now shows Hostname, OS, Kernel, CPU Cores, Total Memory, Uptime, Version. Added `formatUptime()` helper.

### Storage
- **`/api/storage` returned 503** — `df -h` exits non-zero when it cannot stat Docker overlay paths even though stdout is valid. `cmd/webadmin/main.go` now captures stdout/stderr separately and returns the listing as long as stdout is non-empty (errors logged as `Warn`).
- **Binary path lookups** — Added `findBin(name, candidates...)` helper used for `df`, `getent`, `tail`, `dmesg` so the unprivileged `webadmin` user finds the right binary without relying on `$PATH`.

### Log Streaming (privileged via roothelper)
- **"Log stream disconnected" / `ERR_INCOMPLETE_CHUNKED_ENCODING`** — `webadmin` cannot read `/var/log/messages` (root:adm 0640) and cannot run `dmesg -w` (needs CAP_SYSLOG). Now routed through `roothelper`:
  - New file `cmd/roothelper/logstream.go` — listens on a dedicated Unix socket (`/run/webadmin/logs.sock`, root:webadmin 0660), verifies peer UID via `SO_PEERCRED`, spawns `tail -F /var/log/messages` (or `dmesg -w` fallback) as root, copies stdout to the connection, kills child on disconnect.
  - `cmd/roothelper/main.go` starts `serveLogStream` in a goroutine alongside the IPC server (non-fatal).
  - `cmd/webadmin/main.go` `/api/logs` now just dials `cfg.LogsSocket` and forwards lines as SSE; sends `: ping` heartbeats every 15 s; closes upstream when client disconnects.
  - `pkg/config/config.go` — new `LogsSocket` field with `DefaultLogsSocket = "/run/webadmin/logs.sock"`.
  - `etc/config.json` — documents the new field.

### OpenRC / Logging
- **No log file existed for diagnostics** — `init/openrc/webadmin` and `init/openrc/roothelper` now set `output_log` and `error_log` so structured logs land in `/var/log/webadmin.log` and `/var/log/roothelper.log`.
- **`webadmin` user permissions** — `setup.sh::create_system_user()` now adds `webadmin` to the `adm` group as a defense-in-depth measure (no longer strictly required after log streaming was moved to roothelper, but harmless).

### Required Testbench Actions After Pulling These Changes

```bash
./setup.sh build
doas cp bin/webadmin bin/roothelper /usr/sbin/

# Update deployed config (the in-repo etc/config.json is NOT auto-copied if /etc/webadmin/config.json exists)
doas tee /etc/webadmin/config.json >/dev/null <<'EOF'
{
  "listen": ":8080",
  "ipc_socket": "/run/webadmin/ipc.sock",
  "logs_socket": "/run/webadmin/logs.sock",
  "session_ttl": 3600,
  "ws_max_conns": 10,
  "rate_limit_rps": 20,
  "allowed_helpers": [
    "/sbin/rc-service", "/sbin/rc-status", "/sbin/rc-update", "/sbin/apk",
    "/bin/rc-service",  "/bin/rc-status",  "/bin/rc-update",  "/bin/apk",
    "/sbin/reboot", "/sbin/poweroff", "/bin/reboot", "/bin/poweroff"
  ]
}
EOF

# Refresh init scripts (logging additions)
doas cp init/openrc/webadmin init/openrc/roothelper /etc/init.d/
doas chmod 755 /etc/init.d/webadmin /etc/init.d/roothelper
doas touch /var/log/webadmin.log /var/log/roothelper.log
doas chown webadmin:webadmin /var/log/webadmin.log

doas rc-service roothelper restart
doas rc-service webadmin restart
```

## Project Status

Alpine WebAdmin is a two-process Go web administration tool for Alpine Linux, providing service control, package management, user management, storage/network monitoring, log streaming, and system power control. The architecture uses `webadmin` (unprivileged HTTPS/WebSocket server) + `roothelper` (root-privileged IPC daemon). All frontend assets are embedded.

**Status**: ✅ **READY FOR TESTING** — Complete with robust setup script and error handling

## What Was Completed in This Session

### Setup & Deployment (Complete)
- `setup.sh` — Comprehensive Alpine Linux setup script with robust error handling
  - Automatic Go 1.22+ installation (from repository or source)
  - Automatic dependency installation with error checking
  - Automatic binary building
  - Automatic deployment and service configuration
  - Graceful fallbacks for missing packages
  - Clear error messages with actionable next steps
- `SETUP_GUIDE.md` — Complete setup and configuration guide
- `SETUP_SUMMARY.md` — Quick reference guide
- `SETUP_VERIFICATION.md` — Comprehensive verification checklist
- `SETUP_ERROR_HANDLING.md` — Error handling documentation
- `SETUP_COMPLETE.txt` — Quick summary

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

### High Priority (Ready for Testing)
1. ✅ **Setup script** — `./setup.sh build` to build binaries, or `sudo ./setup.sh all` to build and deploy (COMPLETE)
2. ✅ **Alpine.js embed** — Setup script downloads Alpine.js automatically; verified >100KB after download (COMPLETE)
3. ✅ **Config validation** — Config paths validated; setup script creates all required directories (COMPLETE)

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

### Using Setup Script (Recommended - Alpine Linux Only)

```bash
# Make script executable
chmod +x setup.sh

# Build binaries on development machine
./setup.sh build

# Deploy to testbench (requires root)
sudo ./setup.sh deploy

# Build and deploy in one step (requires root for deploy phase)
sudo ./setup.sh all
```

**Features**:
- ✅ Automatic Go 1.22+ installation (from repository or source)
- ✅ Automatic dependency installation
- ✅ Automatic Alpine.js download
- ✅ Graceful error handling with clear messages
- ✅ Fallback to source installation if needed
- ✅ Comprehensive error checking

### Manual Build (if setup.sh not available)

```bash
# Download dependencies
make deps

# Build everything
make build

# Run tests (skip stress tests)
go test ./... -short

# Run all tests including stress
make test

# Production build
make build-release

# Verify frontend assets embedded
go list -f '{{.EmbedFiles}}' ./internal/frontend
```

## Setup Documentation

For complete setup instructions and error handling details, see:

- **`SETUP_SUMMARY.md`** — Quick reference (start here)
- **`SETUP_GUIDE.md`** — Detailed setup and configuration
- **`SETUP_VERIFICATION.md`** — Verification checklist
- **`SETUP_ERROR_HANDLING.md`** — Error handling details
- **`SETUP_COMPLETE.txt`** — Quick summary

## Testing Documentation

For testing procedures and checklists, see:

- **`TEST_READINESS.md`** — Complete test plan (Phases 1-5)
- **`TESTBENCH_CHECKLIST.md`** — Step-by-step deployment checklist
- **`SETUP_VERIFICATION.md`** — Verification checklist

## Contacts / References

- Security review: `docs/security-review.md`
- Deployment guide: `docs/deployment-guide.md`
- Disaster recovery: `docs/disaster-recovery.md`
- Architecture review: `docs/architecture-review.md`
- Frontend docs: `docs/frontend.md`

# Alpine WebAdmin

Lightweight server administration platform for Alpine Linux on resource-constrained hardware (Intel Atom, 2 GB RAM, 4 GB eMMC).

## Architecture

- **Language**: Go (CGO_ENABLED=0, single static binaries)
- **Frontend**: Alpine.js embedded via `go:embed`
- **Backend**: `net/http` standard library
- **IPC**: Unix domain socket with `SO_PEERCRED` authentication
- **Telemetry**: Direct `/proc` and `/sys` parsing, pushed via WebSocket

## Prerequisites

- Go 1.22+
- Alpine.js minified build (see below)

## Quick Start

### 1. Download Alpine.js

Before building, place the real Alpine.js into the embedded assets:

```sh
curl -L https://unpkg.com/alpinejs@3.14.3/dist/cdn.min.js \
  -o internal/frontend/assets/alpine.min.js
```

### 2. Build

```sh
# Development
go build -o bin/webadmin ./cmd/webadmin
go build -o bin/roothelper ./cmd/roothelper

# Release (static, stripped)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X github.com/alpine-webadmin/alpine-webadmin/pkg/version.Version=0.1.0" \
  -o bin/webadmin ./cmd/webadmin

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X github.com/alpine-webadmin/alpine-webadmin/pkg/version.Version=0.1.0" \
  -o bin/roothelper ./cmd/roothelper
```

### 3. Configure

Create `/etc/webadmin/config.json`:

```json
{
  "listen": ":8080",
  "ipc_socket": "/run/webadmin/ipc.sock",
  "session_ttl": 3600,
  "ws_max_conns": 10,
  "rate_limit_rps": 20,
  "allowed_helpers": ["/sbin/rc-service", "/sbin/rc-status", "/sbin/apk"]
}
```

Create `/etc/webadmin/passwd` with a bcrypt hash:

```sh
# Generate hash (run on any machine with Go)
go run -e 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { b, _ := bcrypt.GenerateFromPassword([]byte("yourpassword"), bcrypt.DefaultCost); fmt.Println(string(b)) }'
```

### 4. Run

```sh
# As root
./bin/roothelper -config /etc/webadmin/config.json

# As unprivileged user (e.g., webadmin)
./bin/webadmin -config /etc/webadmin/config.json
```

## Documentation

- `docs/SYSTEM_DESIGN.md` — Complete system design (20 sections)
- `docs/architecture.md` — Component overview
- `docs/security.md` — Security model and threat mitigations
- `docs/ipc.md` — IPC protocol definitions and wire format
- `docs/telemetry.md` — Telemetry collection and broadcast design
- `docs/memory.md` — Memory management strategy and targets
- `docs/deployment.md` — Deployment flow and Alpine compatibility

## Structure

```
cmd/webadmin          # Unprivileged HTTP/WebSocket server
cmd/roothelper        # Privileged capability daemon
pkg/config            # Configuration management
pkg/ipc               # IPC wire protocol
pkg/proc              # /proc and /sys parsers
pkg/telemetry         # Metric collection and broadcast hub
pkg/auth              # Session cookie management
pkg/log               # Structured JSON logging
pkg/ws                # Minimal RFC 6455 WebSocket
pkg/security          # CSRF, rate limiting, path sanitization
internal/frontend     # Embedded HTML/JS/CSS assets
init/openrc           # OpenRC init scripts
init/apk              # Alpine packaging (APKBUILD)
```

## Security Model

- `webadmin` runs as unprivileged user; `roothelper` runs as root
- IPC authenticated via `SO_PEERCRED` on Unix socket
- Session cookies are `__Host-SID` (Secure, HttpOnly, SameSite=Strict)
- CSRF double-submit cookie protection
- Per-IP token-bucket rate limiting
- No shell execution; all commands use absolute binary paths
- All privileged operations validated against allowlist

## Status

Implementation complete. Ready for testing and deployment.

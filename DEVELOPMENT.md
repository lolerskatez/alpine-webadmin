# Alpine WebAdmin — Development Guide

## Prerequisites

- Go 1.22 or later
- Alpine.js (downloaded before build)
- Linux environment (or WSL) for /proc testing

## Repository Layout

```
.
├── cmd/
│   ├── webadmin/          # Unprivileged HTTP/WebSocket server
│   └── roothelper/        # Privileged IPC daemon
├── pkg/
│   ├── auth/              # Session cookies + bcrypt
│   ├── config/            # JSON config loading + validation
│   ├── ipc/               # Unix socket IPC protocol
│   ├── log/               # Structured JSON logger
│   ├── proc/              # /proc and /sys parsers
│   ├── security/          # CSRF, rate limiting, sanitization
│   ├── telemetry/         # Metric collection + broadcast hub
│   ├── version/           # Build-time version string
│   └── ws/                # Minimal RFC 6455 WebSocket
├── internal/
│   └── frontend/          # go:embed'd HTML/JS/CSS
├── init/
│   ├── openrc/            # OpenRC init scripts
│   └── apk/               # Alpine packaging
├── etc/                   # Example configs
├── build/                 # Release build script
├── docs/                  # Architecture documentation
└── Makefile
```

## Build

### Development (local)

```sh
make build
# → bin/webadmin, bin/roothelper
```

### Release (static Linux amd64)

```sh
make build-release VERSION=0.1.0
# → bin/webadmin, bin/roothelper (stripped, no debug info)
```

### Manual

```sh
# Before first build, download Alpine.js:
curl -L https://unpkg.com/alpinejs@3.14.3/dist/cdn.min.js \
  -o internal/frontend/assets/alpine.min.js

# Development
CGO_ENABLED=0 go build -o bin/webadmin ./cmd/webadmin
CGO_ENABLED=0 go build -o bin/roothelper ./cmd/roothelper

# Release
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X github.com/alpine-webadmin/alpine-webadmin/pkg/version.Version=0.1.0" \
  -o bin/webadmin ./cmd/webadmin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X github.com/alpine-webadmin/alpine-webadmin/pkg/version.Version=0.1.0" \
  -o bin/roothelper ./cmd/roothelper
```

## Local Testing

### 1. Create config

```sh
mkdir -p /tmp/webadmin/run
cp etc/config.json /tmp/webadmin/config.json
# Edit /tmp/webadmin/config.json to use /tmp/webadmin/run/ipc.sock
```

### 2. Generate password hash

```sh
go run -e 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { b, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost); fmt.Println(string(b)) }' > /tmp/webadmin/passwd
```

### 3. Run roothelper (as your user for testing)

```sh
./bin/roothelper -config /tmp/webadmin/config.json -v
```

### 4. Run webadmin

```sh
./bin/webadmin -config /tmp/webadmin/config.json -v
```

### 5. Open browser

Navigate to `http://localhost:8080`. Login with password `admin`.

## Code Quality

```sh
make fmt    # gofmt -w .
make vet    # go vet ./...
make test   # go test ./...
```

## Binary Size Targets

| Binary | Stripped Size |
|--------|---------------|
| webadmin | ~4.5 MB |
| roothelper | ~2.5 MB |

Verify: `ls -la bin/`

## Memory Targets

| State | Target |
|-------|--------|
| Idle RSS | ≤ 15 MB |
| Peak RSS | ≤ 30 MB |

Set runtime tuning:
```sh
export GOGC=50
export GOMEMLIMIT=40MiB
```

## OpenRC Quick Install (local test)

```sh
sudo make install
sudo rc-update add roothelper default
sudo rc-update add webadmin default
sudo rc-service roothelper start
sudo rc-service webadmin start
```

## Contributing

- No CGO. Ever.
- No external dependencies beyond `golang.org/x/crypto/bcrypt`.
- All imports at top of file.
- All errors wrapped with `fmt.Errorf("pkg: context: %w", err)`.

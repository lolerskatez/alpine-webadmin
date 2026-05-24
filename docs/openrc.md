# Alpine WebAdmin — OpenRC Service Management

## Overview

The `pkg/openrc` package provides structured, safe service management for Alpine Linux's OpenRC init system. It wraps `rc-service`, `rc-status`, and `rc-update` with bounded execution timeouts, structured output parsing, and audit logging.

## Architecture

```
┌────────────────────────────────────┐
│  Admin UI / API                    │
└────────────┬───────────────────────┘
             │
             ▼
┌────────────────────────────────────┐
│  IPC Broker (roothelper)           │
│  ┌──────────────────────────────┐│
│  │  openrc.Manager               ││
│  │  ├─ List()    → rc-status -s ││
│  │  ├─ Status()  → rc-service   ││
│  │  ├─ Start()   → rc-service   ││
│  │  ├─ Stop()    → rc-service   ││
│  │  ├─ Restart() → rc-service   ││
│  │  ├─ Enable()  → rc-update    ││
│  │  ├─ Disable() → rc-update    ││
│  │  └─ Dependencies() → init.d  ││
│  └──────────────────────────────┘│
└────────────────────────────────────┘
```

## Types

### ServiceState

```go
type ServiceState string

const (
    StateStarted  ServiceState = "started"
    StateStopped  ServiceState = "stopped"
    StateCrashed  ServiceState = "crashed"
    StateInBoot   ServiceState = "inboot"
    StateStarting ServiceState = "starting"
    StateStopping ServiceState = "stopping"
    StateUnknown  ServiceState = "unknown"
)
```

### Service

```go
type Service struct {
    Name        string       `json:"name"`
    Description string       `json:"description,omitempty"`
    State       ServiceState `json:"state"`
    Runlevel    string       `json:"runlevel,omitempty"`
    PID         int          `json:"pid,omitempty"`
    Enabled     bool         `json:"enabled"`
    InBoot      bool         `json:"in_boot"`
}
```

### Dependency

```go
type Dependency struct {
    Name        string `json:"name"`
    Type        string `json:"type"` // need, use, after, before, provide
    Description string `json:"description,omitempty"`
}
```

## API

### Manager

```go
mgr := openrc.NewManager(logger)
mgr.SetTimeout(30 * time.Second)
```

| Method | Description | Binary |
|--------|-------------|--------|
| `List()` | All services with state and runlevel | `rc-status -s` |
| `Status(name)` | Detailed status of one service | `rc-service <name> status` |
| `Start(name)` | Start a service | `rc-service <name> start` |
| `Stop(name)` | Stop a service | `rc-service <name> stop` |
| `Restart(name)` | Restart a service | `rc-service <name> restart` |
| `Enable(name)` | Add to default runlevel | `rc-update add <name> default` |
| `Disable(name)` | Remove from all runlevels | `rc-update del <name>` |
| `Dependencies(name)` | Parse init script depend() | `/bin/cat /etc/init.d/<name>` |
| `DetectFailures()` | List crashed/unknown services | composite |

## Safety Guarantees

| Guarantee | Implementation |
|-----------|---------------|
| **No shell** | `exec.CommandContext` with explicit args |
| **Bounded time** | `context.WithTimeout` (default 30s) |
| **No privilege escalation** | `SysProcAttr.Credential` lock |
| **Input validation** | `validateServiceName` rejects shell metacharacters |
| **Audit logging** | Every mutation logged with action + service name |

## Example Usage

### Direct (privileged process)

```go
mgr := openrc.NewManager(logger)

// List all services
svcs, err := mgr.List()
for _, s := range svcs {
    fmt.Printf("%s: %s\n", s.Name, s.State)
}

// Check status
st, err := mgr.Status("sshd")
fmt.Printf("sshd is %s (pid=%d, enabled=%v)\n", st.State, st.PID, st.Enabled)

// Control
if err := mgr.Restart("nginx"); err != nil {
    log.Fatal(err)
}

// Boot management
if err := mgr.Enable("webadmin"); err != nil {
    log.Fatal(err)
}

// Dependencies
deps, _ := mgr.Dependencies("nginx")
for _, d := range deps {
    fmt.Printf("%s: %s\n", d.Type, d.Name)
}
```

### Via IPC (unprivileged → roothelper)

```go
// Client sends:
Envelope{
    Type: "cap.service.control",
    Payload: []byte(`{"name":"nginx","action":"restart"}`),
}

// Roothelper delegates to openrc.Manager and responds:
Envelope{
    Type: "response.ok",
    Payload: []byte(`{"name":"nginx","status":"restarted"}`),
}
```

## Error Handling

| Error | Cause |
|-------|-------|
| `openrc: service name is empty` | Empty name passed |
| `openrc: invalid service name` | Name contains `/;|&`$\n\r` |
| `openrc: rc-status failed` | `rc-status` command failed |
| `openrc: start failed` | Service failed to start |
| `openrc: enable failed` | `rc-update` failed |

## IPC Message Types

| Type | Request | Response |
|------|---------|----------|
| `cap.service.list` | `ServiceListReq{}` | `ServiceListData` |
| `cap.service.status` | `ServiceStatusReq{name}` | `ServiceStatusData` |
| `cap.service.control` | `ServiceControlReq{name,action}` | `ServiceStatusData` |
| `cap.service.enable` | `ServiceEnableReq{name}` | `ServiceStatusData` |
| `cap.service.disable` | `ServiceDisableReq{name}` | `ServiceStatusData` |

## Example API Responses

### Service List

```json
{
  "services": [
    {"name": "crond", "status": "started", "runlevel": "default"},
    {"name": "sshd", "status": "started", "runlevel": "default"},
    {"name": "nginx", "status": "stopped"}
  ]
}
```

### Service Status

```json
{
  "name": "sshd",
  "status": "started",
  "pid": 1234,
  "enabled": true,
  "runlevel": "default"
}
```

### Service Control

```json
{
  "name": "nginx",
  "status": "restarted"
}
```

## Resource Considerations

- Each API call spawns a subprocess; rate-limit at the IPC layer
- `DetectFailures()` performs O(N) status queries; use sparingly
- `Dependencies()` reads and parses init script files; cached at caller if needed
- Default timeout is 30s; network-dependent services may need longer

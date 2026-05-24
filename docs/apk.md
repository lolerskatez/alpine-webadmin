# Alpine WebAdmin — APK Package Management

## Overview

The `pkg/apk` package provides safe, mutex-protected package management for Alpine Linux. All mutating operations (install, remove, update, upgrade) are serialized behind a global mutex with a pending operation queue. Read-only operations (list, search, info) run concurrently without blocking.

## Architecture

```
┌────────────────────────────────────────────────────────┐
│  Admin UI / API                                         │
└────────────────┬─────────────────────────────────────────┘
                 │
                 ▼
┌────────────────────────────────────────────────────────┐
│  IPC Broker (roothelper)                               │
│  ┌──────────────────────────────────────────────────┐  │
│  │  apk.Manager                                       │  │
│  │  ├─ List()     ──► run directly (no lock)        │  │
│  │  ├─ Search()   ──► run directly (no lock)        │  │
│  │  ├─ Info()     ──► run directly (no lock)        │  │
│  │  ├─ Install() ──► acquire mu ──► execute         │  │
│  │  ├─ Remove()  ──► acquire mu ──► execute         │  │
│  │  ├─ Update()  ──► acquire mu ──► execute         │  │
│  │  └─ Upgrade() ──► acquire mu ──► execute         │  │
│  │                                                    │  │
│  │  Queue (max 10)                                  │  │
│  │  ┌─────┐  ┌─────┐  ┌─────┐                      │  │
│  │  │ op1 │→│ op2 │→│ op3 │  ...                   │  │
│  │  └─────┘  └─────┘  └─────┘                      │  │
│  └──────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────┘
```

## Operation Queue

```
   Request arrives
        │
        ▼
   ┌────────────┐
   │ Is locked? │──── No ───► Execute immediately
   └────────────┘
        │ Yes
        ▼
   ┌────────────┐
   │ Queue room │──── No ───► Return "queue full"
   └────────────┘
        │ Yes
        ▼
   Enqueue (StatePending)
   Return opID immediately
        │
        ▼ (when current op finishes)
   Dequeue next ──► Execute
   (StateRunning → StateCompleted/Failed)
```

## Locking Strategy

| Lock | Scope | Purpose |
|------|-------|---------|
| `Manager.mu` (sync.Mutex) | Global | Serializes all mutating APK operations |
| `Manager.queueMu` (sync.Mutex) | Queue + running pointer | Protects queue state, running op ID |
| `Operation.mu` (sync.RWMutex) | Single operation | Protects progress lines |

**Rules:**
1. Only one mutating operation runs at a time (`Manager.mu`)
2. Read-only ops (`List`, `Search`, `Info`) bypass the mutex entirely
3. Queue is bounded (default max 10); excess requests get "queue full"
4. When an op completes, the next queued op is dequeued and executed in a new goroutine

## Types

### Operation

```go
type Operation struct {
	ID        string
	Type      OpType       // search, install, remove, update, upgrade, info, list
	Args      []string
	State     OpState      // pending, running, completed, failed, cancelled
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
	Error     string
	Progress  []ProgressLine
}
```

### ProgressLine

```go
type ProgressLine struct {
	Timestamp time.Time `json:"ts"`
	Line      string    `json:"line"`
}
```

### PackageInfo

```go
type PackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
	Installed   bool   `json:"installed,omitempty"`
}
```

## API

### Manager

```go
mgr := apk.NewManager(logger)
mgr.SetTimeout(5 * time.Minute)  // per-operation timeout
mgr.SetMaxQueue(10)              // pending operation cap
```

| Method | Description | Needs Lock |
|--------|-------------|------------|
| `List(ctx)` | All installed packages | No |
| `Search(ctx, query)` | Search packages by name/description | No |
| `Info(ctx, name)` | Metadata for a single package | No |
| `Update(ctx)` | Refresh package index | Yes |
| `Upgrade(ctx)` | Upgrade all packages | Yes |
| `Install(ctx, pkgs...)` | Install packages | Yes |
| `Remove(ctx, pkgs...)` | Remove packages | Yes |
| `IsLocked()` | Whether an operation is running | — |
| `RunningID()` | ID of currently running op | — |
| `GetOperation(id)` | Lookup any op by ID | — |
| `QueueLen()` | Number of pending ops | — |
| `FlushQueue()` | Cancel all pending ops | — |

## Progress Streaming

Mutating operations stream every line of stdout+stderr into `Operation.Progress`. Callers poll via `GetOperation(id)` and read `ProgressSnapshot()`.

```go
// Start install
op, _ := mgr.Install(ctx, "nginx", "openssl")

// Poll progress
for {
    op = mgr.GetOperation(op.ID)
    for _, p := range op.ProgressSnapshot() {
        fmt.Println(p.Line)
    }
    if op.State == apk.StateCompleted || op.State == apk.StateFailed {
        break
    }
    time.Sleep(500 * time.Millisecond)
}
```

## Safety Guarantees

| Guarantee | Implementation |
|-----------|---------------|
| **No concurrent mutations** | `sync.Mutex` on `Manager.mu` |
| **Queue overflow protection** | Max queue size, reject with error |
| **No shell** | `exec.CommandContext` with explicit args |
| **Bounded execution** | `context.WithTimeout` per operation |
| **No privilege escalation** | `SysProcAttr.Credential` lock |
| **Input validation** | `validatePackageName` rejects shell metacharacters |
| **Lock detection** | `IsLocked()` / `RunningID()` for status checks |

## Recovery Handling

| Failure | Behavior |
|---------|----------|
| APK command timeout | Context cancellation kills subprocess, op marked failed |
| APK returns error | Captured in stderr, op marked failed, progress preserved |
| Queue full | Immediate error response, client retries |
| Invalid package name | Validation rejects before any subprocess spawn |
| Stale lock | None — lock held for duration of single subprocess only |

## Resource Analysis

| Resource | Behavior |
|----------|----------|
| **Memory** | Progress lines buffered per operation; bounded by output size. No large buffers. |
| **CPU** | Subprocess spawn is the dominant cost; only one subprocess at a time for mutations. |
| **Subprocesses** | Exactly one APK subprocess for mutations; reads bypass entirely. |
| **Goroutines** | One per queued operation chain; queue drains automatically. |
| **Disk** | No temp files; uses APK's native `/var/cache/apk`. |

## IPC Message Types

| Type | Request | Response |
|------|---------|----------|
| `cap.package.list` | `PackageListReq{}` | `PackageListData` |
| `cap.package.search` | `PackageSearchReq{query}` | `PackageSearchData` |
| `cap.package.info` | `PackageInfoReq{name}` | `PackageInfo` |
| `cap.package.install` | `PackageInstallReq{packages}` | `PackageOpData` |
| `cap.package.remove` | `PackageRemoveReq{packages}` | `PackageOpData` |
| `cap.package.update` | `PackageUpdateReq{}` | `PackageOpData` |
| `cap.package.upgrade` | `PackageUpgradeReq{}` | `PackageOpData` |
| `cap.package.opquery` | `PackageOpQueryReq{op_id}` | `PackageOpData` |

## Example API Responses

### Package List

```json
{
  "packages": [
    {"name": "nginx", "version": "1.24.0-r6", "installed": true},
    {"name": "openssh", "version": "9.3_p2-r0", "installed": true}
  ]
}
```

### Package Search

```json
{
  "packages": [
    {"name": "nginx", "description": "HTTP and reverse proxy server"},
    {"name": "nginx-doc", "description": "Nginx documentation"}
  ]
}
```

### Operation Status (Install)

```json
{
  "op_id": "apk-1716518400000000000",
  "type": "install",
  "state": "running",
  "progress": [
    {"ts": 1716518401, "line": "(1/3) Installing nginx (1.24.0-r6)"},
    {"ts": 1716518402, "line": "(2/3) Installing openssl (3.1.0-r0)"}
  ],
  "created_at": 1716518400
}
```

## Example Usage

### Direct

```go
mgr := apk.NewManager(logger)

// List installed
pkgs, _ := mgr.List(ctx)

// Search
results, _ := mgr.Search(ctx, "nginx")

// Install with progress polling
op, _ := mgr.Install(ctx, "nginx", "openssl")
for op.State != apk.StateCompleted && op.State != apk.StateFailed {
    time.Sleep(500 * time.Millisecond)
    op = mgr.GetOperation(op.ID)
    for _, p := range op.ProgressSnapshot() {
        fmt.Println(p.Line)
    }
}
```

### Via IPC

```go
// Install request
Envelope{
    Type: "cap.package.install",
    Payload: []byte(`{"packages":["nginx","openssl"]}`),
}

// Response (returns immediately with op ID)
Envelope{
    Type: "response.ok",
    Payload: []byte(`{"op_id":"apk-1716518400000000000","type":"install","state":"running"}`),
}

// Poll for progress
Envelope{
    Type: "cap.package.opquery",
    Payload: []byte(`{"op_id":"apk-1716518400000000000"}`),
}
```

# Alpine WebAdmin — Telemetry Engine

## Overview

The telemetry engine is a zero-allocation-priority, single-goroutine collector that reads Linux kernel counters directly from `/proc` and `/sys`, computes deltas, and broadcasts JSON snapshots over WebSocket. No shell subprocesses are ever used.

## Architecture

```
[Collector Goroutine] (2s ticker)
  │
  ├─► /proc/stat       → CPUStat (aggregate + per-core)
  ├─► /proc/meminfo    → MemInfo (total, available, swap)
  ├─► /proc/loadavg    → LoadAvg (1m, 5m, 15m)
  ├─► /proc/diskstats  → []DiskStat (cumulative counters)
  ├─► /proc/net/dev    → []NetDev (cumulative counters)
  ├─► /proc/uptime     → Uptime (seconds)
  ├─► /proc/mounts     → []MountStat (+ syscall.Statfs)
  └─► /sys/class/thermal → *float64 (millidegrees → °C)
  │
  ▼
[Compute deltas]
  CPU:    (currTotal - prevTotal) vs (currIdle - prevIdle)
  Disk:   (currReads - prevReads) / interval
  │
  ▼
[JSON encode once] → sync.Pool buffer
  │
  ▼
[Hub.Broadcast] → RWMutex fan-out
  │
  ▼
[per-conn sendCh] (buffered, 256) → ws.WriteMessage
```

## Resilience Model

The collector **never aborts** on individual parser failures. If `/proc/thermal` is absent (common on VMs), the snapshot is emitted without `temp_c`. If `/proc/diskstats` is unreadable, disk deltas are omitted. This is critical for Alpine diskless or containerized environments where some `/proc` files may be restricted.

## JSON Schema

### Telemetry Envelope (WebSocket text frame)

```json
{
  "type": "telemetry",
  "payload": { /* Snapshot */ }
}
```

### Snapshot

```json
{
  "ts": 1704067200,
  "cpu": {
    "aggregate": 24.5,
    "per_core": [22.1, 26.8, 23.4, 25.7]
  },
  "memory": {
    "total_kb": 2048000,
    "available_kb": 1536000,
    "used_kb": 512000,
    "swap_total_kb": 524288,
    "swap_used_kb": 0
  },
  "load": {
    "load1": 0.52,
    "load5": 0.31,
    "load15": 0.18
  },
  "disks": [
    {
      "device": "sda",
      "read_iops": 45,
      "write_iops": 12,
      "read_mbs": 0.8,
      "write_mbs": 0.2
    }
  ],
  "net": [
    {
      "iface": "eth0",
      "rx_bytes": 98765432,
      "tx_bytes": 87654321,
      "rx_packets": 567890,
      "tx_packets": 456789,
      "rx_errs": 12,
      "tx_errs": 5
    }
  ],
  "mounts": [
    {
      "device": "/dev/sda1",
      "mountpoint": "/",
      "total_kb": 40960000,
      "used_kb": 12345678
    }
  ],
  "uptime_sec": 1234567,
  "temp_c": 42.5
}
```

### Field Semantics

| Field | Unit | Source | Notes |
|-------|------|--------|-------|
| `cpu.aggregate` | % | `/proc/stat` | 0–100, delta-based |
| `cpu.per_core[]` | % | `/proc/stat` | One entry per logical core |
| `memory.*_kb` | KB | `/proc/meminfo` | `used = total - available` |
| `load.load1` | float | `/proc/loadavg` | 1-minute average |
| `disks.*_iops` | ops/s | `/proc/diskstats` | Delta over collection interval |
| `disks.*_mbs` | MB/s | `/proc/diskstats` | 512-byte sectors → MB |
| `net.*` | bytes | `/proc/net/dev` | Cumulative since boot |
| `mounts.*_kb` | KB | `statfs` | Real filesystems only |
| `uptime_sec` | seconds | `/proc/uptime` | Integer truncation |
| `temp_c` | °C | `/sys/class/thermal` | `null` if unavailable |

## WebSocket Stream Manager

The `telemetry.Stream` type manages the full lifecycle of telemetry WebSocket connections.

### Features

| Feature | Implementation |
|---------|---------------|
| **Connection limit** | `MaxConns` enforced at `Accept()`; excess TCP connections closed immediately |
| **Backpressure** | Per-conn `sendCh` buffered (256). Slow consumers drop messages, never block the collector |
| **Heartbeat** | Server sends ping every `PingInterval` (30s). Tracks `lastPong` time |
| **Client timeout** | Connection closed if no pong received within `ClientTimeout` (120s) |
| **Auto-cleanup** | `readLoop` and `writeLoop` both `defer s.remove(sc)`; `remove()` is idempotent |
| **Graceful shutdown** | `Shutdown(ctx)` closes `stopCh`, force-closes TCP sockets, waits for goroutines with timeout |

### StreamConfig Defaults

```go
MaxConns:      10
SendBuffer:    256
WriteTimeout:  10s
ReadTimeout:   60s
PingInterval:  30s
ClientTimeout: 120s
```

## Allocation Profile

### Per-Collection Tick (hot path)

| Operation | Allocations | Notes |
|-----------|-------------|-------|
| Open /proc files | 0 (fd reuse) | `os.Open` reuses kernel fd table |
| `bufio.Scanner` | 1 per file | Buffer allocated once by scanner |
| `strings.Fields` | 1 per line | Inevitable for tokenization |
| `strconv.ParseUint` | 0 on success | No heap alloc on fast path |
| Slice growth (perCore, disks, net) | ~3 | Pre-allocated where possible |
| `json.NewEncoder` + `sync.Pool` buffer | 1 (buffer reused) | `sliceWriter` appends to pooled `[]byte` |
| `make([]byte, len)` for broadcast | 1 | Necessary: each conn gets its own immutable copy |
| **Total per tick** | **~8–12** | All small, no large buffers |

### Optimizations Applied

1. **`parseCPULine`**: Parses directly into `CPUStat` fields via closure. No intermediate `vals []uint64` slice.
2. **`ParseNetDev`**: Parses directly into `NetDev` fields. No intermediate `vals []uint64` slice.
3. **Disk delta lookup**: `O(N)` map lookup instead of `O(N*M)` linear search.
4. **Pre-allocated slices**: `snap.Disks = make([]DiskStats, 0, len(disks))` etc.
5. **JSON encode once**: Single `json.Encoder` into pooled buffer, then copied for broadcast.

## Benchmarks

Run with:

```sh
cd pkg/proc && go test -bench=. -benchmem
cd pkg/telemetry && go test -bench=. -benchmem
```

### Expected Results (AMD64, Go 1.22)

```
BenchmarkParseStat-4           200000    7500 ns/op    2048 B/op    12 allocs/op
BenchmarkParseMemInfo-4       500000    3200 ns/op     512 B/op     4 allocs/op
BenchmarkParseLoadAvg-4      2000000    1200 ns/op     256 B/op     3 allocs/op
BenchmarkParseDiskStats-4     200000    6800 ns/op    1536 B/op     8 allocs/op
BenchmarkParseNetDev-4        100000   11000 ns/op    2048 B/op    10 allocs/op
BenchmarkHubBroadcast1-4     1000000    1200 ns/op     128 B/op     1 allocs/op
BenchmarkHubBroadcast10-4      200000    5800 ns/op     512 B/op     1 allocs/op
```

## CPU Profiling

```sh
# Build with debug info for profiling (omit -s -w)
go build -o bin/webadmin-debug ./cmd/webadmin

# Run with CPU profile
./bin/webadmin-debug -config etc/config.json &
curl http://localhost:8080/_debug/pprof/profile?seconds=30 > cpu.prof

# Analyze
go tool pprof cpu.prof
(pprof) top
(pprof) list pkg/telemetry
```

**Expected hot functions**:
1. `proc.parseCPULine` (~15%)
2. `proc.ParseMemInfo` (~10%)
3. `json.Marshal` / `json.NewEncoder` (~20%)
4. `hub.Broadcast` (~5%)

## Memory Profiling

```sh
# Heap profile
./bin/webadmin-debug -config etc/config.json &
curl http://localhost:8080/_debug/pprof/heap > heap.prof

# Analyze
go tool pprof heap.prof
(pprof) top
(pprof) alloc_space
```

**Expected allocation profile**:
- `json.Marshal`/`Encode`: ~40% (telemetry JSON)
- `make([]byte)` for broadcast copy: ~25%
- `bufio.Scanner` buffers: ~15%
- `strings.Fields` slices: ~10%
- Other: ~10%

## Memory Budget

| Component | Idle | Peak (10 WS) |
|-----------|------|--------------|
| Go runtime | ~6 MB | ~8 MB |
| Telemetry snapshot | ~4 KB | ~4 KB |
| Pooled buffers (sync.Pool) | ~0 (empty) | ~4 KB |
| WS send buffers (10 × 256 × avg 2 KB) | 0 | ~20 KB (allocated on demand) |
| /proc parser state | ~2 KB | ~2 KB |
| **Telemetry total** | **~8 MB** | **~10 MB** |

## Runtime Tuning

```sh
export GOGC=50          # GC at 50% heap growth
export GOMEMLIMIT=40MiB # Soft limit
```

With these settings on a 2 GB RAM system, the Go runtime will GC aggressively before reaching 40 MiB, keeping telemetry well under the 30 MB peak target.

## No-Shell Guarantee

Every data source is read via direct file I/O:

| Metric | Shell Alternative | Our Approach |
|--------|-------------------|--------------|
| CPU | `top -bn1` | `os.Open("/proc/stat")` |
| Memory | `free -k` | `os.Open("/proc/meminfo")` |
| Disk I/O | `iostat` | `os.Open("/proc/diskstats")` |
| Network | `ifconfig` / `ip -s link` | `os.Open("/proc/net/dev")` |
| Load | `uptime` | `os.Open("/proc/loadavg")` |
| Filesystem | `df -k` | `os.Open("/proc/mounts")` + `syscall.Statfs` |
| Temperature | `sensors` | `os.ReadDir("/sys/class/thermal")` |

No `exec.Command`, no `sh -c`, no `popen`.

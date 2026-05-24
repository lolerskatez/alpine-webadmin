# Alpine WebAdmin — Telemetry Architecture

## 1. Design Principles

- **Zero external dependencies**: No `node_exporter`, no `telegraf`, no shell scripts.
- **Direct kernel interface**: Read `/proc` and `/sys` via Go file I/O.
- **Push model**: Server pushes telemetry to connected WebSocket clients on a fixed interval. No client polling.
- **Bounded history**: Maintain only the latest sample in memory; no time-series database.

## 2. Data Sources

All parsers live in `pkg/proc`. Each parser accepts an `io.Reader` and returns a flat struct, enabling unit testing with strings.

| Source | File Path | Parsed Fields |
|--------|-----------|---------------|
| CPU aggregate | `/proc/stat` | user, nice, system, idle, iowait, irq, softirq, steal, guest, total_jiffies |
| CPU per-core | `/proc/stat` | Same as above, per-core lines |
| Memory | `/proc/meminfo` | MemTotal, MemFree, MemAvailable, Buffers, Cached, SwapTotal, SwapFree |
| Load | `/proc/loadavg` | load1, load5, load15, running, total |
| Uptime | `/proc/uptime` | uptime_sec, idle_sec |
| Disk I/O | `/proc/diskstats` | per-device reads, writes, sectors, ms |
| Network | `/proc/net/dev` | per-interface rx/tx bytes, packets, errs, drop |
| Thermal | `/sys/class/thermal/thermal_zone*/temp` | millidegrees C (optional) |
| Filesystem | `/proc/mounts` + `unix.Statfs` | per-mount total, free, avail bytes |

## 3. Collection Cycle

```
[Telemetry Goroutine] (interval = 2s)
  │
  ▼
[Read /proc/stat] ──► [Calculate CPU % per core] ──► [Store in snapshot]
[Read /proc/meminfo] ──► [Normalize to KB] ──► [Store in snapshot]
[Read /proc/loadavg] ──► [Parse floats] ──► [Store in snapshot]
[Read /proc/diskstats] ──► [Delta since last read] ──► [Store in snapshot]
[Read /proc/net/dev] ──► [Store in snapshot]
  │
  ▼
[Marshal snapshot to JSON] (using sync.Pool buffer)
  │
  ▼
[Broadcast to Hub]
  │
  ▼
[Hub fans out to all registered WebSocket conns]
```

### CPU Calculation

```
prev_total = prev_user + prev_nice + prev_system + prev_idle + ...
curr_total = curr_user + curr_nice + curr_system + curr_idle + ...
diff_idle  = curr_idle - prev_idle
diff_total = curr_total - prev_total
cpu_pct    = 100.0 * (1.0 - float64(diff_idle)/float64(diff_total))
```

Previous sample stored in Telemetry struct field; no external state.

### Disk I/O Calculation

Diskstats are cumulative since boot. Telemetry stores the previous raw values and computes deltas:

```
read_iops  = (curr_reads - prev_reads) / interval
write_iops = (curr_writes - prev_writes) / interval
```

## 4. Snapshot Struct

```go
type Snapshot struct {
    Timestamp   int64       `json:"ts"`
    CPU         CPUStats    `json:"cpu"`
    Memory      MemStats    `json:"memory"`
    Load        LoadStats   `json:"load"`
    Disks       []DiskStats `json:"disks,omitempty"`
    Net         []NetStats  `json:"net,omitempty"`
    UptimeSec   int64       `json:"uptime_sec"`
    TempC       *float64    `json:"temp_c,omitempty"`
}

type CPUStats struct {
    Aggregate float64   `json:"aggregate"`
    PerCore   []float64 `json:"per_core,omitempty"`
}

type MemStats struct {
    TotalKB     int64 `json:"total_kb"`
    AvailableKB int64 `json:"available_kb"`
    UsedKB      int64 `json:"used_kb"`
    SwapTotalKB int64 `json:"swap_total_kb"`
    SwapUsedKB  int64 `json:"swap_used_kb"`
}

type LoadStats struct {
    Load1  float64 `json:"load1"`
    Load5  float64 `json:"load5"`
    Load15 float64 `json:"load15"`
}

type DiskStats struct {
    Device    string `json:"device"`
    ReadIOPS  uint64 `json:"read_iops"`
    WriteIOPS uint64 `json:"write_iops"`
    ReadMBs   float64 `json:"read_mbs"`
    WriteMBs  float64 `json:"write_mbs"`
}

type NetStats struct {
    Interface string `json:"iface"`
    RxBytes   uint64 `json:"rx_bytes"`
    TxBytes   uint64 `json:"tx_bytes"`
    RxPackets uint64 `json:"rx_packets"`
    TxPackets uint64 `json:"tx_packets"`
    RxErrs    uint64 `json:"rx_errs"`
    TxErrs    uint64 `json:"tx_errs"`
}
```

## 5. WebSocket Broadcast Hub

```go
type Hub struct {
    conns map[*Conn]struct{} // guarded by mu
    mu    sync.RWMutex
}

func (h *Hub) Broadcast(msg []byte) {
    h.mu.RLock()
    defer h.mu.RUnlock()
    for c := range h.conns {
        select {
        case c.sendCh <- msg:
        default:
            // slow consumer; drop message, do not block telemetry loop
        }
    }
}
```

- **Message Rate**: 1 message per 2 seconds (configurable).
- **Max Conns**: Default 10. Excess connections receive HTTP 503 on upgrade.
- **Backpressure**: Per-conn send channel is buffered (256 messages). If full, messages are dropped. This protects the telemetry goroutine from slow/broken clients.

## 6. WebSocket Message Format

Each telemetry push is a single WebSocket **text** frame containing JSON:

```json
{
  "type": "telemetry",
  "payload": { /* Snapshot struct */ }
}
```

Heartbeat is implicit via regular telemetry pushes. No separate ping/pong frames required for data liveness, but the WebSocket implementation still handles RFC 6455 ping/pong for connection health.

## 7. Error Handling

| Scenario | Behavior |
|----------|----------|
| `/proc` file missing | Log WARN once; omit metric from snapshot; other metrics continue. |
| Parse failure on single file | Log ERROR with filename; omit metric; continue. |
| Thermal zone absent | `TempC` omitted from JSON (null field omitted). |
| WebSocket client unread | Message dropped after buffer full; client eventually timed out and closed. |
| No WebSocket clients | Telemetry collection continues; broadcast is no-op. |

## 8. Frontend Consumption

Alpine.js binds to a reactive `telemetry` object:

```html
<div x-data="{ telemetry: {} }"
     x-init="initWS($data)">
  <span x-text="telemetry.memory?.used_kb + ' KB used'"></span>
</div>
```

`initWS` opens `wss?://${location.host}/ws`, sets `telemetry = JSON.parse(event.data).payload` on each `message` event.

## 9. Testing Strategy

- `pkg/proc` unit tests use `strings.NewReader` with captured `/proc` snapshots.
- `pkg/telemetry` uses a mock `Hub` interface to verify broadcast calls.
- No integration tests against live `/proc` in CI; tests run on any Linux host or with fixture data.

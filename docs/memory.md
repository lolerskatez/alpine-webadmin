# Alpine WebAdmin — Memory Management Strategy

## 1. Targets

| Scenario | Target |
|----------|--------|
| Idle (no clients) | ≤ 15 MB RSS |
| Peak (10 concurrent WS + active API) | ≤ 30 MB RSS |
| Binary size (stripped, each) | ≤ 5 MB |

## 2. Go Runtime Tuning

The following environment variables and build flags are recommended for the target hardware:

- `GOGC=50` — Lower GC target to reclaim memory faster at the cost of slightly more CPU. Appropriate for 2 GB RAM.
- `GOMEMLIMIT=40MiB` — Soft memory limit; Go runtime will GC more aggressively before reaching this.
- Build flags: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w"`

## 3. Allocation Hotspots & Mitigations

### HTTP Static File Serving

- **Problem**: `http.ServeFile` may buffer or stat every request.
- **Mitigation**: Use `io.CopyN` from `embed.FS` directly, or serve small assets from memory. Since all assets are embedded, there are no `os.Open` syscalls per request. Go's `embed.FS` provides `io.ReaderAt` interfaces suitable for zero-copy serving.

### JSON Encoding

- **Problem**: `json.Marshal` allocates a new `[]byte` for every response.
- **Mitigation**: Use `json.NewEncoder(w).Encode(v)` which streams directly to `http.ResponseWriter` without intermediate buffers for most cases.
- **Telemetry**: Because telemetry JSON is broadcast to N WebSocket clients, we encode once into a pooled `[]byte`, then reuse that slice for all fan-outs.

### WebSocket Frame Writing

- **Problem**: Each WebSocket text frame requires a frame header + mask.
- **Mitigation**: Pre-allocate a `[]byte` of `len(msg)+14` (max frame header), reuse via `sync.Pool`. No per-frame heap allocation in steady state.

### /proc Parsing

- **Problem**: Repeated `os.ReadFile` calls allocate large buffers.
- **Mitigation**: Use `os.Open` + `bufio.NewReaderSize(r, 4096)`. Parse line-by-line, writing into pre-allocated struct fields. Reuse the `bufio.Reader` via `sync.Pool` if needed. Do not read entire `/proc/stat` into a single string on every tick.

### IPC Messages

- **Problem**: JSON encode/decode per request allocates.
- **Mitigation**: IPC messages are small (< 1 KB). Acceptable allocation. Use `json.Encoder`/`json.Decoder` over the `net.Conn` directly rather than intermediate buffers.

### Session Store

- **Problem**: In-memory map can grow unbounded if clients generate sessions without expiry.
- **Mitigation**: TTL sweeper evicts expired entries every 5 minutes. Max sessions implicitly bounded by `session_ttl / login_frequency`. No persistent storage means memory is freed on restart.

## 4. sync.Pool Usage

```go
var telemetryBufPool = sync.Pool{
    New: func() interface{} {
        b := make([]byte, 0, 4096)
        return &b
    },
}

func getBuf() *[]byte { return telemetryBufPool.Get().(*[]byte) }
func putBuf(b *[]byte) {
    *b = (*b)[:0]
    telemetryBufPool.Put(b)
}
```

Pools:

| Pool | Item | Use |
|------|------|-----|
| `telemetryBufPool` | `[]byte` | JSON encode of telemetry snapshot |
| `wsFramePool` | `[]byte` | WebSocket frame assembly |
| `bufioReaderPool` | `*bufio.Reader` | `/proc` file parsing |

## 5. Goroutine Budget

| Component | Goroutines | Notes |
|-----------|------------|-------|
| HTTP server | 1 per request + accept | Managed by `net/http` |
| Telemetry collector | 1 | Ticker loop |
| WebSocket Hub | 1 + 2 per conn | Hub broadcaster + read/write per conn |
| Session sweeper | 1 | `time.Ticker` |
| Rate limiter sweeper | 1 | `time.Ticker` |
| RootHelper accept | 1 + 1 per request | Accept loop + handler goroutine |

**Max expected**: ~30 goroutines at peak. Go scheduler overhead is negligible on Atom x5-E8000.

## 6. Connection & Buffer Limits

| Resource | Limit | Enforcement |
|----------|-------|-------------|
| HTTP request body | 1 MB | `http.MaxBytesReader` wrapper |
| HTTP header size | 1 MB | `http.Server.MaxHeaderBytes` |
| WebSocket frame | 65,536 bytes | Hard reject in `pkg/ws` |
| WebSocket per-message | 65,536 bytes | Same as frame (no fragmentation support needed for telemetry) |
| Concurrent WS conns | 10 | `http.Error(503)` on upgrade if cap reached |
| IPC message | 65,536 bytes | `roothelper` closes conn if exceeded |

## 7. Memory Profiling & Verification

- Build with `go build` (no extra flags) and run with `GODEBUG=gctrace=1` to verify GC frequency.
- Use `runtime.ReadMemStats` exposed on a debug endpoint (`/_debug/mem`) gated to localhost-only.
- Acceptance test: `ps -o rss= -p <pid>` after 5 minutes idle must read ≤ 15360 KB.

## 8. Alpine musl Considerations

- `CGO_ENABLED=0` means no musl dependency; binary runs on any Linux kernel.
- If CGO were enabled, musl's allocator differs from glibc; but since it is disabled, only Go's `runtime` allocator applies.
- Go's `MADV_DONTNEED` behavior on Linux releases pages back to OS promptly with `GOGC=50`, which is important on a 2 GB RAM system.

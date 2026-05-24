# Alpine WebAdmin — IPC Protocol Definitions

## 1. Transport

- **Type**: `AF_UNIX` stream socket (SOCK_STREAM).
- **Path**: Configurable; default `/run/webadmin/ipc.sock`.
- **Permissions**: `0660`, owner `root`, group `webadmin`.
- **Max Message Size**: 65536 bytes.
- **Connection Semantics**: One request/response per connection (HTTP-like over stream). `roothelper` closes connection after writing response. This simplifies state management and isolates failures.

## 2. Framing

All messages use a 4-byte big-endian length prefix followed by JSON payload.

```
┌─────────────────┬─────────────────────────────────┐
│ Length (uint32) │ JSON Payload (Length bytes)     │
│ Big Endian      │ UTF-8 encoded                   │
└─────────────────┴─────────────────────────────────┘
```

- If `Length == 0`, connection is treated as closed.
- If `Length > 65536`, receiver closes connection immediately.

## 3. Message Envelope

```go
type Envelope struct {
    Version     int    `json:"v"`     // Protocol version, currently 1
    RequestID   string `json:"rid"`   // UUID v4 correlation ID ( echoed in response )
    MessageType string `json:"type"`  // See below
    Payload     json.RawMessage `json:"payload"`
}
```

## 4. Message Types

### Client → Server (`webadmin` → `roothelper`)

| `type` | Description | `Payload` Struct |
|--------|-------------|------------------|
| `cap.service.list` | List all OpenRC services | `ServiceListReq{}` |
| `cap.service.status` | Get status of one service | `ServiceStatusReq{Name string}` |
| `cap.service.control` | Start/stop/restart | `ServiceControlReq{Name string, Action string}` |
| `cap.system.info` | Read system info | `SystemInfoReq{}` |
| `cap.package.list` | List installed APK packages | `PackageListReq{}` |

### Server → Client (`roothelper` → `webadmin`)

| `type` | Description | `Payload` Struct |
|--------|-------------|------------------|
| `response.ok` | Success | `ResponseOK{Data json.RawMessage}` |
| `response.error` | Failure | `ResponseError{Code string, Message string}` |

## 5. Payload Schemas

### Requests

```go
type ServiceListReq struct{}

type ServiceStatusReq struct {
    Name string `json:"name"`
}

type ServiceControlReq struct {
    Name   string `json:"name"`   // e.g., "nginx"
    Action string `json:"action"` // "start", "stop", "restart", "status"
}

type SystemInfoReq struct{}

type PackageListReq struct{}
```

### OK Responses

```go
type ServiceListData struct {
    Services []ServiceEntry `json:"services"`
}

type ServiceEntry struct {
    Name   string `json:"name"`
    Status string `json:"status"` // "started", "stopped", "crashed"
    Runlevel string `json:"runlevel,omitempty"`
}

type ServiceStatusData struct {
    Name    string `json:"name"`
    Status  string `json:"status"`
    PID     int    `json:"pid,omitempty"`
}

type SystemInfoData struct {
    Hostname    string `json:"hostname"`
    OS          string `json:"os"`
    Kernel      string `json:"kernel"`
    UptimeSec   int64  `json:"uptime_sec"`
    TotalMemKB  int64  `json:"total_mem_kb"`
    CpuCount    int    `json:"cpu_count"`
}

type PackageListData struct {
    Packages []string `json:"packages"`
}
```

### Error Codes

| Code | Meaning |
|------|---------|
| `InvalidRequest` | Malformed JSON or missing fields |
| `CapabilityDenied` | Requested capability not in allowlist |
| `ServiceNotFound` | Service name not known to OpenRC |
| `ActionNotAllowed` | Action string not in {start,stop,restart,status} |
| `ExecutionFailed` | Binary execution returned non-zero exit |
| `InternalError` | Unexpected roothelper failure |

## 6. Wire Example

**Request** (cap.service.control)

```json
{
  "v": 1,
  "rid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "type": "cap.service.control",
  "payload": {
    "name": "nginx",
    "action": "restart"
  }
}
```

Length prefix: `0x000000A2` (162 bytes) + UTF-8 JSON.

**Response** (response.ok)

```json
{
  "v": 1,
  "rid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "type": "response.ok",
  "payload": {
    "name": "nginx",
    "status": "started",
    "pid": 1234
  }
}
```

## 7. Go Implementation Notes

### Codec

```go
// pkg/ipc/codec.go (pseudocode)

func WriteMessage(w io.Writer, env Envelope) error {
    data, _ := json.Marshal(env)
    header := make([]byte, 4)
    binary.BigEndian.PutUint32(header, uint32(len(data)))
    _, err := w.Write(header)
    _, err = w.Write(data)
    return err
}

func ReadMessage(r io.Reader) (Envelope, error) {
    header := make([]byte, 4)
    if _, err := io.ReadFull(r, header); err != nil { return ... }
    length := binary.BigEndian.Uint32(header)
    if length == 0 || length > MaxMessageSize { return ... }
    payload := make([]byte, length)
    if _, err := io.ReadFull(r, payload); err != nil { return ... }
    var env Envelope
    err := json.Unmarshal(payload, &env)
    return env, err
}
```

### Client

```go
// pkg/ipc/client.go (pseudocode)

type Client struct {
    SocketPath string
}

func (c *Client) Call(ctx context.Context, req Envelope) (Envelope, error) {
    d := net.Dialer{}
    conn, err := d.DialContext(ctx, "unix", c.SocketPath)
    if err != nil { return Envelope{}, err }
    defer conn.Close()

    // Set deadline from ctx
    if err := WriteMessage(conn, req); err != nil { return Envelope{}, err }
    return ReadMessage(conn)
}
```

### Server

```go
// pkg/ipc/server.go (pseudocode, used by roothelper)

func Serve(socketPath string, handler func(Envelope) Envelope) error {
    // bind, chmod, listen loop
    // accept -> SO_PEERCRED check -> ReadMessage -> handler -> WriteMessage -> close
}
```

## 8. Versioning

- `Version` field is mandatory. Current version is `1`.
- If `roothelper` receives a version it does not support, it returns `response.error` with `Code: "InvalidRequest"` and closes the connection.
- Future versions may switch to a binary codec (e.g., Protocol Buffers) if JSON overhead becomes measurable. The length-prefix framing is version-agnostic.

## 9. Connection Hygiene

- Each IPC call opens a fresh connection.
- `roothelper` sets a 5-second read deadline on each accepted connection.
- `webadmin` sets a 10-second timeout on the `DialContext`.
- No persistent connections or connection pooling required; Unix socket creation is cheap.

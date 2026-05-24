# Alpine WebAdmin — Security Model

## 1. Threat Model

| Threat | Mitigation |
|--------|------------|
| Privilege escalation via web exploit | Web server runs unprivileged; all privileged ops delegated to `roothelper` via IPC. |
| Unix socket impersonation | `SO_PEERCRED` authentication; `roothelper` rejects any UID other than `webadmin`. |
| Session hijacking | `__Host-SID` cookie (Secure, HttpOnly, SameSite=Strict); 128-bit CSPRNG token. |
| CSRF | Double-submit cookie pattern; all state-changing requests require `X-CSRF-Token` header matching cookie. |
| Replay / brute force | Per-IP token-bucket rate limiter in `pkg/security`. |
| Command injection | No shell execution. `roothelper` invokes exact absolute binary paths from an allowlist. No `PATH` reliance. |
| Path traversal in API | `pkg/security.SanitizePath` rejects `..`, null bytes, and non-ASCII control characters. |
| DoS via large body | All handlers enforce `http.MaxBytesReader` (default 1 MB). |
| DoS via WebSocket | Connection cap enforced; aggressive frame size limit (65 KB max); slowloris mitigated by read deadlines. |

## 2. Privilege Separation

```
┌──────────────┐      Unix Domain Socket       ┌──────────────┐
│   webadmin   │ ◄──────────────────────────► │  roothelper  │
│  UID 1000+   │      /run/webadmin/ipc.sock   │    UID 0     │
│  No caps     │                               │  Full caps   │
└──────────────┘                               └──────────────┘
```

- `webadmin` user has no supplementary groups that grant file or device access.
- `roothelper` socket file is `root:webadmin 0660`.
- `roothelper` executes no user-supplied strings as command arguments. Arguments are validated against a strict allowlist of service names (read from `/etc/webadmin/services.json` at startup) or system commands.

## 3. Authentication

### Session Cookie (`__Host-SID`)

- **Name**: `__Host-SID` (prefix prevents overriding from insecure origins).
- **Value**: 32-byte random (hex) = 64 hex chars, generated via `crypto/rand`.
- **Attributes**:
  - `Secure` (mandatory; if TLS disabled, cookie still set but browser enforces on HTTPS).
  - `HttpOnly`
  - `SameSite=Strict`
  - `Path=/`
  - `Max-Age` derived from config `session_ttl`.
- **Storage**: In-memory `map[string]Session` in `pkg/auth`. No disk persistence.
- **TTL Sweeper**: Background goroutine evicts expired sessions every 5 minutes.

### Login Flow

```
POST /api/login
  ├── Validate rate limit (IP-based)
  ├── Compare bcrypt(password, hash) against /etc/webadmin/passwd (or config)
  ├── Generate SID
  ├── Set __Host-SID cookie
  └── Return 204
```

Password hash is stored in `/etc/webadmin/passwd` as bcrypt string. Default password set at install time; admin must change on first login.

## 4. Authorization

- **Role model**: Single admin role for MVP. All authenticated sessions have full read/write access to allowed capabilities.
- **Capability matrix** (enforced in `roothelper`):

| Capability | Description | Example Binaries |
|------------|-------------|------------------|
| `service.list` | List OpenRC services | `/sbin/rc-status` |
| `service.control` | Start/stop/restart services | `/sbin/rc-service <svc> start\|stop\|restart` |
| `system.info` | Read OS info | direct /proc parsing |
| `package.list` | List installed packages | `/sbin/apk info -v` |

Any request outside the capability matrix returns IPC error `CapabilityDenied`.

## 5. CSRF Protection

1. On first visit (or login), server sets a `__Host-CSRF` cookie with a random 32-byte token.
2. Frontend reads the cookie and sends the same value in `X-CSRF-Token` header for every mutating request (`POST`, `PUT`, `DELETE`, `PATCH`).
3. Server rejects if header does not match cookie.
4. Cookie is `SameSite=Strict`, `HttpOnly=false` (so JS can read it).

## 6. Rate Limiting

- **Algorithm**: Token bucket per client IP.
- **Capacity**: Configurable `rate_limit_rps` (default 20). Burst = 2× capacity.
- **Storage**: In-memory `map[string]*Bucket`; no Redis/external dependency.
- **Eviction**: Periodic sweeper removes stale entries every 60 s.
- **Response**: HTTP 429 with `Retry-After` header.

## 7. IPC Security

- **Transport**: `AF_UNIX` stream socket.
- **Authentication**: Kernel-level `SO_PEERCRED` (`getsockopt` with `SO_PEERCRED` on Linux).
- **Authorization**: Only UID of `webadmin` accepted. Any other UID causes immediate connection close and `WARN` log.
- **Integrity**: Messages are length-prefixed JSON. `roothelper` imposes a max message size of 64 KB. Oversized messages cause connection termination.
- **Confidentiality**: Unix socket permissions (0660) provide OS-level access control. No encryption layer required for local-only socket.

## 8. Network Exposure

- By default, `webadmin` binds to all interfaces (`:8443`). It is expected to be placed behind an HTTPS reverse proxy (nginx/traefik) or accessed via VPN.
- TLS can be enabled natively via config `tls_cert` / `tls_key` for direct exposure.
- `roothelper` binds **only** to Unix domain socket; no TCP exposure.

## 9. Audit & Forensics

- All `roothelper` capability invocations are logged with:
  - Requesting UID (always `webadmin`, but logged for integrity)
  - Capability name
  - Target service/package
  - Success/failure
  - Timestamp
- `webadmin` logs all authentication attempts (success and failure) with source IP.
- No PII beyond IP address and usernames are logged.

## 10. Failure Modes

| Scenario | Behavior |
|----------|----------|
| `roothelper` not running | IPC calls return HTTP 503; UI shows degraded mode (read-only telemetry still works via `pkg/proc`). |
| `roothelper` rejects UID | Connection closed; `webadmin` logs security warning; HTTP 500 to client. |
| Rate limit exceeded | HTTP 429; no IPC leakage. |
| Invalid session | HTTP 401; frontend redirects to login. |
| CSRF mismatch | HTTP 403. |

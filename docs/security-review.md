# Alpine WebAdmin — Security Review & Hardening Checklist

## Executive Summary

This document contains a comprehensive security review of the Alpine WebAdmin system, covering authentication, authorization, IPC, network exposure, and operational hardening. All findings are actionable with remediation steps.

## Threat Model

### Assets

| Asset | Value | Exposure |
|-------|-------|----------|
| WebAdmin HTTPS listener | High | Network-facing |
| Roothelper Unix socket | Critical | Local only |
| Session store (in-memory) | High | Process memory |
| Configuration files | High | Local filesystem |
| IPC channel | Critical | Local only |

### Attack Surfaces

1. **WebAdmin HTTP server** — exposed to network; target for auth bypass, DoS, XSS, CSRF
2. **IPC Unix socket** — local only; target for privilege escalation if compromised
3. **Subprocess execution** — `roothelper` runs as root; command injection is critical
4. **Static assets** — embedded frontend; target for supply-chain / CDN attacks
5. **WebSocket endpoint** — authenticated but real-time; target for resource exhaustion

## Authentication Review

### Password Hashing

```go
// pkg/auth/auth.go
func HashPassword(password string) (string, error) {
    return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}
```

- **Strength**: bcrypt with DefaultCost (~10) is adequate
- **Hardening**: Consider increasing cost to 12 for production if CPU budget allows
- **Verification**: `CheckPassword` uses `bcrypt.CompareHashAndPassword` — constant-time

### Session Management

| Feature | Status | Notes |
|---------|--------|-------|
| Cryptographically random SID | PASS | `crypto/rand` 32 bytes → 64 hex chars |
| Session TTL / expiry | PASS | Configurable TTL with sweeper |
| Sliding window renewal | PASS | Extends expiry on active use |
| Anti-session-fixation | PASS | `CreateWithMetadata` deletes old sessions |
| Secure cookie attributes | PASS | `HttpOnly`, `Secure`, `SameSite=Strict` |
| Cookie prefix | PASS | `__Host-SID` enforces path=/ and Secure |
| CSRF token | PASS | Double-submit cookie pattern |
| Rate limiting | PASS | Per-IP failed login tracker |

### Hardening Recommendations

1. **Session binding**: Bind sessions to IP + User-Agent hash to reduce session hijacking risk
2. **Max concurrent sessions**: Add per-user session limit (e.g., 3 concurrent)
3. **Logout everywhere**: Add API endpoint to invalidate all sessions for a user
4. **Password policy**: Enforce minimum length (12 chars) and complexity in frontend + backend

## Authorization Review

### Role-Based Access Control

```go
const (
    RoleAdmin Role = "admin"
    RoleRead  Role = "read"
)
```

- **Current**: Two roles; `RequireRole` middleware blocks non-admin
- **Gap**: No fine-grained permissions (e.g., read-only on logs but full on services)
- **Recommendation**: For production, consider adding `RoleOperator` for service/package ops without user management

### API Authorization Matrix

| Endpoint | Role Required | CSRF Required |
|----------|---------------|---------------|
| `GET /api/*` | Any authenticated | No |
| `POST /api/services/*` | Admin | Yes |
| `POST /api/packages/*` | Admin | Yes |
| `POST /api/users/*` | Admin | Yes |
| `POST /api/reboot` | Admin | Yes |
| `POST /api/shutdown` | Admin | Yes |
| `DELETE /api/sessions/:id` | Admin | Yes |

## IPC Security

### Unix Domain Socket

- **Path**: Configurable (default `/run/webadmin/roothelper.sock`)
- **Permissions**: Must be `0600` or `0660` with appropriate group
- **SO_PEERCRED**: Verified on every connection — only `webadmin` UID accepted
- **No network exposure**: Unix sockets are not network-routable

### IPC Message Validation

- **Length limit**: 4-byte length header, max 16MB per message
- **JSON envelope**: Version field validated; unknown versions rejected
- **Timeout**: Read/write deadlines on socket operations

### Hardening Recommendations

1. **Socket permissions audit**: Add startup check that socket is not world-readable
2. **Message signing**: Consider HMAC over payload for future defense-in-depth
3. **Rate limiting**: Add per-message-type rate limits in roothelper

## Input Validation

### Service Name Validation

```go
func ValidateServiceName(name string) error {
    if name == "" || len(name) > 64 {
        return fmt.Errorf("invalid service name length")
    }
    for _, r := range name {
        if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
            return fmt.Errorf("invalid character in service name")
        }
    }
    return nil
}
```

- **PASS**: Whitelist-based, no shell metacharacters allowed

### Package Name Validation

- **PASS**: Same pattern as service names; `apk` tool validates further

### User Input Validation

- **Username**: Pattern `[a-z_][a-z0-9_-]*` enforced in frontend and backend
- **Password**: Minimum length enforced via frontend; bcrypt handles arbitrary input safely
- **JSON payloads**: Standard `encoding/json` unmarshal; no custom parsers

## CSRF Protection

### Implementation

- **Token generation**: `crypto/rand` 32 bytes, base64-encoded
- **Storage**: `__Host-CSRF` cookie (SameSite=Strict, HttpOnly=false for JS access)
- **Validation**: Double-submit — cookie value must match `X-CSRF-Token` header
- **Scope**: All mutating endpoints (POST, PUT, DELETE, PATCH)

### Hardening Recommendations

1. **Token rotation**: Rotate CSRF token on privilege change or session renewal
2. **Origin validation**: Verify `Origin` header matches expected host
3. **Referer validation**: Fall back to `Referer` check if `Origin` missing

## WebSocket Security

### Current Protections

- **Authentication required**: Connection rejected if no valid session cookie
- **No origin check**: Should add `Origin` header validation
- **Frame size limit**: No explicit limit set; add max frame size (e.g., 1MB)
- **Message rate limit**: No limit; add per-client max messages/sec

### Hardening Recommendations

1. Add `Origin` validation to WebSocket upgrade handler
2. Add max frame size: reject frames > 1MB
3. Add per-client rate limiter for WebSocket messages
4. Implement WebSocket connection limit per IP

## Subprocess Security

### Command Execution

| Manager | Subprocess | Input Validation | Timeout | Credential Lock |
|---------|-----------|------------------|---------|-----------------|
| `openrc.Manager` | `rc-service`, `rc-update` | Service name whitelist | Yes | Yes |
| `apk.Manager` | `apk` | Package name whitelist | Yes | Yes |
| `configtx` | Various (`cat`, `cp`, etc.) | Path validation | Yes | Yes |

### Command Injection Mitigation

- **No shell execution**: All commands use `exec.Command(name, args...)` directly
- **No user input in shell**: Service/package names validated before use
- **Environment isolation**: No user-controlled env vars passed to subprocesses

## Dependency Security

### Go Module Audit

```bash
go list -m -versions golang.org/x/crypto
```

- **golang.org/x/crypto v0.24.0**: Latest stable; includes bcrypt fixes
- **No other external dependencies**: Minimal attack surface

### Frontend Dependencies

- **Alpine.js v3.14.3**: Fetched from jsdelivr CDN at build time
- **Supply chain risk**: Pin exact version; verify SRI hash in future
- **No runtime CDN**: All assets embedded — no external requests in production

## Network Security

### TLS Configuration

- **Current**: Uses `crypto/tls` with default config
- **Hardening**: Explicitly configure minimum TLS 1.2, cipher suite preference

```go
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
    CipherSuites: []uint16{
        tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
        tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
    },
    PreferServerCipherSuites: true,
}
```

### HTTP Headers

| Header | Status | Recommendation |
|--------|--------|----------------|
| `X-Content-Type-Options` | MISSING | Add `nosniff` |
| `X-Frame-Options` | MISSING | Add `DENY` |
| `Content-Security-Policy` | MISSING | Add strict CSP |
| `Strict-Transport-Security` | MISSING | Add HSTS (if HTTPS) |
| `Referrer-Policy` | MISSING | Add `no-referrer` |

## Operational Security

### File Permissions

| File | Recommended | Notes |
|------|-------------|-------|
| `/etc/webadmin/config.json` | `0640 root:webadmin` | Contains admin password hash |
| `/usr/sbin/webadmin` | `0755 root:root` | SUID not required |
| `/usr/sbin/roothelper` | `0755 root:root` | Runs as root via OpenRC |
| `/run/webadmin/` | `0750 root:webadmin` | Socket directory |

### Process Hardening

1. **webadmin**:
   - Run as dedicated user (`webadmin`)
   - `NoNewPrivileges=true` in service unit
   - `ProtectSystem=strict` (if systemd) or equivalent OpenRC sandbox
   - Drop capabilities: only `CAP_NET_BIND_SERVICE` if binding low port

2. **roothelper**:
   - Runs as root (required for service/package management)
   - `ProtectHome=true`
   - `ProtectKernelTunables=true`
   - Restrict to Unix socket only

## Security Checklist

### Pre-Deployment

- [ ] Change default admin password
- [ ] Generate unique `cookie_secret` in config
- [ ] Enable HTTPS (TLS certificate configured)
- [ ] Set `bind` to specific interface (not `0.0.0.0` if possible)
- [ ] Configure firewall to block port 8443 from untrusted networks
- [ ] Verify Unix socket permissions (`srw-------` or `srw-rw----`)
- [ ] Review `allowed_uids` in roothelper config
- [ ] Enable audit logging in roothelper
- [ ] Set `max_request_size` to reasonable value (e.g., 1MB)
- [ ] Configure log rotation for `/var/log/webadmin/`

### Runtime

- [ ] Monitor failed login attempts (`/var/log/webadmin/access.log`)
- [ ] Monitor roothelper audit log for anomalies
- [ ] Review active sessions periodically (`/api/sessions`)
- [ ] Keep Alpine packages updated (`apk upgrade`)
- [ ] Monitor disk space (logs can grow quickly with audit enabled)
- [ ] Backup `/etc/webadmin/` before config changes

### Post-Incident

- [ ] Revoke all sessions (`/api/sessions` → DELETE each)
- [ ] Rotate `cookie_secret` in config and restart
- [ ] Review roothelper audit log for unauthorized commands
- [ ] Check filesystem integrity (`apk verify`)
- [ ] Rotate TLS certificates if compromise suspected

## Vulnerability Scanning

```bash
# Go vulnerability check
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# Binary hardening check
scanelf -e ./bin/webadmin
scanelf -e ./bin/roothelper

# File permission audit
find /etc/webadmin -type f -perm /o+w
find /usr/sbin -name 'webadmin*' -type f -perm /o+w
```

## Security Contacts

- **Bug reports**: security@alpine-webadmin.local
- **Response SLA**: 48 hours for critical, 7 days for high
- **Disclosure**: Coordinated disclosure preferred; 90-day window

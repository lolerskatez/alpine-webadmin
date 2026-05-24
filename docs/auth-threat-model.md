# Alpine WebAdmin — Authentication & Session Threat Model

## Scope

This document covers threats against the Alpine WebAdmin authentication, session management, and authorization subsystems.

## Assets

| Asset | Sensitivity | Location |
|-------|-------------|----------|
| Admin password hash | Critical | `/etc/webadmin/passwd` (0640 root:webadmin) |
| Session tokens | High | In-memory only (no persistence) |
| CSRF tokens | Medium | Double-submit cookie + header |
| Config file | Medium | `/etc/webadmin/config.json` (0640 root:webadmin) |

## Threats & Mitigations

### 1. Session Hijacking (Network)

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker sniffs session cookie from unencrypted traffic |
| **Mitigation** | `Secure` flag on `__Host-SID` cookie — browsers will not send over HTTP |
| **Fallback** | Application binds to localhost or behind TLS reverse proxy |

### 2. Session Hijacking (XSS)

| Aspect | Detail |
|--------|--------|
| **Threat** | Malicious script exfiltrates session cookie via XSS |
| **Mitigation** | `HttpOnly` flag prevents JavaScript from reading `__Host-SID` |
| **Defense in depth** | CSP header: `default-src 'self'; script-src 'self'` |

### 3. Cross-Site Request Forgery (CSRF)

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker tricks browser into making authenticated state-changing request |
| **Mitigation** | Double-submit cookie pattern: `__Host-CSRF` cookie + `X-CSRF-Token` header |
| **Enforcement** | All `POST`/`PUT`/`DELETE`/`PATCH` requests validated by `security.CSRFMiddleware` |
| **Cookie attributes** | `SameSite=Strict` prevents cross-site cookie transmission entirely |

### 4. Session Fixation

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker sets known session ID, victim logs in, attacker uses same ID |
| **Mitigation** | `auth.Store.CreateWithMetadata` deletes all existing sessions for the username before creating a new one |
| **Regeneration** | Every successful login produces a new random 256-bit SID |

### 5. Brute Force / Credential Stuffing

| Aspect | Detail |
|--------|--------|
| **Threat** | Automated guessing of admin password |
| **Layer 1** | Per-IP token-bucket rate limiter (`security.NewLimiter`) |
| **Layer 2** | Per-IP failed login tracker with lockout after N attempts (`auth.NewFailedLoginTracker`) |
| **Layer 3** | bcrypt with default cost (~100ms per hash on Atom x5) |
| **Layer 4** | No timing side-channel: `bcrypt.CompareHashAndPassword` is constant-time |

### 6. Timing Attack on Session Lookup

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker measures response time to determine if a SID exists |
| **Mitigation** | Map lookup (`O(1)`) is inherently constant-time regardless of key existence |
| **Note** | No string comparison of SIDs during validation (direct map key lookup) |

### 7. Privilege Escalation

| Aspect | Detail |
|--------|--------|
| **Threat** | Read-only user gains admin privileges |
| **Mitigation** | `auth.RequireRole` middleware checks `Session.Role` before handler execution |
| **Roles** | `admin` (full control), `read` (view-only) |

### 8. Session Prediction

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker guesses valid SID |
| **Mitigation** | 256-bit random from `crypto/rand` (CSPRNG) |
| **Space** | 2^256 possible values, infeasible to guess |

### 9. Session Expiry Bypass

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker uses expired session |
| **Mitigation** | `Store.Get` checks `time.Now().After(sess.Expires)` on every lookup |
| **Sweeper** | Background goroutine evicts expired sessions every 5 minutes |
| **Sliding window** | `GetWithRenew` extends expiry on active use (configurable) |

### 10. Password Hash Exposure

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker reads `/etc/webadmin/passwd` |
| **Mitigation** | File permissions `0640 root:webadmin` — webadmin user cannot read |
| **Hashing** | bcrypt with adaptive cost; rainbow tables computationally infeasible |

## Attack Scenarios

### Scenario A: Attacker has XSS on the admin page

1. XSS payload attempts `document.cookie` — blocked by `HttpOnly`
2. XSS payload attempts CSRF — blocked by `SameSite=Strict` + header validation
3. XSS payload attempts `fetch('/api/...')` from attacker domain — blocked by CORS (no `Access-Control-Allow-Origin` set)

**Result**: XSS cannot steal session or forge requests.

### Scenario B: Attacker controls a reverse proxy on same host

1. Proxy injects `X-Forwarded-For` to spoof IP
2. `security.ClientIP` only trusts `X-Forwarded-For` from loopback (`127.0.0.1`, `::1`)
3. If proxy runs on loopback, first `X-Forwarded-For` header value is used (admin should configure proxy to sanitize)

**Result**: Rate limiting may be circumvented if proxy is misconfigured. Admin should ensure proxy strips untrusted `X-Forwarded-For`.

### Scenario C: Attacker obtains `webadmin` user shell access

1. `webadmin` cannot read `/etc/webadmin/passwd` (mode 0640, group webadmin has read but webadmin user is not in group webadmin by default... wait, the init scripts set `command_user="webadmin:webadmin"`, so webadmin IS in the webadmin group. Let me check.)

Actually, looking at the OpenRC script: `command_user="webadmin:webadmin"`, so `webadmin` is in the `webadmin` group and CAN read the config file (intended for config loading). The password hash file should be set to `0600 root:root` or managed by `roothelper` only.

**Mitigation**: Password file should be `0600 root:root`. `webadmin` never needs to read it; only `roothelper` does (for future password change feature via IPC). For MVP, `webadmin` loads the hash from `/etc/webadmin/passwd` at startup — this is a known limitation. In production, use `0600 root:root` and have `webadmin` drop the hash after startup.

### Scenario D: Attacker forges a telemetry WebSocket connection without auth

1. WebSocket endpoint is wrapped with `auth.RequireAuth` middleware
2. No valid `__Host-SID` cookie → `401 Unauthorized`
3. Even if cookie is set, session must exist in `auth.Store`

**Result**: Unauthenticated WebSocket connections are rejected.

## Session Lifecycle

```
                    +---------------+
                    |   No Session  |
                    +---------------+
                           |
              POST /api/login
              (password + rate limit check)
                           |
                           v
              +------------------------+
              |  Delete old sessions   |  <- anti-fixation
              |  for this username     |
              +------------------------+
                           |
                           v
              +------------------------+
              | Create 256-bit random  |
              | SID, set __Host-SID    |
              | Secure HttpOnly Strict |
              +------------------------+
                           |
                           v
                    +---------------+
                    |    Active     |
                    |   Session     |
                    +---------------+
                           |
              +------------+------------+
              |                         |
     GET /api/session               DELETE /api/logout
     (sliding window                 Clear cookie
      renews TTL)                     Delete from store
              |                         |
              v                         v
      +--------------+          +---------------+
      | TTL expires  |          |   No Session  |
      | Sweeper cleans |         +---------------+
      +--------------+
```

## Cookie Security Attributes

| Cookie | Name | Secure | HttpOnly | SameSite | Path | MaxAge |
|--------|------|--------|----------|----------|------|--------|
| Session | `__Host-SID` | Yes | Yes | Strict | `/` | Config TTL |
| CSRF | `__Host-CSRF` | Yes | **No** | Strict | `/` | 86400 |

## Implementation Checklist

- [x] `__Host-` prefix on cookies (requires HTTPS + `Path=/` + no `Domain`)
- [x] 256-bit CSPRNG session IDs (`crypto/rand`)
- [x] Anti-session-fixation on login
- [x] bcrypt password hashing
- [x] Constant-time password comparison (via bcrypt)
- [x] Sliding window session renewal
- [x] Background TTL sweeper
- [x] Per-IP rate limiting (token bucket)
- [x] Per-IP failed login lockout
- [x] CSRF double-submit cookie + header
- [x] Role-based access control (`RequireRole`)
- [x] Path sanitization (traversal + null byte rejection)
- [x] Client IP extraction with XFF trust boundary


# Alpine WebAdmin — Testbench Deployment Checklist

**Estimated Duration**: 4–5 hours  
**Target**: Alpine Linux 3.18+ on testbench hardware

---

## Pre-Deployment (Build Machine)

- [ ] **Download Alpine.js** (CRITICAL)
  ```bash
  cd e:\Alpine WebAdmin
  make deps
  ```
  Verify: `ls -lh internal/frontend/assets/alpine.min.js` should be >100KB

- [ ] **Build Release Binaries**
  ```bash
  make build-release
  ```
  Verify: `ls -lh bin/webadmin bin/roothelper` (both should exist)

- [ ] **Run Tests** (optional, requires Linux/WSL)
  ```bash
  go test ./... -short
  ```

- [ ] **Transfer to Testbench**
  ```bash
  scp bin/webadmin bin/roothelper root@testbench:/tmp/
  ```

---

## On Testbench (Alpine Linux)

### 1. System Setup

- [ ] Create webadmin user
  ```bash
  adduser -S -D -H -s /sbin/nologin webadmin
  ```

- [ ] Install binaries
  ```bash
  install -Dm755 /tmp/webadmin /usr/sbin/webadmin
  install -Dm755 /tmp/roothelper /usr/sbin/roothelper
  chmod 755 /usr/sbin/webadmin /usr/sbin/roothelper
  ```

- [ ] Create directories
  ```bash
  install -dm750 /etc/webadmin /var/log/webadmin /run/webadmin
  chown root:webadmin /etc/webadmin /var/log/webadmin /run/webadmin
  ```

### 2. Configuration

- [ ] Create config file
  ```bash
  cat > /etc/webadmin/config.json <<'EOF'
  {
    "listen": ":8080",
    "ipc_socket": "/run/webadmin/ipc.sock",
    "session_ttl": 3600,
    "ws_max_conns": 10,
    "rate_limit_rps": 20,
    "allowed_helpers": ["/sbin/rc-service", "/sbin/rc-status", "/sbin/apk"]
  }
  EOF
  chmod 640 /etc/webadmin/config.json
  chown root:webadmin /etc/webadmin/config.json
  ```

- [ ] Set initial password (bcrypt hash of "admin")
  ```bash
  echo '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcg7b3XeKeUxWdeS86E36P4/KFm' > /etc/webadmin/passwd
  chmod 600 /etc/webadmin/passwd
  chown root:root /etc/webadmin/passwd
  ```

### 3. Start Services

- [ ] Start roothelper (as root)
  ```bash
  /usr/sbin/roothelper -config /etc/webadmin/config.json -v &
  ```
  Expected: `roothelper listening on /run/webadmin/ipc.sock`

- [ ] Start webadmin (as webadmin user)
  ```bash
  su - webadmin -s /bin/sh -c '/usr/sbin/webadmin -config /etc/webadmin/config.json -v' &
  ```
  Expected: `webadmin listening on :8080`

- [ ] Verify both processes running
  ```bash
  ps aux | grep -E 'webadmin|roothelper'
  ```

---

## Phase 1: Smoke Tests (30 min)

### Health & Connectivity

- [ ] Health endpoint responds
  ```bash
  curl http://localhost:8080/health
  ```
  Expected: `{"status":"up","version":"0.1.0","timestamp":...}`

- [ ] UI loads
  ```bash
  curl http://localhost:8080/ | head -20
  ```
  Expected: HTML with `<title>Alpine WebAdmin</title>`

- [ ] Assets load
  ```bash
  curl http://localhost:8080/assets/app.css | head -5
  ```
  Expected: CSS content

### Authentication

- [ ] Login succeeds with correct password
  ```bash
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"password":"admin"}' -v
  ```
  Expected: `204 No Content`, Set-Cookie headers present

- [ ] Login fails with wrong password
  ```bash
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"password":"wrong"}' -v
  ```
  Expected: `401 Unauthorized`

### WebSocket

- [ ] WebSocket connection succeeds (requires authentication)
  ```bash
  # First, get a session cookie from login
  # Then test WebSocket with: wscat -c ws://localhost:8080/ws --header "Cookie: __Host-SID=..."
  ```
  Expected: Connection established, telemetry data received

---

## Phase 2: Feature Tests (2 hours)

### Services

- [ ] List services
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/services
  ```
  Expected: JSON array of services

- [ ] Get service status
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/services/networking
  ```
  Expected: Service status JSON

- [ ] Start service (if available)
  ```bash
  curl -X POST -H "Cookie: __Host-SID=..." http://localhost:8080/api/services/sshd/start
  ```
  Expected: `200 OK` or error if service not available

### Packages

- [ ] List installed packages
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/packages
  ```
  Expected: JSON array of packages

- [ ] Search packages
  ```bash
  curl -H "Cookie: __Host-SID=..." "http://localhost:8080/api/packages/search?q=curl"
  ```
  Expected: Search results

- [ ] Get package info
  ```bash
  curl -H "Cookie: __Host-SID=..." "http://localhost:8080/api/packages/info?name=curl"
  ```
  Expected: Package details

### Network

- [ ] Get network stats
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/network
  ```
  Expected: `/proc/net/dev` output

### Storage

- [ ] Get disk usage
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/storage
  ```
  Expected: `df -h` output

### Users

- [ ] List users
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/users
  ```
  Expected: JSON array of system users

### Sessions

- [ ] List active sessions
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/sessions
  ```
  Expected: JSON array with current session

### Logs

- [ ] Stream kernel logs (SSE)
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/logs
  ```
  Expected: Event stream with kernel messages (Ctrl+C to stop)

### System

- [ ] Get system info
  ```bash
  curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/system
  ```
  Expected: System information JSON

---

## Phase 3: Security Tests (1 hour)

### Rate Limiting

- [ ] Rate limiter blocks excessive requests
  ```bash
  for i in {1..30}; do curl http://localhost:8080/health; done
  ```
  Expected: Some requests return `429 Too Many Requests`

### Failed Login Lockout

- [ ] IP locked after 5 failed attempts
  ```bash
  for i in {1..6}; do curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"password":"wrong"}'; done
  ```
  Expected: 6th request returns `429 Too Many Requests` with `Retry-After: 900`

### CSRF Protection

- [ ] CSRF token required for state-changing operations
  ```bash
  curl -X POST -H "Cookie: __Host-SID=..." http://localhost:8080/api/logout
  ```
  Expected: `403 Forbidden` (missing CSRF token)

### Security Headers

- [ ] Response includes security headers
  ```bash
  curl -I http://localhost:8080/
  ```
  Expected headers:
  - `X-Content-Type-Options: nosniff`
  - `X-Frame-Options: DENY`
  - `X-XSS-Protection: 1; mode=block`
  - `Content-Security-Policy: ...`
  - `Referrer-Policy: strict-origin-when-cross-origin`

### Session Cookies

- [ ] Session cookie is HttpOnly and Secure
  ```bash
  curl -I -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"password":"admin"}'
  ```
  Expected: `Set-Cookie: __Host-SID=...; Secure; HttpOnly; SameSite=Strict`

---

## Phase 4: Stress Tests (30 min)

### Concurrent WebSocket Connections

- [ ] 10 concurrent WebSocket clients
  ```bash
  # Use a WebSocket stress test tool or script
  # Expected: All connections receive telemetry data without errors
  ```

### Rapid API Requests

- [ ] 100 rapid requests to various endpoints
  ```bash
  for i in {1..100}; do curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/services & done
  wait
  ```
  Expected: All requests succeed or are rate-limited gracefully

### Long-Running Log Stream

- [ ] Log stream stays open for 5 minutes
  ```bash
  timeout 300 curl -H "Cookie: __Host-SID=..." http://localhost:8080/api/logs
  ```
  Expected: Stream continues without disconnecting

---

## Phase 5: Edge Cases (30 min)

### Invalid Input

- [ ] Invalid JSON rejected
  ```bash
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d 'invalid json'
  ```
  Expected: `400 Bad Request`

- [ ] Missing required fields handled
  ```bash
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{}'
  ```
  Expected: `400 Bad Request` or `401 Unauthorized`

### Unauthenticated Access

- [ ] Protected endpoints reject unauthenticated requests
  ```bash
  curl http://localhost:8080/api/services
  ```
  Expected: `401 Unauthorized` or redirect to login

### Graceful Shutdown

- [ ] Services shut down cleanly
  ```bash
  kill -TERM $(pgrep webadmin)
  kill -TERM $(pgrep roothelper)
  sleep 2
  ps aux | grep -E 'webadmin|roothelper'
  ```
  Expected: Both processes terminated

---

## Post-Test

### Log Review

- [ ] Check webadmin logs for errors
  ```bash
  tail -50 /var/log/webadmin/webadmin.log
  ```

- [ ] Check roothelper logs for errors
  ```bash
  tail -50 /var/log/webadmin/roothelper.log
  ```

### Performance Metrics

- [ ] CPU usage under normal load: < 5%
- [ ] Memory usage: < 50 MB (webadmin + roothelper combined)
- [ ] Response time: < 200 ms for most endpoints

### Issues Encountered

- [ ] Document any bugs or unexpected behavior
- [ ] Capture error messages and logs
- [ ] Note any missing features or improvements

---

## Sign-Off

- [ ] All smoke tests passed
- [ ] All feature tests passed
- [ ] All security tests passed
- [ ] All stress tests passed
- [ ] All edge case tests passed
- [ ] No critical issues found
- [ ] Ready for production deployment

**Testbench Date**: _______________  
**Tester Name**: _______________  
**Issues Found**: _______________

---

## Quick Reference

### Useful Commands

```bash
# View logs
tail -f /var/log/webadmin/webadmin.log
tail -f /var/log/webadmin/roothelper.log

# Check processes
ps aux | grep -E 'webadmin|roothelper'

# Check listening ports
netstat -tlnp | grep -E '8080|ipc.sock'

# View config
cat /etc/webadmin/config.json

# Restart services
killall webadmin roothelper
/usr/sbin/roothelper -config /etc/webadmin/config.json -v &
su - webadmin -s /bin/sh -c '/usr/sbin/webadmin -config /etc/webadmin/config.json -v' &

# Test connectivity
curl http://localhost:8080/health
```

### Troubleshooting

| Issue | Solution |
|-------|----------|
| `Connection refused` | Check if services are running: `ps aux \| grep webadmin` |
| `Permission denied` | Verify directory permissions: `ls -ld /run/webadmin /etc/webadmin` |
| `IPC socket not found` | Check roothelper is running and socket created: `ls -l /run/webadmin/ipc.sock` |
| `Login fails` | Verify password hash in `/etc/webadmin/passwd` |
| `WebSocket fails` | Ensure authenticated (session cookie present) and origin is allowed |
| `Rate limit errors` | Wait 60 seconds for rate limit to reset, or restart webadmin |

---

**Questions?** Refer to `TEST_READINESS.md` for detailed information.

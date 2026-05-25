# Alpine WebAdmin — Setup Verification Checklist

Use this checklist to verify that the setup script and all components are working correctly.

---

## Pre-Setup Verification

### System Check

- [ ] Running Alpine Linux 3.18+
  ```bash
  cat /etc/os-release | grep -E "NAME|VERSION"
  ```
  Expected: `NAME="Alpine Linux"` and `VERSION_ID="3.18"` or higher

- [ ] Internet connectivity
  ```bash
  ping -c 1 8.8.8.8
  ```
  Expected: Successful ping

- [ ] Disk space available
  ```bash
  df -h / | tail -1
  ```
  Expected: At least 500 MB available

- [ ] Root access (for deployment)
  ```bash
  id
  ```
  Expected: `uid=0(root)` for deployment phase

### Script Check

- [ ] setup.sh exists and is executable
  ```bash
  ls -l setup.sh
  ```
  Expected: `-rwxr-xr-x` (executable)

- [ ] setup.sh is readable
  ```bash
  head -5 setup.sh
  ```
  Expected: Shows shebang and comments

---

## Build Phase Verification

### Before Build

- [ ] Go modules present
  ```bash
  ls -l go.mod go.sum
  ```
  Expected: Both files exist

- [ ] Source code present
  ```bash
  ls -d cmd/webadmin cmd/roothelper pkg internal
  ```
  Expected: All directories exist

### During Build

- [ ] Script starts without errors
  ```bash
  ./setup.sh build 2>&1 | head -20
  ```
  Expected: `[INFO]` messages, no `[ERROR]`

- [ ] Go is installed or installed successfully
  ```bash
  go version
  ```
  Expected: `go version go1.22.X linux/amd64` or higher

- [ ] Dependencies are downloaded
  ```bash
  ls -l go.mod go.sum
  ```
  Expected: Files have recent modification time

- [ ] Alpine.js is downloaded
  ```bash
  ls -lh internal/frontend/assets/alpine.min.js
  ```
  Expected: File size > 100 KB

### After Build

- [ ] Binaries are created
  ```bash
  ls -lh bin/webadmin bin/roothelper
  ```
  Expected: Both files exist and are executable

- [ ] Binaries are reasonable size
  ```bash
  ls -lh bin/webadmin bin/roothelper | awk '{print $5}'
  ```
  Expected: webadmin ~4-5 MB, roothelper ~2-3 MB

- [ ] Binaries are static (no dependencies)
  ```bash
  file bin/webadmin bin/roothelper
  ```
  Expected: `ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked`

- [ ] Tests pass
  ```bash
  ./setup.sh build 2>&1 | grep -i "test"
  ```
  Expected: Tests run and complete (failures are OK on non-Linux)

---

## Deployment Phase Verification

### Before Deployment

- [ ] Binaries exist
  ```bash
  ls -l bin/webadmin bin/roothelper
  ```
  Expected: Both files exist and are executable

- [ ] Running as root
  ```bash
  id
  ```
  Expected: `uid=0(root)`

### During Deployment

- [ ] Script starts without errors
  ```bash
  sudo ./setup.sh deploy 2>&1 | head -20
  ```
  Expected: `[INFO]` messages, no `[ERROR]`

- [ ] User is created
  ```bash
  id webadmin
  ```
  Expected: User exists with no shell

- [ ] Directories are created
  ```bash
  ls -ld /etc/webadmin /run/webadmin /var/log/webadmin
  ```
  Expected: All directories exist with correct permissions

### After Deployment

- [ ] Binaries are installed
  ```bash
  ls -l /usr/sbin/webadmin /usr/sbin/roothelper
  ```
  Expected: Both files exist and are executable

- [ ] Configuration exists
  ```bash
  cat /etc/webadmin/config.json
  ```
  Expected: Valid JSON with expected keys

- [ ] Password file exists
  ```bash
  ls -l /etc/webadmin/passwd
  ```
  Expected: File exists with 600 permissions

- [ ] Init scripts exist
  ```bash
  ls -l /etc/init.d/webadmin /etc/init.d/roothelper
  ```
  Expected: Both files exist and are executable

- [ ] Services are enabled
  ```bash
  rc-update show | grep -E 'webadmin|roothelper'
  ```
  Expected: Both services listed in default runlevel

- [ ] Services are running
  ```bash
  rc-service webadmin status
  rc-service roothelper status
  ```
  Expected: `status: started` for both

---

## Functional Verification

### Health Check

- [ ] Health endpoint responds
  ```bash
  curl http://localhost:8080/health
  ```
  Expected: JSON response with `"status":"up"`

- [ ] Response time is acceptable
  ```bash
  time curl http://localhost:8080/health
  ```
  Expected: < 100 ms

### Authentication

- [ ] Login with correct password succeeds
  ```bash
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"password":"admin"}' -v
  ```
  Expected: `204 No Content` with `Set-Cookie` header

- [ ] Login with wrong password fails
  ```bash
  curl -X POST http://localhost:8080/api/login \
    -H "Content-Type: application/json" \
    -d '{"password":"wrong"}' -v
  ```
  Expected: `401 Unauthorized`

### Web UI

- [ ] UI loads
  ```bash
  curl http://localhost:8080/ | grep -c "Alpine WebAdmin"
  ```
  Expected: At least 1 match

- [ ] CSS loads
  ```bash
  curl http://localhost:8080/assets/app.css | head -5
  ```
  Expected: CSS content

- [ ] JavaScript loads
  ```bash
  curl http://localhost:8080/assets/app.js | head -5
  ```
  Expected: JavaScript content

- [ ] Alpine.js loads
  ```bash
  curl http://localhost:8080/assets/alpine.min.js | wc -c
  ```
  Expected: > 100000 bytes

### API Endpoints

- [ ] Services endpoint
  ```bash
  curl -H "Cookie: __Host-SID=test" http://localhost:8080/api/services
  ```
  Expected: JSON response (may be empty or error if not authenticated)

- [ ] Packages endpoint
  ```bash
  curl -H "Cookie: __Host-SID=test" http://localhost:8080/api/packages
  ```
  Expected: JSON response

- [ ] Network endpoint
  ```bash
  curl -H "Cookie: __Host-SID=test" http://localhost:8080/api/network
  ```
  Expected: Network data

- [ ] Storage endpoint
  ```bash
  curl -H "Cookie: __Host-SID=test" http://localhost:8080/api/storage
  ```
  Expected: Disk usage data

### Logs

- [ ] Webadmin logs exist
  ```bash
  ls -l /var/log/webadmin/webadmin.log
  ```
  Expected: File exists with recent content

- [ ] Roothelper logs exist
  ```bash
  ls -l /var/log/webadmin/roothelper.log
  ```
  Expected: File exists with recent content

- [ ] Logs are readable
  ```bash
  tail -5 /var/log/webadmin/webadmin.log
  ```
  Expected: Recent log entries

---

## Performance Verification

### Memory Usage

- [ ] Webadmin memory is reasonable
  ```bash
  ps aux | grep webadmin | grep -v grep | awk '{print $6}'
  ```
  Expected: < 50 MB

- [ ] Roothelper memory is reasonable
  ```bash
  ps aux | grep roothelper | grep -v grep | awk '{print $6}'
  ```
  Expected: < 30 MB

### CPU Usage

- [ ] Webadmin CPU is idle
  ```bash
  ps aux | grep webadmin | grep -v grep | awk '{print $3}'
  ```
  Expected: < 1% when idle

- [ ] Roothelper CPU is idle
  ```bash
  ps aux | grep roothelper | grep -v grep | awk '{print $3}'
  ```
  Expected: < 1% when idle

### Disk Usage

- [ ] Binaries are small
  ```bash
  du -sh /usr/sbin/webadmin /usr/sbin/roothelper
  ```
  Expected: < 10 MB combined

- [ ] Config is small
  ```bash
  du -sh /etc/webadmin
  ```
  Expected: < 1 MB

---

## Security Verification

### File Permissions

- [ ] Config is readable only by owner and group
  ```bash
  ls -l /etc/webadmin/config.json
  ```
  Expected: `-rw-r-----` (640)

- [ ] Password is readable only by owner
  ```bash
  ls -l /etc/webadmin/passwd
  ```
  Expected: `-rw-------` (600)

- [ ] Directories are restricted
  ```bash
  ls -ld /etc/webadmin /run/webadmin /var/log/webadmin
  ```
  Expected: `drwxr-x---` (750)

### Process Permissions

- [ ] Webadmin runs as unprivileged user
  ```bash
  ps aux | grep webadmin | grep -v grep | awk '{print $1}'
  ```
  Expected: `webadmin` (not root)

- [ ] Roothelper runs as root
  ```bash
  ps aux | grep roothelper | grep -v grep | awk '{print $1}'
  ```
  Expected: `root`

### Network

- [ ] Only listening on localhost
  ```bash
  netstat -tlnp | grep 8080
  ```
  Expected: `127.0.0.1:8080` or `0.0.0.0:8080` (depending on config)

---

## Troubleshooting Verification

### If Build Fails

- [ ] Check Go version
  ```bash
  go version
  ```
  Expected: `go1.22.X` or higher

- [ ] Check internet connectivity
  ```bash
  ping -c 1 unpkg.com
  ```
  Expected: Successful ping

- [ ] Check disk space
  ```bash
  df -h /
  ```
  Expected: At least 500 MB available

### If Deployment Fails

- [ ] Check root access
  ```bash
  id
  ```
  Expected: `uid=0(root)`

- [ ] Check binaries exist
  ```bash
  ls -l bin/webadmin bin/roothelper
  ```
  Expected: Both files exist

- [ ] Check permissions on /usr/sbin
  ```bash
  ls -ld /usr/sbin
  ```
  Expected: Writable by root

### If Services Won't Start

- [ ] Check logs
  ```bash
  tail -50 /var/log/webadmin/webadmin.log
  tail -50 /var/log/webadmin/roothelper.log
  ```

- [ ] Check port availability
  ```bash
  netstat -tlnp | grep 8080
  ```

- [ ] Check socket availability
  ```bash
  ls -l /run/webadmin/ipc.sock
  ```

---

## Summary

### Build Verification
- [ ] Script runs without errors
- [ ] Go is installed
- [ ] Dependencies are downloaded
- [ ] Alpine.js is downloaded
- [ ] Binaries are created
- [ ] Binaries are static
- [ ] Tests pass

### Deployment Verification
- [ ] Script runs without errors
- [ ] User is created
- [ ] Directories are created
- [ ] Binaries are installed
- [ ] Configuration is created
- [ ] Password is set
- [ ] Init scripts are installed
- [ ] Services are enabled
- [ ] Services are running

### Functional Verification
- [ ] Health endpoint responds
- [ ] Authentication works
- [ ] Web UI loads
- [ ] API endpoints respond
- [ ] Logs are created

### Performance Verification
- [ ] Memory usage is acceptable
- [ ] CPU usage is acceptable
- [ ] Disk usage is acceptable

### Security Verification
- [ ] File permissions are correct
- [ ] Process permissions are correct
- [ ] Network access is restricted

---

**All checks passed?** You're ready for testing! See `TEST_READINESS.md` for the test plan.

# Alpine WebAdmin — Setup Guide

Complete guide for building and deploying Alpine WebAdmin on Alpine Linux.

---

## Quick Start

### Option 1: Build and Deploy in One Step (on testbench)

```bash
sudo ./setup.sh all
```

This will:
1. Install Go 1.22+ (if needed)
2. Download dependencies
3. Download Alpine.js
4. Build binaries
5. Create system user and directories
6. Install binaries
7. Create configuration
8. Start services

### Option 2: Build on Development Machine, Deploy on Testbench

**On development machine:**
```bash
./setup.sh build
```

**Transfer binaries to testbench:**
```bash
scp bin/webadmin bin/roothelper root@testbench:/tmp/
```

**On testbench:**
```bash
sudo ./setup.sh deploy
```

---

## Prerequisites

### System Requirements

- **OS**: Alpine Linux 3.18+
- **Architecture**: x86_64, aarch64, or armv6l
- **RAM**: 512 MB minimum (1 GB recommended)
- **Disk**: 100 MB for binaries and config
- **Network**: Internet access (for downloading Go and Alpine.js)

### Required Tools

- `apk` (Alpine package manager)
- `curl` or `wget` (for downloads)
- `git` (for cloning/pulling)
- Root access (for deployment)

---

## Detailed Setup Instructions

### Phase 1: Build

#### 1.1 Prerequisites Check

Ensure you have Alpine Linux 3.18+:

```bash
cat /etc/os-release
```

Expected output includes `NAME="Alpine Linux"` and `VERSION_ID="3.18"` or higher.

#### 1.2 Run Build Script

```bash
cd /path/to/alpine-webadmin
chmod +x setup.sh
./setup.sh build
```

The script will:
- Detect your architecture
- Install Go 1.22+ (if needed)
- Install build dependencies (build-base, git, curl, wget, pkgconfig)
- Download Go modules
- Download Alpine.js
- Build webadmin and roothelper binaries
- Run tests

#### 1.3 Verify Build

Check that binaries were created:

```bash
ls -lh bin/webadmin bin/roothelper
```

Expected output:
```
-rwxr-xr-x 1 user group 4.5M May 25 00:00 bin/webadmin
-rwxr-xr-x 1 user group 2.5M May 25 00:00 bin/roothelper
```

### Phase 2: Deployment

#### 2.1 Transfer Binaries (if built elsewhere)

From development machine:
```bash
scp bin/webadmin bin/roothelper root@testbench:/tmp/
```

#### 2.2 Run Deployment Script

On testbench (as root):

```bash
cd /path/to/alpine-webadmin
sudo ./setup.sh deploy
```

The script will:
- Verify binaries exist
- Create `webadmin` system user
- Create directories:
  - `/etc/webadmin` (config)
  - `/run/webadmin` (runtime)
  - `/var/log/webadmin` (logs)
- Install binaries to `/usr/sbin`
- Create `/etc/webadmin/config.json`
- Create `/etc/webadmin/passwd` (with default password)
- Install OpenRC init scripts
- Enable services at boot
- Start services

#### 2.3 Verify Deployment

Check that services are running:

```bash
rc-service webadmin status
rc-service roothelper status
```

Expected output:
```
 * status: started
```

Check that binaries are installed:

```bash
ls -l /usr/sbin/webadmin /usr/sbin/roothelper
```

Check that config exists:

```bash
cat /etc/webadmin/config.json
```

Check logs:

```bash
tail -f /var/log/webadmin/*
```

---

## Configuration

### Default Configuration

The setup script creates `/etc/webadmin/config.json`:

```json
{
  "listen": ":8080",
  "ipc_socket": "/run/webadmin/ipc.sock",
  "session_ttl": 3600,
  "ws_max_conns": 10,
  "rate_limit_rps": 20,
  "allowed_helpers": ["/sbin/rc-service", "/sbin/rc-status", "/sbin/apk"]
}
```

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `listen` | `:8080` | HTTP listen address (host:port) |
| `ipc_socket` | `/run/webadmin/ipc.sock` | Unix socket for IPC |
| `session_ttl` | `3600` | Session timeout in seconds (5 min - 24 hours) |
| `ws_max_conns` | `10` | Max WebSocket connections (1-100) |
| `rate_limit_rps` | `20` | Rate limit in requests/second (1-1000) |
| `tls_cert` | (empty) | Path to TLS certificate (optional) |
| `tls_key` | (empty) | Path to TLS key (optional) |
| `allowed_helpers` | (array) | Absolute paths to allowed helper binaries |

### Production Configuration

For production, edit `/etc/webadmin/config.json`:

```json
{
  "listen": ":8443",
  "tls_cert": "/etc/webadmin/cert.pem",
  "tls_key": "/etc/webadmin/key.pem",
  "ipc_socket": "/run/webadmin/ipc.sock",
  "session_ttl": 3600,
  "ws_max_conns": 50,
  "rate_limit_rps": 20,
  "allowed_helpers": ["/sbin/rc-service", "/sbin/rc-status", "/sbin/apk"]
}
```

Then restart services:

```bash
rc-service webadmin restart
rc-service roothelper restart
```

---

## Password Management

### Default Password

The setup script creates a default password hash (bcrypt of "admin"):

```bash
cat /etc/webadmin/passwd
```

### Change Password

Generate a new bcrypt hash:

```bash
# Using Go (if available)
go run -e 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { b, _ := bcrypt.GenerateFromPassword([]byte("newpassword"), bcrypt.DefaultCost); fmt.Println(string(b)) }'

# Or use Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'newpassword', bcrypt.gensalt(rounds=12)).decode())"
```

Update the password file:

```bash
echo 'HASH_HERE' > /etc/webadmin/passwd
chmod 600 /etc/webadmin/passwd
```

---

## Service Management

### Start Services

```bash
rc-service roothelper start
rc-service webadmin start
```

### Stop Services

```bash
rc-service webadmin stop
rc-service roothelper stop
```

### Restart Services

```bash
rc-service webadmin restart
rc-service roothelper restart
```

### Check Status

```bash
rc-service webadmin status
rc-service roothelper status
```

### View Logs

```bash
# Real-time logs
tail -f /var/log/webadmin/webadmin.log
tail -f /var/log/webadmin/roothelper.log

# All logs
cat /var/log/webadmin/*
```

### Enable/Disable at Boot

```bash
# Enable at boot
rc-update add webadmin default
rc-update add roothelper default

# Disable at boot
rc-update del webadmin default
rc-update del roothelper default

# List boot services
rc-update show
```

---

## Testing

### Health Check

```bash
curl http://localhost:8080/health
```

Expected response:
```json
{"status":"up","version":"0.1.0","timestamp":1234567890}
```

### Login

```bash
curl -X POST http://localhost:8080/api/login \
  -H "Content-Type: application/json" \
  -d '{"password":"admin"}'
```

Expected response: `204 No Content` with `Set-Cookie` headers

### Access Web UI

Open browser and navigate to:
```
http://localhost:8080
```

Login with:
- Username: admin
- Password: admin

---

## Troubleshooting

### Services Won't Start

Check logs:
```bash
tail -50 /var/log/webadmin/webadmin.log
tail -50 /var/log/webadmin/roothelper.log
```

Check permissions:
```bash
ls -ld /etc/webadmin /run/webadmin /var/log/webadmin
```

Check if port is in use:
```bash
netstat -tlnp | grep 8080
```

### IPC Socket Error

Check if socket exists:
```bash
ls -l /run/webadmin/ipc.sock
```

Check permissions:
```bash
ls -l /run/webadmin/
```

Restart services:
```bash
rc-service roothelper restart
rc-service webadmin restart
```

### WebSocket Connection Failed

Ensure you're authenticated (have session cookie):
```bash
curl -v http://localhost:8080/
```

Check browser console for errors.

### High Memory Usage

Check if processes are running:
```bash
ps aux | grep -E 'webadmin|roothelper'
```

Check memory usage:
```bash
top -p $(pgrep webadmin),$(pgrep roothelper)
```

Restart services:
```bash
rc-service webadmin restart
rc-service roothelper restart
```

---

## Uninstallation

To completely remove Alpine WebAdmin:

```bash
# Stop services
rc-service webadmin stop
rc-service roothelper stop

# Disable at boot
rc-update del webadmin default
rc-update del roothelper default

# Remove binaries
rm /usr/sbin/webadmin /usr/sbin/roothelper

# Remove configuration (optional)
rm -rf /etc/webadmin

# Remove logs (optional)
rm -rf /var/log/webadmin

# Remove user (optional)
deluser webadmin

# Remove init scripts (optional)
rm /etc/init.d/webadmin /etc/init.d/roothelper
```

---

## Advanced Configuration

### TLS/HTTPS

Generate self-signed certificate:

```bash
openssl req -x509 -newkey rsa:4096 -keyout /etc/webadmin/key.pem \
  -out /etc/webadmin/cert.pem -days 365 -nodes
chmod 600 /etc/webadmin/key.pem
chmod 644 /etc/webadmin/cert.pem
```

Update config:

```json
{
  "listen": ":8443",
  "tls_cert": "/etc/webadmin/cert.pem",
  "tls_key": "/etc/webadmin/key.pem",
  ...
}
```

Restart services:

```bash
rc-service webadmin restart
```

Access via HTTPS:

```bash
curl -k https://localhost:8443/health
```

### Rate Limiting

Adjust in `/etc/webadmin/config.json`:

```json
{
  "rate_limit_rps": 50
}
```

Restart services:

```bash
rc-service webadmin restart
```

### WebSocket Connections

Adjust in `/etc/webadmin/config.json`:

```json
{
  "ws_max_conns": 100
}
```

Restart services:

```bash
rc-service webadmin restart
```

---

## Performance Tuning

### Memory Limits

Set in `/etc/init.d/webadmin`:

```sh
export GOMEMLIMIT=40MiB
export GOGC=50
```

### CPU Affinity

Pin to specific CPU cores (optional):

```bash
taskset -c 0-1 /usr/sbin/webadmin -config /etc/webadmin/config.json
```

---

## Support & Documentation

- **Security**: See `docs/security-review.md`
- **Architecture**: See `docs/architecture-review.md`
- **Deployment**: See `docs/deployment-guide.md`
- **Testing**: See `TEST_READINESS.md`
- **Troubleshooting**: See `TESTBENCH_CHECKLIST.md`

---

## Next Steps

1. Run setup: `sudo ./setup.sh all`
2. Access web UI: `http://localhost:8080`
3. Login with default credentials (admin/admin)
4. Change password in production
5. Configure TLS for HTTPS
6. Run test suite: See `TEST_READINESS.md`

---

**Questions?** Refer to `DEVELOPMENT.md` for development setup or `TESTBENCH_CHECKLIST.md` for testing procedures.

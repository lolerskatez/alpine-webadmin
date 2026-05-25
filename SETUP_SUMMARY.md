# Alpine WebAdmin — Setup Summary

**Status**: ✅ Complete  
**Target**: Alpine Linux 3.18+  
**Script**: `setup.sh` (comprehensive, single-file setup)

---

## What You Get

A single, robust setup script that handles:

1. **Build Phase** (on development machine)
   - Detects Alpine Linux and CPU architecture
   - Installs Go 1.22+ (from source if needed)
   - Installs build dependencies
   - Downloads Go modules
   - Downloads Alpine.js
   - Builds webadmin and roothelper binaries
   - Runs tests

2. **Deployment Phase** (on testbench)
   - Creates system user and directories
   - Installs binaries
   - Creates configuration
   - Sets up password
   - Installs OpenRC init scripts
   - Enables services at boot
   - Starts services

---

## Quick Start (3 Steps)

### Step 1: Build (on development machine)

```bash
cd alpine-webadmin
chmod +x setup.sh
./setup.sh build
```

**Output**: `bin/webadmin` and `bin/roothelper` binaries

### Step 2: Transfer (to testbench)

```bash
scp bin/webadmin bin/roothelper root@testbench:/tmp/
```

### Step 3: Deploy (on testbench)

```bash
cd alpine-webadmin
sudo ./setup.sh deploy
```

**Result**: Services running on http://localhost:8080

---

## Alternative: Build + Deploy in One Step

If you're running on the testbench machine itself:

```bash
sudo ./setup.sh all
```

This builds and deploys everything in one command.

---

## Usage

```bash
./setup.sh [command]

Commands:
  build       Build binaries only (requires Go 1.22+)
  deploy      Deploy pre-built binaries (requires root)
  all         Build and deploy (requires root for deploy phase)
  help        Show help message
```

---

## What Gets Installed

### Binaries
- `/usr/sbin/webadmin` — HTTP/WebSocket server
- `/usr/sbin/roothelper` — Privileged IPC daemon

### Configuration
- `/etc/webadmin/config.json` — Main configuration
- `/etc/webadmin/passwd` — Password hash (default: admin)

### Directories
- `/run/webadmin/` — Runtime socket and PID files
- `/var/log/webadmin/` — Log files

### Services
- `/etc/init.d/webadmin` — OpenRC init script
- `/etc/init.d/roothelper` — OpenRC init script

### User
- `webadmin` — System user (unprivileged)

---

## Default Credentials

- **URL**: http://localhost:8080
- **Username**: admin
- **Password**: admin

⚠️ **Change in production!**

---

## Service Management

```bash
# Start
rc-service webadmin start
rc-service roothelper start

# Stop
rc-service webadmin stop
rc-service roothelper stop

# Restart
rc-service webadmin restart
rc-service roothelper restart

# Status
rc-service webadmin status
rc-service roothelper status

# View logs
tail -f /var/log/webadmin/webadmin.log
tail -f /var/log/webadmin/roothelper.log
```

---

## Configuration

Edit `/etc/webadmin/config.json`:

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

Then restart:

```bash
rc-service webadmin restart
```

---

## Troubleshooting

### Services won't start

Check logs:
```bash
tail -50 /var/log/webadmin/webadmin.log
tail -50 /var/log/webadmin/roothelper.log
```

Check permissions:
```bash
ls -ld /etc/webadmin /run/webadmin /var/log/webadmin
```

### Build fails

Ensure Alpine Linux 3.18+:
```bash
cat /etc/os-release
```

Ensure internet access for downloading Go and Alpine.js.

### Port already in use

Check what's using port 8080:
```bash
netstat -tlnp | grep 8080
```

Change listen port in `/etc/webadmin/config.json` and restart.

---

## Documentation

- **Setup Guide**: `SETUP_GUIDE.md` — Detailed setup instructions
- **Test Plan**: `TEST_READINESS.md` — Testing procedures
- **Deployment**: `TESTBENCH_CHECKLIST.md` — Step-by-step checklist
- **Security**: `docs/security-review.md` — Security hardening
- **Architecture**: `docs/architecture-review.md` — System design

---

## Next Steps

1. **Build**: `./setup.sh build`
2. **Transfer**: `scp bin/webadmin bin/roothelper root@testbench:/tmp/`
3. **Deploy**: `sudo ./setup.sh deploy`
4. **Test**: Navigate to http://localhost:8080
5. **Run Tests**: See `TEST_READINESS.md`

---

## Features

✅ Automatic Go installation  
✅ Automatic dependency installation  
✅ Automatic Alpine.js download  
✅ Automatic binary building  
✅ Automatic system user creation  
✅ Automatic directory creation  
✅ Automatic configuration  
✅ Automatic service setup  
✅ Automatic service startup  
✅ Comprehensive error handling  
✅ Colored output  
✅ Detailed logging  

---

## Requirements

### Build Machine
- Alpine Linux 3.18+ (or any Linux with apk)
- Internet access
- ~500 MB disk space

### Testbench Machine
- Alpine Linux 3.18+
- Root access
- ~100 MB disk space

---

## Support

Questions? See:
- `SETUP_GUIDE.md` for detailed instructions
- `TESTBENCH_CHECKLIST.md` for testing
- `docs/security-review.md` for security
- `docs/architecture-review.md` for architecture

---

**Ready to deploy?** Run: `./setup.sh build`

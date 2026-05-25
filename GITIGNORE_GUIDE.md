# Alpine WebAdmin — .gitignore Guide

Comprehensive guide to files and directories ignored by Git in the Alpine WebAdmin project.

---

## Overview

The `.gitignore` file prevents committing:
- Build artifacts and binaries
- Generated files
- IDE/editor configuration
- OS-specific files
- Sensitive configuration and secrets
- Log files and runtime data
- Development artifacts

---

## Categories

### Build Artifacts

**Files ignored:**
- `bin/` — Compiled binaries (webadmin, roothelper)
- `dist/` — Distribution packages
- `build/` — Build output
- `*.exe`, `*.dll`, `*.so`, `*.dylib` — Platform-specific binaries

**Why:** Binaries are generated from source and should not be committed. They can be rebuilt anytime.

---

### Go Build Files

**Files ignored:**
- `*.o`, `*.a` — Object and archive files
- `*.test` — Test binaries
- `*.out` — Coverage output
- `coverage.txt`, `coverage.html` — Coverage reports
- `go.work` — Go workspace file
- `vendor/` — Vendored dependencies

**Why:** These are generated during build and testing. Dependencies should be managed via `go.mod` and `go.sum`.

---

### Frontend Assets

**Files ignored:**
- `internal/frontend/assets/alpine.min.js` — Downloaded Alpine.js library

**Why:** Alpine.js is downloaded automatically by the setup script. It's not part of the source code.

**Note:** `index.html`, `app.css`, and `app.js` ARE committed (they're source code).

---

### IDE & Editor Files

**Files ignored:**
- `.vscode/` — VS Code settings
- `.idea/` — JetBrains IDE settings
- `*.swp`, `*.swo` — Vim swap files
- `*~` — Emacs backup files
- `*.sublime-project`, `*.sublime-workspace` — Sublime Text files

**Why:** IDE configuration is personal and should not be shared. Each developer uses their own setup.

---

### OS & System Files

**Files ignored:**
- `.DS_Store` — macOS metadata
- `Thumbs.db` — Windows thumbnail cache
- `Desktop.ini` — Windows folder settings
- `.directory` — Linux folder metadata

**Why:** These are OS-generated and not relevant to the project.

---

### Development & Testing

**Files ignored:**
- `*.tmp`, `*.temp` — Temporary files
- `*.bak`, `*.backup` — Backup files
- `*.coverprofile`, `*.coverage` — Coverage reports
- `*.pprof`, `*.prof` — Profiling data

**Why:** These are generated during development and testing, not part of the source code.

---

### Configuration & Secrets

**Files ignored:**
- `.env`, `.env.local` — Environment variables
- `*.pem`, `*.key`, `*.crt`, `*.cert` — TLS certificates and keys
- `/etc/webadmin/passwd` — Password hashes
- `/etc/webadmin/config.json` — Runtime configuration

**Why:** These contain sensitive information and should never be committed to version control.

**Important:** Always use `.env` files for local development, never commit secrets.

---

### Logs & Runtime

**Files ignored:**
- `*.log` — Log files
- `logs/` — Log directory
- `/var/log/webadmin/` — Application logs
- `*.pid` — Process ID files
- `/run/webadmin/` — Runtime socket and PID files
- `/tmp/`, `/temp/` — Temporary directories

**Why:** These are generated at runtime and specific to each environment.

---

### VirtualBox & VM

**Files ignored:**
- `*.vbox`, `*.vdi`, `*.vmdk`, `*.vhd` — VM disk images
- `.vagrant/` — Vagrant configuration
- `vagrant.log` — Vagrant logs

**Why:** VM images are large and environment-specific. Use provisioning scripts instead.

---

### Docker (if used)

**Files ignored:**
- `docker-compose.override.yml` — Local Docker overrides
- `.dockerignore` — Docker ignore file

**Why:** Local overrides should not be committed. Each developer can have their own.

---

## What IS Committed

### Source Code
- `cmd/` — Go source code
- `pkg/` — Go packages
- `internal/` — Internal packages
- `internal/frontend/assets/index.html` — Frontend HTML
- `internal/frontend/assets/app.css` — Frontend CSS
- `internal/frontend/assets/app.js` — Frontend JavaScript

### Configuration
- `go.mod`, `go.sum` — Go module definitions
- `Makefile` — Build configuration
- `etc/config.json` — Example configuration (no secrets)

### Documentation
- `README.md` — Project overview
- `DEVELOPMENT.md` — Development guide
- `docs/` — Architecture and deployment documentation
- `SETUP_GUIDE.md` — Setup instructions
- `TEST_READINESS.md` — Testing guide

### Build & Deploy
- `setup.sh` — Setup script
- `init/` — Init scripts
- `.gitignore` — This file

---

## Common Mistakes to Avoid

### ❌ Don't Commit

```bash
# Binaries
bin/webadmin
bin/roothelper

# Generated files
*.test
coverage.txt

# Secrets
.env
*.key
*.pem
/etc/webadmin/passwd

# IDE settings
.vscode/settings.json
.idea/

# OS files
.DS_Store
Thumbs.db

# Logs
*.log
/var/log/webadmin/
```

### ✅ Do Commit

```bash
# Source code
cmd/webadmin/main.go
pkg/auth/auth.go
internal/frontend/assets/app.js

# Configuration (without secrets)
go.mod
go.sum
Makefile
etc/config.json

# Documentation
README.md
DEVELOPMENT.md
docs/architecture-review.md

# Build scripts
setup.sh
init/openrc/webadmin
```

---

## Checking What's Ignored

### List all ignored files

```bash
git status --ignored
```

### Check if a specific file is ignored

```bash
git check-ignore -v path/to/file
```

### See what would be committed

```bash
git status
```

---

## If You Accidentally Committed Something

### Remove from Git (keep local file)

```bash
git rm --cached path/to/file
git commit -m "Remove accidentally committed file"
```

### Remove from Git (delete local file)

```bash
git rm path/to/file
git commit -m "Remove accidentally committed file"
```

### Add to .gitignore and remove from history

```bash
echo "path/to/file" >> .gitignore
git rm --cached path/to/file
git commit -m "Add to gitignore and remove from history"
```

---

## Best Practices

1. **Never commit secrets** — Use `.env` files for local development
2. **Never commit binaries** — They're generated from source
3. **Never commit IDE settings** — Each developer uses their own
4. **Never commit generated files** — They're created during build/test
5. **Always commit source code** — That's what Git is for
6. **Always commit documentation** — Help future developers
7. **Always commit build scripts** — Make setup reproducible

---

## Summary

The `.gitignore` file ensures:
- ✅ Clean repository with only source code
- ✅ No sensitive information in version control
- ✅ No environment-specific files
- ✅ No generated artifacts
- ✅ No IDE configuration conflicts
- ✅ Smaller repository size
- ✅ Easier collaboration

**Golden Rule:** If it's generated, temporary, or secret → ignore it. If it's source code or documentation → commit it.

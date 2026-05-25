# .gitignore Summary

## Files & Directories Ignored

### Build & Binaries
```
bin/                    # Compiled binaries
dist/                   # Distribution packages
build/                  # Build output
*.exe, *.dll, *.so      # Platform-specific binaries
```

### Go Build
```
*.o, *.a                # Object/archive files
*.test                  # Test binaries
*.out                   # Coverage output
coverage.txt            # Coverage reports
go.work                 # Go workspace
vendor/                 # Vendored dependencies
```

### Frontend
```
internal/frontend/assets/alpine.min.js    # Downloaded Alpine.js
node_modules/           # Node modules (if using npm)
package-lock.json       # npm lock file
yarn.lock               # Yarn lock file
```

### IDE & Editors
```
.vscode/                # VS Code settings
.idea/                  # JetBrains IDE
*.swp, *.swo            # Vim swap files
*~                      # Emacs backup files
*.sublime-project       # Sublime Text
```

### OS Files
```
.DS_Store               # macOS metadata
Thumbs.db               # Windows thumbnails
Desktop.ini             # Windows folder settings
.directory              # Linux folder metadata
```

### Development
```
*.tmp, *.temp           # Temporary files
*.bak, *.backup         # Backup files
*.coverprofile          # Coverage profiles
*.pprof, *.prof         # Profiling data
```

### Secrets & Config
```
.env                    # Environment variables
.env.local              # Local environment
*.pem, *.key            # TLS certificates
*.crt, *.cert           # Certificates
/etc/webadmin/passwd    # Password hashes
/etc/webadmin/config.json  # Runtime config
```

### Logs & Runtime
```
*.log                   # Log files
logs/                   # Log directory
/var/log/webadmin/      # Application logs
*.pid                   # Process ID files
/run/webadmin/          # Runtime sockets
/tmp/, /temp/           # Temporary directories
```

### VirtualBox & VM
```
*.vbox, *.vdi           # VM images
*.vmdk, *.vhd           # VM disks
.vagrant/               # Vagrant config
vagrant.log             # Vagrant logs
```

### Docker
```
docker-compose.override.yml    # Local overrides
.dockerignore                  # Docker ignore
```

---

## What IS Committed

### Source Code
- `cmd/` — Go binaries
- `pkg/` — Go packages
- `internal/` — Internal packages
- `internal/frontend/assets/index.html` — Frontend HTML
- `internal/frontend/assets/app.css` — Frontend CSS
- `internal/frontend/assets/app.js` — Frontend JavaScript

### Configuration
- `go.mod`, `go.sum` — Module definitions
- `Makefile` — Build config
- `etc/config.json` — Example config

### Documentation
- `README.md` — Overview
- `DEVELOPMENT.md` — Dev guide
- `docs/` — Architecture docs
- `SETUP_GUIDE.md` — Setup instructions
- `TEST_READINESS.md` — Testing guide

### Build & Deploy
- `setup.sh` — Setup script
- `init/` — Init scripts
- `.gitignore` — This file

---

## Quick Reference

### Check what's ignored
```bash
git status --ignored
```

### Check if file is ignored
```bash
git check-ignore -v path/to/file
```

### Remove accidentally committed file
```bash
git rm --cached path/to/file
git commit -m "Remove file"
```

---

## Key Rules

✅ **DO Commit**
- Source code
- Documentation
- Build scripts
- Configuration examples (no secrets)

❌ **DON'T Commit**
- Binaries and build artifacts
- Generated files
- IDE/editor settings
- OS-specific files
- Secrets and passwords
- Log files
- Temporary files

---

For detailed explanations, see `GITIGNORE_GUIDE.md`

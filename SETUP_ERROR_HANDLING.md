# Setup Script — Error Handling Improvements

## What Was Fixed

The setup script now has **comprehensive error handling** for common issues encountered during installation on Alpine Linux.

---

## Error Handling Improvements

### 1. Go Installation from Alpine Repository

**Problem**: Alpine repository may not have Go 1.22+ available, causing `apk add go` to fail with:
```
Error: unable to select packages:
  go (no such package)
```

**Solution**: 
- Script now attempts repository installation first
- If it fails or version is too old, automatically falls back to source installation
- Provides clear warning messages instead of failing

```bash
# Before: Would fail with cryptic error
# After: Gracefully falls back to source installation
```

### 2. Go Source Download

**Problem**: Download could fail due to:
- No internet connection
- Missing curl/wget
- Corrupted download

**Solution**:
- Checks for curl/wget availability with helpful error message
- Verifies download success before extraction
- Checks file size to ensure it's not empty
- Provides clear error messages with next steps

```bash
# Error messages now include:
# - What went wrong
# - How to fix it
# - Alternative options (manual download)
```

### 3. Go Extraction

**Problem**: Corrupted or incomplete download could fail silently during extraction

**Solution**:
- Wraps tar extraction with error checking
- Verifies Go binary exists after extraction
- Provides helpful error messages if extraction fails

### 4. Build Dependencies Installation

**Problem**: `apk add` could fail due to:
- Outdated package index
- Network issues
- Missing packages

**Solution**:
- Runs `apk update` before installing packages
- Checks for errors during package installation
- Provides clear error messages with troubleshooting steps

---

## Error Messages Now Include

### Clear Problem Description
```
[ERROR] Failed to download Go from https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
```

### Actionable Next Steps
```
[ERROR] Please check your internet connection and try again
[ERROR] Or manually download from: https://go.dev/dl
```

### Helpful Context
```
[ERROR] Please install curl or wget: apk add curl
[ERROR] The downloaded file may be corrupted
```

---

## Error Handling Flow

```
install_go()
  ├─ Try: apk add go (from repository)
  │  ├─ Success + correct version? → Done
  │  ├─ Success + old version? → Fall back to source
  │  └─ Failed? → Fall back to source
  │
  └─ install_go_from_source()
     ├─ Check architecture
     ├─ Check curl/wget available
     ├─ Download with error checking
     ├─ Verify file not empty
     ├─ Extract with error checking
     ├─ Verify binary exists
     └─ Add to PATH

install_build_dependencies()
  ├─ apk update (with error checking)
  └─ apk add packages (with error checking)
```

---

## Testing the Error Handling

### Test 1: Missing Internet
```bash
# Disconnect network, then run
./setup.sh build

# Expected: Clear error message about download failure
```

### Test 2: Old Alpine Version
```bash
# On Alpine 3.17 or older
./setup.sh build

# Expected: Script falls back to source installation
```

### Test 3: Missing curl/wget
```bash
# Remove curl and wget
apk del curl wget

# Run setup
./setup.sh build

# Expected: Clear error message about missing tools
```

### Test 4: Corrupted Download
```bash
# Manually corrupt the download (if testing locally)
# Script will detect and report error
```

---

## What Happens Now When You Run Setup

### Scenario 1: Go Not Available in Repository
```
[INFO] Installing Go 1.22.0...
[INFO] Attempting to install Go from Alpine repository...
[WARN] Go not available in Alpine repository (or installation failed)
[INFO] Falling back to source installation from go.dev...
[INFO] Downloading Go from https://go.dev/dl/go1.22.0.linux-amd64.tar.gz...
[INFO] Extracting Go...
[✓] Go installed to /usr/local/go
```

### Scenario 2: Download Fails
```
[INFO] Installing Go 1.22.0...
[INFO] Attempting to install Go from Alpine repository...
[WARN] Go not available in Alpine repository (or installation failed)
[INFO] Falling back to source installation from go.dev...
[INFO] Downloading Go from https://go.dev/dl/go1.22.0.linux-amd64.tar.gz...
[ERROR] Failed to download Go from https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
[ERROR] Please check your internet connection and try again
[ERROR] Or manually download from: https://go.dev/dl
```

### Scenario 3: Dependencies Fail
```
[INFO] Installing build dependencies...
[ERROR] Failed to update package index
[ERROR] Please check your internet connection
```

---

## Key Improvements

✅ **Graceful Fallbacks** — Repository → Source installation  
✅ **Download Verification** — Checks file size and integrity  
✅ **Clear Error Messages** — Explains what went wrong and how to fix it  
✅ **Helpful Suggestions** — Provides next steps and alternatives  
✅ **Network Error Handling** — Detects and reports connectivity issues  
✅ **File Verification** — Ensures downloads and extractions succeed  
✅ **Dependency Checking** — Verifies required tools are available  

---

## For Users

If you encounter an error:

1. **Read the error message carefully** — It now explains what went wrong
2. **Follow the suggested action** — Error messages include next steps
3. **Check internet connection** — Most errors are network-related
4. **Try again** — Network issues are often temporary

---

## For Developers

The error handling uses:
- `if ! command 2>/dev/null; then` — Suppress stderr, check exit code
- `[ ! -f file ] || [ ! -s file ]` — Verify file exists and is not empty
- `trap "rm -rf $tmpdir" EXIT` — Clean up temp files on exit
- Clear log messages at each step — Easy to debug issues

---

## Summary

The setup script now handles errors **gracefully and informatively**, making it suitable for:
- ✅ Automated deployments
- ✅ CI/CD pipelines
- ✅ User-facing installations
- ✅ Troubleshooting and debugging

**No more cryptic errors — just clear, actionable messages!**

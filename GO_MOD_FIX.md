# Go Modules Fix Guide

## Problem

When building, you get:
```
missing go.sum entry for module providing package golang.org/x/crypto/bcrypt
```

This means Go dependencies haven't been properly downloaded.

---

## Solution

### Option 1: Updated Setup Script (Recommended)

The setup script has been updated to automatically handle this:

```bash
./setup.sh build
```

The script now:
1. Runs `go mod tidy` to clean up modules
2. Runs `go mod download` to fetch dependencies
3. Builds binaries with error checking

### Option 2: Manual Fix

If you're not using the setup script:

```bash
# 1. Tidy up modules
go mod tidy

# 2. Download dependencies
go mod download

# 3. Verify modules
go mod verify

# 4. Build
go build -o bin/webadmin ./cmd/webadmin
go build -o bin/roothelper ./cmd/roothelper
```

### Option 3: Quick One-Liner

```bash
go mod tidy && go mod download && go build -o bin/webadmin ./cmd/webadmin && go build -o bin/roothelper ./cmd/roothelper
```

---

## What Each Command Does

### `go mod tidy`
- Removes unused dependencies
- Adds missing dependencies
- Updates `go.mod` and `go.sum`

### `go mod download`
- Downloads all dependencies to local cache
- Ensures `go.sum` has all entries
- Verifies checksums

### `go mod verify`
- Verifies all dependencies are correct
- Checks checksums match

### `go build`
- Compiles the binary
- Uses downloaded dependencies

---

## Why This Happens

1. **Fresh clone** — Dependencies not yet downloaded
2. **Missing `go.sum`** — Dependency checksums not recorded
3. **Network issue** — Download failed silently
4. **Go version mismatch** — Different Go versions have different module handling

---

## Prevention

The updated setup script now:
- ✅ Runs `go mod tidy` before building
- ✅ Runs `go mod download` before building
- ✅ Checks for errors at each step
- ✅ Provides helpful error messages

---

## Testing the Fix

After running the fix:

```bash
# Verify go.sum exists and has entries
ls -l go.sum
wc -l go.sum

# Verify modules are correct
go mod verify

# Try building
go build -o bin/webadmin ./cmd/webadmin
```

---

## If You Still Get Errors

### Check internet connection
```bash
ping 8.8.8.8
```

### Check Go version
```bash
go version
```
Expected: `go1.22.X` or higher

### Clear Go cache and retry
```bash
go clean -modcache
go mod download
go build -o bin/webadmin ./cmd/webadmin
```

### Check go.mod is valid
```bash
cat go.mod
```
Should show:
```
module github.com/alpine-webadmin/alpine-webadmin
go 1.22
require golang.org/x/crypto v0.24.0
```

---

## Summary

The setup script has been updated to automatically:
1. Tidy Go modules
2. Download dependencies
3. Build with error checking

**Just run:**
```bash
./setup.sh build
```

**Or manually:**
```bash
go mod tidy && go mod download && ./setup.sh build
```

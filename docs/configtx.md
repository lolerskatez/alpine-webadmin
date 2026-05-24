# Alpine WebAdmin — Configuration Transaction System

## Overview

The `pkg/configtx` package provides atomic, rollback-safe configuration changes for Alpine Linux systems. It supports both disk-based and diskless (lbu) installations, with special safety handling for network configs to prevent remote lockout.

## Architecture

```
┌────────────────────────────────────────────────────┐
│  Admin UI / API                                    │
└────────────────┬───────────────────────────────────┘
                 │
                 ▼
┌────────────────────────────────────────────────────┐
│  Transaction Engine                                │
│  ┌─────────┐  ┌──────────┐  ┌───────────────┐    │
│  │  Begin  │→ │  Stage   │→ │   Validate    │    │
│  │ (backup)│  │ (staging)│  │ (validators)  │    │
│  └─────────┘  └──────────┘  └───────────────┘    │
│         │                          │              │
│         │         rollback ←───────┘ on failure  │
│         ▼                                          │
│  ┌───────────────────────────────────────────────┐ │
│  │  Commit                                       │ │
│  │   ├─ Network? → StageCommit (commit-confirm)│ │
│  │   └─ Normal  → os.Rename (atomic)             │ │
│  └───────────────────────────────────────────────┘ │
│         │                                          │
│         ▼                                          │
│  ┌──────────────┐  ┌───────────────────────────┐ │
│  │  Confirm     │  │  Auto-rollback (network)  │ │
│  │  (network)   │  │  Timer expires → restore   │ │
│  └──────────────┘  └───────────────────────────┘ │
│         │                                          │
│         ▼                                          │
│  ┌──────────────┐  ┌───────────────────────────┐ │
│  │  Service     │  │  lbu commit (diskless)    │ │
│  │  Reload      │  │  or direct disk write     │ │
│  └──────────────┘  └───────────────────────────┘ │
└────────────────────────────────────────────────────┘
```

## Transaction Lifecycle

```
    +----------+     begin      +--------+
    |  Idle    | --------------→| Pending|
    +----------+               +--------+
                                     │
                                     │ stage(content)
                                     ▼
                                  +--------+
                                  | Staged |
                                  +--------+
                                     │
                                     │ validate()
                        ┌────────────┴────────────┐
                        │                         │
                        ▼                         ▼
                   +----------+            +---------+
                   |Validated │            | Failed  │
                   +----------+            +---------+
                        │                         │
                        │ commit()                │ rollback()
                        ▼                         ▼
    +--------------------------------+    +-------------+
    │  Network?                      │    │  Restored   │
    │  ├─ Yes → apply + start timer │    +-------------+
    │  └─ No  → atomic rename       │
    +--------------------------------+
                        │
            ┌───────────┴───────────┐
            │                         │
            ▼                         ▼
    +-----------+            +--------------+
    │ Committed │            │ Rolled Back  │
    +-----------+            +--------------+
            │
            │ confirm() [network only]
            ▼
    +-----------+
    │ Confirmed │
    +-----------+
```

## Supported Configurations

| Config | Path Example | Validator | Network | Reload Service |
|--------|-------------|-----------|---------|---------------|
| SSH | `/etc/ssh/sshd_config` | `sshd -t` | No | `sshd` |
| Samba | `/etc/samba/smb.conf` | `testparm` | No | `samba` |
| Network | `/etc/network/interfaces` | Syntax check | **Yes** | `networking` |
| FSTAB | `/etc/fstab` | `findmnt` | No | — |
| NFS Exports | `/etc/exports` | Syntax check | No | `nfs` |
| OpenRC | `/etc/conf.d/*` | Syntax check | No | varies |

## Network Safety (Commit-Confirm)

Network configuration changes use a **commit-confirm** pattern to prevent remote lockout:

1. **Apply**: Staged config is moved to live path, service is restarted
2. **Timer**: 120-second confirmation window starts
3. **Confirm**: Admin must explicitly confirm the change is working
4. **Auto-rollback**: If not confirmed, original config is restored and service restarted

```go
target := &configtx.TargetConfig{
    Name:      "network",
    Path:      "/etc/network/interfaces",
    IsNetwork: true,
    Service:   "networking",
    Validate:  configtx.ValidateNetwork,
}

tx, _ := engine.Begin(target)
tx.Stage(newConfig)
tx.Validate()
engine.Commit(tx.ID) // applies + starts timer

// ... admin verifies connectivity ...
engine.Confirm(tx.ID) // stops timer, persists
```

## Diskless Alpine Support

For diskless Alpine installations (detected by tmpfs/squashfs root):

1. Changes are written to tmpfs (immediately active)
2. On successful transaction confirmation, `lbu commit` is called
3. Pending changes can be tracked via `lbu status`

```go
if engine.persist.IsDiskless() {
    log.Println("Running in diskless mode; lbu commit on success")
}
files, _ := engine.persist.PendingChanges()
```

## Safety Guarantees

| Guarantee | Mechanism |
|-----------|-----------|
| **Atomic writes** | `os.Rename` for staged → live |
| **Always rollback-able** | Backup created before any modification |
| **Validation before commit** | Every config type has a validator |
| **No remote lockout** | Network configs use commit-confirm with auto-rollback |
| **No arbitrary execution** | `runCmd` uses explicit args, no shell |
| **Credential lock** | Subprocesses cannot escalate privileges |

## Failure Recovery

| Failure | Behavior |
|---------|----------|
| Validation fails | Transaction marked failed, staged file removed, live config untouched |
| Commit fails | Staged file may remain in backup dir; original config intact |
| Network service restart fails | Config is still applied; auto-rollback timer starts |
| Auto-rollback fires | Original config restored from backup, service restarted |
| lbu commit fails | Change is active in tmpfs but not persisted; admin alerted |
| Transaction crash | Cleanup sweeper removes stale transactions after TTL |

## Example Usage

```go
engine := configtx.NewEngine("/var/backups/webadmin", logger)

// Define a managed config
target := &configtx.TargetConfig{
    Name:       "sshd",
    Path:       "/etc/ssh/sshd_config",
    Validate:   configtx.ValidateSSHD,
    NeedReload: true,
    Service:    "sshd",
}

// Begin transaction
tx, err := engine.Begin(target)
if err != nil {
    log.Fatal(err)
}

// Write proposed config to staging
if err := tx.Stage(newSSHDConfig); err != nil {
    engine.Rollback(tx.ID)
    log.Fatal(err)
}

// Validate
if err := tx.Validate(); err != nil {
    engine.Rollback(tx.ID)
    log.Fatal("validation failed:", err)
}

// Commit (atomic rename + reload + persist)
if err := engine.Commit(tx.ID); err != nil {
    engine.Rollback(tx.ID)
    log.Fatal(err)
}

// For network configs, call Confirm after verifying connectivity
// engine.Confirm(tx.ID)
```

## API Reference

### Engine

| Method | Description |
|--------|-------------|
| `NewEngine(dir, logger)` | Create engine with backup directory |
| `Begin(target)` | Start transaction, create backup |
| `Commit(txid)` | Validate → rename → reload → persist |
| `Rollback(txid)` | Restore from backup |
| `Confirm(txid)` | Confirm network change, stop timer |
| `Get(txid)` | Retrieve transaction by ID |
| `List()` | List all transactions |
| `Cleanup(ttl, retain)` | Prune old transactions and backups |
| `Reload(target)` | Trigger OpenRC service reload |

### Transaction

| Method | Description |
|--------|-------------|
| `Stage(content)` | Write proposed config to staging file |
| `Validate()` | Run target validator against staged file |

### Validators

| Function | Command |
|----------|---------|
| `ValidateSSHD(path)` | `/usr/sbin/sshd -t -f <path>` |
| `ValidateSamba(path)` | `/usr/bin/testparm -s <path>` |
| `ValidateNetwork(path)` | Syntax check (iface, address) |
| `ValidateFSTAB(path)` | `findmnt --verify` + field count |
| `ValidateExports(path)` | Path + client field checks |
| `ValidateOpenRC(path)` | Shell injection check |

## Implementation Checklist

- [x] Atomic writes via `os.Rename`
- [x] Timestamped backups per target
- [x] Validation pipeline with built-in validators
- [x] Rollback from any stage
- [x] Network commit-confirm with auto-rollback timer
- [x] Service reload after commit
- [x] Diskless detection (`/proc/mounts` tmpfs check)
- [x] `lbu commit` integration
- [x] Transaction cleanup sweeper
- [x] No shell execution (explicit binary paths)
- [x] Credential lock on subprocesses

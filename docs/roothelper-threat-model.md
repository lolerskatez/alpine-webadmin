# Alpine WebAdmin — Roothelper Threat Model

## Scope

This document covers threats against the privileged `roothelper` daemon, which runs as root and exposes capabilities over a Unix domain socket to the unprivileged `webadmin` server.

## Trust Boundaries

```
┌─────────────────────────────────────────┐
│  Browser  │  Attacker (untrusted)        │
├─────────────────────────────────────────┤
│  webadmin │  Runs as unprivileged user   │  ← Trust Boundary A
│           │  (webadmin:webadmin)         │
├─────────────────────────────────────────┤
│  IPC      │  Unix socket /run/webadmin/  │  ← Trust Boundary B
│           │  ipc.sock (0660 root:webadmin) │
├─────────────────────────────────────────┤
│  roothelper│  Runs as root               │  ← Trust Boundary C
│           │  (UID 0)                       │
└─────────────────────────────────────────┘
```

## Threats & Mitigations

### 1. Unauthorized IPC Connection

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker connects directly to Unix socket, bypassing webadmin |
| **Mitigation** | `SO_PEERCRED` validates peer UID matches `webadmin` user |
| **Fallback** | Socket permissions `0660 root:webadmin` — only webadmin group can connect |
| **Note** | Any process running as `webadmin` can connect; compromise of webadmin = full capability access |

### 2. Command Injection via Arguments

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker sends `"nginx; rm -rf /"` as service name |
| **Mitigation** | `exec.CommandContext` with explicit args — no shell involved, no string interpolation |
| **Validation** | `validateService` rejects names not matching `^[a-zA-Z0-9_\-]+$` |
| **Validation** | `validateUsername` enforces POSIX username rules |
| **Validation** | `validateModule` rejects non-alphanumeric module names |
| **Validation** | `validateMountpoint` rejects relative paths and `..` traversal |
| **Validation** | `validateDevice` requires `/dev/` prefix |

### 3. Capability Escalation via Allowlist Bypass

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker requests operation using binary not in allowlist |
| **Mitigation** | Every handler checks `checkAllow(path)` before execution |
| **Config** | `allowed_helpers` in config.json explicitly lists permitted binaries |
| **Deny-by-default** | Unknown operations return `ErrCapabilityDenied` |

### 4. Path Traversal in Mount/Device Arguments

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker mounts `/dev/../etc/passwd` to extract secrets |
| **Mitigation** | `validateMountpoint` and `validateDevice` both reject `..` sequences |
| **Mitigation** | Device paths must start with `/dev/` |

### 5. Arbitrary Action Execution

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker sends `action: "; /bin/sh -c evil"` for service control |
| **Mitigation** | `validateAction` compares against explicit allowlist (`start`, `stop`, `restart`, `status`) |
| **No shell** | `exec.CommandContext` receives action as literal string argument |

### 6. Long-Running / Hanging Commands

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker triggers `modprobe` that hangs indefinitely |
| **Mitigation** | Every command uses `context.WithTimeout` |
| **Timeouts** | Service list: 5s, Status: 5s, Control: 30s, Package list: 10s, Mount/umount: 15s, Module: 15s |
| **Kill** | `exec.CommandContext` sends SIGKILL on context cancellation |

### 7. Privilege Escalation via setuid Binary

| Aspect | Detail |
|--------|--------|
| **Threat** | Called binary has setuid bit, attacker exploits it |
| **Mitigation** | `SysProcAttr.Credential` locks UID/GID to current process credentials |
| **No setuid** | Prevents any privilege change during subprocess execution |

### 8. Audit Log Tampering

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker wipes logs to cover tracks |
| **Mitigation** | Structured JSON logs to stderr; captured by OpenRC logger (syslog) |
| **Immutability** | Roothelper does not write to files; all output via stderr |
| **Every request logged** | Request type, RID, response status, duration |

### 9. Information Disclosure via Error Messages

| Aspect | Detail |
|--------|--------|
| **Threat** | Detailed error messages reveal system internals |
| **Mitigation** | `ipc.ErrExecutionFailed` includes command stderr (needed for debugging) |
| **Balance** | Full stderr included for legitimate admin debugging; no stack traces |

### 10. Replay Attack

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker replays captured IPC message |
| **Mitigation** | No replay protection currently (one-shot request/response per connection) |
| **Note** | Unix socket is local only; replay requires access to the socket or webadmin process |

### 11. Denial of Service via Resource Exhaustion

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker opens thousands of concurrent IPC connections |
| **Mitigation** | `ipc.Serve` spawns one goroutine per connection; Go scheduler handles load |
| **Mitigation** | `handleConnection` sets 5s read deadline, closes conn after one request |
| **Note** | No connection pool limit currently; add if needed |

### 12. Kernel Module Injection

| Aspect | Detail |
|--------|--------|
| **Threat** | Attacker loads malicious kernel module |
| **Mitigation** | `validateModule` rejects names with `;`, `/`, or shell metacharacters |
| **Mitigation** | `modprobe` only loads from standard module paths (`/lib/modules/...`) |
| **Note** | Module loading is inherently powerful; disable in `allowed_helpers` if not needed |

## Attack Scenarios

### Scenario A: Attacker compromises webadmin process

1. Attacker gains execution as `webadmin` user
2. Can connect to IPC socket (same UID)
3. Can request any allowed capability
4. **Cannot** execute arbitrary commands (all paths absolute, no shell)
5. **Cannot** read `/etc/webadmin/passwd` (file permissions)
6. **Cannot** escalate to root via setuid (credential lock)

**Result**: Damage limited to allowed capability set. Can reboot, restart services, create users — but not arbitrary code execution.

### Scenario B: Attacker sends malicious payload

1. Attacker crafts IPC message with `name: "nginx; rm -rf /"`
2. `validateService` rejects — regex `^[a-zA-Z0-9_\-]+$` fails
3. Even if bypassed, `exec.CommandContext` treats entire string as single argument
4. `/sbin/rc-service "nginx; rm -rf /" start` → rc-service looks for service literally named `nginx; rm -rf /` → "not found"

**Result**: No code execution.

### Scenario C: Attacker exploits mount handler

1. Attacker sends `device: "/dev/sda1", mountpoint: "/tmp/../../etc"`
2. `validateMountpoint` rejects — contains `..`
3. Even if bypassed, `mount` receives literal string `"/tmp/../../etc"` as mountpoint
4. `mount` resolves paths; but `/tmp/../../etc` is still `/etc` which is already mounted

**Result**: No arbitrary mount achieved. Add AppArmor/SELinux for defense in depth.

## Failure Recovery

| Failure | Behavior |
|---------|----------|
| Config missing | Log error, exit with code 1 |
| Socket in use | Remove stale socket, re-bind |
| SO_PEERCRED fails | Close connection silently |
| Command timeout | Context kills subprocess, return timeout error |
| Command not found | Return `ErrExecutionFailed` with stderr |
| Invalid JSON payload | Return `ErrInvalidRequest` |
| Unknown message type | Return `ErrInvalidRequest` |
| Version mismatch | Return `ErrInvalidRequest` |

## Security Checklist

- [x] `SO_PEERCRED` validation on every connection
- [x] Socket permissions `0660 root:webadmin`
- [x] Absolute binary paths only (no shell, no PATH resolution)
- [x] `exec.CommandContext` with no shell interpolation
- [x] Credential lock (`SysProcAttr.Credential`)
- [x] Capability allowlist (`allowed_helpers`)
- [x] Input validation: service names, usernames, modules, paths
- [x] Action validation against explicit allowlists
- [x] Execution timeouts on all commands
- [x] Structured audit logging (request/response/duration)
- [x] Deny-by-default for unknown operations
- [x] Path traversal rejection (`..` detection)

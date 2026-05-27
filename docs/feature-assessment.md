# Alpine WebAdmin — Feature Assessment

_Last updated: May 26, 2026_

Focused review of the current codebase (`cmd/`, `pkg/`, `internal/frontend/assets/`)
and handoff notes (`docs/handoff.md`). Captures what is implemented, what is
stubbed but unused, and what is missing — together with a recommended
prioritization for future work.

## What's Solidly Implemented

- **Auth**: bcrypt password, `__Host-SID` session cookie, CSRF double-submit,
  per-IP rate limit, failed-login lockout (`pkg/auth/auth.go`).
- **Services**: list / status / start / stop / restart / enable / disable via
  OpenRC (`cmd/webadmin/main.go` services routes).
- **Packages**: list, search, info, install, remove, update, upgrade, async op
  polling — with mutex + IPC size/timeout fixes (`pkg/apk/apk.go`).
- **System**: hostname / OS / kernel / uptime / cores / memory, reboot, shutdown.
- **Logs**: live SSE via privileged `roothelper` log socket
  (`cmd/roothelper/logstream.go`).
- **Telemetry**: WebSocket push of CPU / mem / disk / net from `/proc`.
- **Sessions**: list + revoke.
- **Frontend**: 9-tab Alpine.js SPA, dark-mode dense UI, embedded via
  `go:embed`.

## Significant Gaps / Missing Features

### Wired in code but not exposed

- **ConfigTx engine** (`pkg/configtx/`) — staging + validation + persistence +
  LBU exist but **no IPC message types**, no `roothelper` dispatcher, no UI.
  This is the foundation for transactional `/etc/network/interfaces`,
  `/etc/resolv.conf`, etc. Currently dead code.
- **Mount / Unmount** — `ipc.MountReq` / `UnmountReq` defined in
  `pkg/ipc/ipc.go`, no handler, no UI.
- **Kernel modules** — `KModLoadReq` / `KModUnloadReq` defined, no handler,
  no UI.
- **TLS** — `cfg.TLSCert` / `cfg.TLSKey` honored, but `setup.sh` deploys plain
  HTTP `:8080` with no cert generation or HTTPS redirect.

### Read-only where they should be operational

- **Network tab** — dumps raw `/proc/net/dev`. No interface up/down, no
  IP/DNS/gateway/route config, no Wi-Fi, no `/etc/network/interfaces` editor,
  no `wpa_supplicant`, no DHCP toggle.
- **Storage tab** — dumps raw `df -h`. No mount/unmount, no fstab editor, no
  block device list (`lsblk`), no SMART, no LVM, no filesystem creation, no
  usage drill-down per directory.
- **Users tab** — list + create + password reset only. No delete, no
  lock/unlock, no group membership editing, no shell change, no SSH
  `authorized_keys` management, no sudo/doas config.

### Entire feature areas absent

- **Firewall** — no `nftables` / `iptables` / `awall` management at all.
- **SSH server config** — no `sshd_config` editor, no host-key view/rotate,
  no port / PermitRootLogin / PasswordAuth toggles.
- **APK repositories** — no `/etc/apk/repositories` editor, no mirror picker,
  no signing-key (`/etc/apk/keys`) management.
- **Time / NTP / timezone** — no `chrony` / `ntpd` config, no timezone setter
  (`setup-timezone`), no manual clock set.
- **Hostname change** — read-only.
- **Cron / scheduled tasks** — no crontab UI.
- **Backup / restore** — Alpine `lbu` not exposed (configtx hooks exist but
  no UI). No config export/import.
- **Real alerts** — `/api/alerts` returns hardcoded `"System operational"`.
  No threshold rules, no notification channels.
- **Audit log** — admin actions are logged to `webadmin.log` but no in-UI
  audit trail / no separate immutable audit file.
- **Multi-user accounts** — single shared password in `/etc/webadmin/passwd`;
  `RoleAdmin` is the only role; no per-user accounts, no RBAC, no user
  management for the webadmin tool itself.
- **2FA / TOTP / WebAuthn** — none.
- **Historical log search** — only live tail; no grep/filter/paginate over
  `/var/log/messages`, no journal-style search, no per-service logs view
  (e.g. `rc-service X status` output, `/var/log/<svc>.log`).
- **Container/VM management** — no Docker / Podman / LXC integration
  (may be intentional).
- **Process list / kill** — no `ps`-style view (telemetry has top-N CPU but
  no kill action).
- **File browser / config editor** — despite configtx engine, no generic
  `/etc/*` editor.
- **System updates indicator** — no "X packages have upgrades" badge on
  dashboard.
- **Disk SMART / health** — none.
- **Metrics export** — no `/metrics` (Prometheus) endpoint (flagged in
  handoff as Medium priority).

### Security / hardening items already flagged in handoff

- **WebSocket Origin validation** — missing.
- **WebSocket max frame size** — missing.
- **Session IP/UA binding** — not implemented.
- **Light theme / i18n** — explicitly deferred (low priority).

## Recommended Prioritization

| Priority | Item | Rationale |
|---|---|---|
| **P0** | Wire `configtx` → IPC → UI for `/etc/network/interfaces` + `/etc/resolv.conf` | Engine already written; Network tab is the biggest read-only gap |
| **P0** | TLS-by-default in `setup.sh` (self-signed) + redirect 80→443 | Currently ships HTTP-only |
| **P0** | WebSocket `Origin` check + frame-size limit | Security review item, small change |
| **P1** | Firewall management (`awall` or raw `nft`) | Common admin need on Alpine |
| **P1** | Real alerts (threshold rules over telemetry, in-memory ring buffer) | Endpoint exists as stub |
| **P1** | Hostname / timezone / NTP setters | Common day-1 ops |
| **P1** | APK repository editor (`/etc/apk/repositories`) | Required to install anything non-default |
| **P1** | User delete / lock / group / SSH-key | Half the user feature is missing |
| **P2** | Mount / Unmount + fstab + `lsblk` | IPC stubs already there |
| **P2** | Multi-account login + per-user audit | Single shared password is a meaningful limit |
| **P2** | Historical log search + per-service log files | Live-only tail is limiting for diagnostics |
| **P2** | Cron / scheduled tasks UI | Standard admin surface |
| **P3** | Backup/restore via `lbu` | Alpine-idiomatic; configtx already integrates with it |
| **P3** | 2FA (TOTP) | Defense in depth |
| **P3** | `/metrics` Prometheus endpoint | Already listed in handoff |
| **P3** | Kernel module load/unload UI | IPC stubs already there |

## Bottom Line

Core ops surface (services, packages, sessions, power, telemetry, logs) is
**complete and tested**. The biggest functional gaps are:

1. **Network and Storage tabs are read-only.**
2. **No firewall support.**
3. **Single shared password** (no real multi-user / RBAC).
4. **`configtx` engine is unused** despite being the natural foundation for
   most of the missing config-editing features.

The most strategically high-value next step is wiring the existing `configtx`
engine to the UI, since that unlocks network, fstab, repositories, hostname,
and timezone editing through one transactional pattern rather than ad-hoc
handlers.

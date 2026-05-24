# Alpine WebAdmin — Deployment Flow & Alpine Linux Compatibility

## 1. Build Flow

### Development Build

```sh
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/webadmin ./cmd/webadmin
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/roothelper ./cmd/roothelper
```

### Release Build

```sh
export GOOS=linux GOARCH=amd64
export CGO_ENABLED=0

go build -trimpath -ldflags="-s -w" -o dist/webadmin ./cmd/webadmin
go build -trimpath -ldflags="-s -w" -o dist/roothelper ./cmd/roothelper

# Optional: upx packing (verify runtime first)
# upx --best dist/webadmin dist/roothelper
```

### Build Tags

- `prod`: Disables debug endpoints and verbose logging (compile-time dead code elimination).
- `debug`: Enables `/_debug/pprof` and `/_debug/mem` (localhost-only).

## 2. Installation Flow (Manual)

```
1. Copy binaries
   /usr/sbin/webadmin
   /usr/sbin/roothelper

2. Create unprivileged user
   addgroup -S webadmin 2>/dev/null || true
   adduser -S -D -H -G webadmin -s /sbin/nologin webadmin

3. Create directories
   /etc/webadmin/        (config, 0750 root:webadmin)
   /run/webadmin/        (runtime socket, 0750 root:webadmin)
   /var/lib/webadmin/    (optional persistent data, 0750 webadmin:webadmin)

4. Install config
   /etc/webadmin/config.json
   /etc/webadmin/passwd   (bcrypt hash of admin password)
   /etc/webadmin/services.json   (allowlist of controllable services)

5. Install OpenRC init scripts
   /etc/init.d/webadmin
   /etc/init.d/roothelper

6. Set permissions
   chmod 750 /usr/sbin/webadmin /usr/sbin/roothelper
   chmod 640 /etc/webadmin/*
   chown -R root:webadmin /etc/webadmin

7. Start services
   rc-update add roothelper default
   rc-update add webadmin default
   rc-service roothelper start
   rc-service webadmin start
```

## 3. OpenRC Init Scripts

### `/etc/init.d/roothelper`

```sh
#!/sbin/openrc-run

description="Alpine WebAdmin Root Helper"
command="/usr/sbin/roothelper"
command_args="-config /etc/webadmin/config.json"
pidfile="/run/webadmin/roothelper.pid"
command_background=true

depend() {
    need localmount
    after firewall
}

start_pre() {
    checkpath --directory --mode 0750 --owner root:webadmin /run/webadmin
    checkpath --file --mode 0640 --owner root:webadmin /etc/webadmin/config.json
}
```

### `/etc/init.d/webadmin`

```sh
#!/sbin/openrc-run

description="Alpine WebAdmin Web Server"
command="/usr/sbin/webadmin"
command_args="-config /etc/webadmin/config.json"
command_user="webadmin:webadmin"
pidfile="/run/webadmin/webadmin.pid"
command_background=true

depend() {
    need net roothelper
}

start_pre() {
    checkpath --directory --mode 0750 --owner webadmin:webadmin /run/webadmin
}
```

## 4. APK Packaging (`APKBUILD`)

```sh
# Contributor: Alpine WebAdmin Team
# Maintainer: Alpine WebAdmin Team
pkgname=alpine-webadmin
pkgver=0.1.0
pkgrel=0
pkgdesc="Lightweight web administration platform for Alpine Linux"
url="https://github.com/yourname/alpine-webadmin"
arch="x86_64"
license="MIT"
options="!check" # No test suite in packaging phase
source="$pkgname-$pkgver.tar.gz"
builddir="$srcdir/$pkgname-$pkgver"

build() {
    export CGO_ENABLED=0
    go build -ldflags="-s -w" -o bin/webadmin ./cmd/webadmin
    go build -ldflags="-s -w" -o bin/roothelper ./cmd/roothelper
}

package() {
    install -Dm755 bin/webadmin "$pkgdir"/usr/sbin/webadmin
    install -Dm755 bin/roothelper "$pkgdir"/usr/sbin/roothelper

    install -Dm640 config.json "$pkgdir"/etc/webadmin/config.json
    install -Dm640 services.json "$pkgdir"/etc/webadmin/services.json

    install -Dm755 init/openrc/webadmin "$pkgdir"/etc/init.d/webadmin
    install -Dm755 init/openrc/roothelper "$pkgdir"/etc/init.d/roothelper
}

post_install() {
    addgroup -S webadmin 2>/dev/null || true
    adduser -S -D -H -G webadmin -s /sbin/nologin webadmin

    rc-update add roothelper default
    rc-update add webadmin default
}
```

## 5. Reverse Proxy (nginx)

If TLS termination is handled by nginx:

```nginx
server {
    listen 443 ssl;
    server_name admin.example.com;

    ssl_certificate /etc/ssl/certs/admin.example.com.crt;
    ssl_certificate_key /etc/ssl/private/admin.example.com.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

- `webadmin` runs on localhost only (`127.0.0.1:8080`) with TLS disabled.
- nginx handles HTTPS, client certificates, and additional access control.

## 6. Alpine-Specific Compatibility Notes

### musl libc

- `CGO_ENABLED=0` eliminates any musl/glibc dependency. The binary is a pure Linux amd64 ELF with no dynamic linker requirements.
- No `ldd` dependencies. `ldd /usr/sbin/webadmin` will report `not a dynamic executable`.

### OpenRC

- No systemd units provided. Alpine uses OpenRC exclusively.
- `supervise-daemon` is available on newer Alpine versions; init scripts above use `command_background` for compatibility with both `start-stop-daemon` and `supervise-daemon` modes.

### /run as tmpfs

- `/run/webadmin` must be created by init script `start_pre()`. Do not assume it exists after reboot.
- Never place the Unix socket under `/tmp`.

### BusyBox Utilities

- Do not rely on `$PATH` or BusyBox applets for helper execution. Use absolute paths in `roothelper` allowlist:
  - `/sbin/rc-service`
  - `/sbin/rc-status`
  - `/sbin/rc-update`
  - `/bin/kill`
  - `/sbin/apk`

### Storage Constraints (4 GB eMMC)

- Combined binary footprint target: < 10 MB.
- No SQLite, no BoltDB, no embedded database. All state is in config files or ephemeral memory.
- Log rotation is handled by OpenRC/svlogd or busybox `logrotate` if configured.

### Firewall (iptables/nftables/awall)

- Default init script ordering (`after firewall`) ensures firewall rules are active before `webadmin` binds.
- It is recommended to firewall port 8080/8443 to management VLAN or VPN interfaces only.

## 7. Upgrade Flow

1. `apk add --upgrade alpine-webadmin` (or manual binary replacement).
2. `rc-service webadmin stop`
3. `rc-service roothelper stop`
4. Replace binaries.
5. `rc-service roothelper start`
6. `rc-service webadmin start`

Config files in `/etc/webadmin/` are marked as `config` in `APKBUILD` so they are preserved across upgrades.

## 8. Rollback

- Previous binary kept as `/usr/sbin/webadmin.old` or via `apk` package downgrade.
- No database migrations; rollback is pure binary swap.

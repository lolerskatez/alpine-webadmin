# Alpine WebAdmin — Production Deployment Guide

## Prerequisites

- Alpine Linux 3.18+ (x86_64, aarch64)
- OpenRC init system
- Root access for installation
- TLS certificate (recommended: Let's Encrypt via `acme-client` or self-signed)

## Build

```bash
# Clone or extract source
cd /path/to/alpine-webadmin

# Download frontend dependencies
make deps

# Build binaries (CGO disabled for static linking)
make build-release

# Output:
#   bin/webadmin
#   bin/roothelper
```

## Installation

### 1. Create System User

```bash
adduser -S -D -H -s /sbin/nologin webadmin
```

### 2. Install Binaries

```bash
install -Dm755 bin/webadmin /usr/sbin/webadmin
install -Dm755 bin/roothelper /usr/sbin/roothelper
```

### 3. Create Directories

```bash
install -dm750 /etc/webadmin
install -dm750 /var/lib/webadmin
install -dm750 /var/log/webadmin
install -dm750 /run/webadmin

chown root:webadmin /etc/webadmin /var/lib/webadmin /var/log/webadmin /run/webadmin
```

### 4. Configuration

```bash
cat > /etc/webadmin/config.json <<'EOF'
{
  "listen": ":8443",
  "session_ttl": 3600,
  "ws_max_conns": 50,
  "cookie_secret": "GENERATE_THIS",
  "tls_cert": "/etc/webadmin/cert.pem",
  "tls_key": "/etc/webadmin/key.pem",
  "roothelper_socket": "/run/webadmin/roothelper.sock",
  "log_level": "info"
}
EOF
chmod 640 /etc/webadmin/config.json
chown root:webadmin /etc/webadmin/config.json
```

Generate a strong cookie secret:

```bash
openssl rand -base64 32 > /etc/webadmin/.cookie_secret
chmod 600 /etc/webadmin/.cookie_secret
chown root:root /etc/webadmin/.cookie_secret
```

### 5. Initial Password

```bash
# Set admin password (bcrypt hash)
python3 -c "import bcrypt, sys; print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt(rounds=12)).decode())" "YourStrongPassword" > /etc/webadmin/passwd
chmod 600 /etc/webadmin/passwd
chown root:root /etc/webadmin/passwd
```

Or use the built-in helper:

```bash
/usr/sbin/webadmin -set-password
```

### 6. TLS Certificate

#### Option A: Let's Encrypt (with acme-client)

```bash
apk add acme-client
acme-client -v your-hostname.example.com
ln -s /etc/ssl/acme/your-hostname.example.com/fullchain.pem /etc/webadmin/cert.pem
ln -s /etc/ssl/acme/your-hostname.example.com/key.pem /etc/webadmin/key.pem
```

#### Option B: Self-signed (first boot only)

```bash
openssl req -x509 -newkey rsa:4096 -keyout /etc/webadmin/key.pem \
  -out /etc/webadmin/cert.pem -sha256 -days 365 -nodes \
  -subj "/CN=$(hostname)"
chmod 600 /etc/webadmin/key.pem
```

### 7. OpenRC Services

```bash
cat > /etc/init.d/webadmin <<'EOF'
#!/sbin/openrc-run

description="Alpine WebAdmin Web Server"
pidfile="/run/webadmin/webadmin.pid"
command="/usr/sbin/webadmin"
command_args="-config /etc/webadmin/config.json"
command_background=true
command_user="webadmin:webadmin"
directory="/var/lib/webadmin"

supervisor=supervise-daemon
output_log="/var/log/webadmin/webadmin.log"
error_log="/var/log/webadmin/webadmin.log"

depend() {
    need net roothelper
    after firewall
}
EOF
chmod 755 /etc/init.d/webadmin

cat > /etc/init.d/roothelper <<'EOF'
#!/sbin/openrc-run

description="Alpine WebAdmin Root Helper Daemon"
pidfile="/run/webadmin/roothelper.pid"
command="/usr/sbin/roothelper"
command_args="-config /etc/webadmin/config.json"
command_background=true
command_user="root:root"
directory="/var/lib/webadmin"

supervisor=supervise-daemon
output_log="/var/log/webadmin/roothelper.log"
error_log="/var/log/webadmin/roothelper.log"

depend() {
    need net
}

start_pre() {
    checkpath -d -m 0750 -o root:webadmin /run/webadmin
}
EOF
chmod 755 /etc/init.d/roothelper
```

### 8. Firewall

```bash
# Allow HTTPS (8443) from management network only
# Example: management subnet 10.0.0.0/24
iptables -A INPUT -p tcp --dport 8443 -s 10.0.0.0/24 -j ACCEPT
iptables -A INPUT -p tcp --dport 8443 -j DROP

# Persist rules
iptables-save > /etc/iptables/rules-save
```

### 9. Log Rotation

```bash
cat > /etc/logrotate.d/webadmin <<'EOF'
/var/log/webadmin/*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 0640 root webadmin
    sharedscripts
    postrotate
        /etc/init.d/webadmin reload >/dev/null 2>&1 || true
        /etc/init.d/roothelper reload >/dev/null 2>&1 || true
    endscript
}
EOF
```

### 10. Start Services

```bash
rc-update add roothelper default
rc-update add webadmin default
rc-service roothelper start
rc-service webadmin start
```

## Health Checks

```bash
# Check webadmin is responsive
curl -sfk https://localhost:8443/health || echo "WEBADMIN DOWN"

# Check roothelper socket exists and has correct permissions
ls -la /run/webadmin/roothelper.sock

# Check for goroutine leaks (requires build with debug flags)
curl -sfk https://localhost:8443/debug/pprof/goroutine?debug=1 2>/dev/null | head
```

## Monitoring (Optional)

### Prometheus Metrics

If metrics endpoint is enabled (`-metrics :9090`):

```yaml
# prometheus.yml scrape config
- job_name: 'webadmin'
  static_configs:
    - targets: ['localhost:9090']
```

### Alert Rules

```yaml
groups:
  - name: webadmin
    rules:
      - alert: WebAdminDown
        expr: up{job="webadmin"} == 0
        for: 1m
        labels:
          severity: critical
      - alert: RootHelperDown
        expr: up{job="roothelper"} == 0
        for: 1m
        labels:
          severity: critical
```

## Upgrade Procedure

### Zero-Downtime Upgrade (if single-node)

```bash
# 1. Build new version
make build-release

# 2. Backup current config
cp -a /etc/webadmin /etc/webadmin.bak.$(date +%s)

# 3. Stop webadmin (keep roothelper running)
rc-service webadmin stop

# 4. Install new binary
install -Dm755 bin/webadmin /usr/sbin/webadmin

# 5. Start webadmin
rc-service webadmin start

# 6. Verify health
curl -sfk https://localhost:8443/health

# 7. On failure, rollback
# cp /etc/webadmin.bak.*/config.json /etc/webadmin/config.json
# rc-service webadmin restart
```

### Rolling Upgrade (if clustered / behind LB)

Drain → Stop → Upgrade → Start → Verify → Repeat.

## Performance Tuning

### Kernel Parameters

```bash
# /etc/sysctl.d/99-webadmin.conf
# Increase connection backlog
net.core.somaxconn = 4096

# Reduce TIME_WAIT sockets
net.ipv4.tcp_tw_reuse = 1

# File descriptors for webadmin user
# /etc/security/limits.conf
webadmin soft nofile 65536
webadmin hard nofile 65536
```

### webadmin Config Tuning

```json
{
  "listen": ":8443",
  "session_ttl": 3600,
  "ws_max_conns": 100,
  "log_level": "warn",
  "telemetry_interval": "5s"
}
```

## Troubleshooting

### WebAdmin won't start

```bash
# Check config validity
/usr/sbin/webadmin -config /etc/webadmin/config.json -validate

# Check port conflict
ss -tlnp | grep 8443

# Check permissions
ls -la /etc/webadmin/
ls -la /usr/sbin/webadmin
id webadmin
```

### RootHelper connection refused

```bash
# Verify socket exists
ls -la /run/webadmin/roothelper.sock

# Verify webadmin can write to socket (same group)
stat -c '%U:%G %a' /run/webadmin/roothelper.sock

# Check roothelper logs
tail -f /var/log/webadmin/roothelper.log
```

### High memory usage

```bash
# Check goroutine count
curl -sfk https://localhost:8443/debug/pprof/goroutine?debug=1 | grep -c '^goroutine'

# Check heap profile
curl -sfk https://localhost:8443/debug/pprof/heap > /tmp/heap.prof
go tool pprof /tmp/heap.prof
```

### TLS errors

```bash
# Verify certificate
openssl x509 -in /etc/webadmin/cert.pem -text -noout | head -20

# Verify key matches certificate
openssl x509 -noout -modulus -in /etc/webadmin/cert.pem | openssl md5
openssl rsa -noout -modulus -in /etc/webadmin/key.pem | openssl md5
```

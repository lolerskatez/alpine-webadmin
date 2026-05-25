# Alpine WebAdmin — Disaster Recovery Guide

## Recovery Scenarios

### 1. Forgotten Admin Password

**Symptom**: Cannot log in to WebAdmin.

**Recovery**:

```bash
# Stop webadmin to prevent race conditions
rc-service webadmin stop

# Generate new bcrypt hash
python3 -c "import bcrypt, sys; print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt()).decode())" "NewSecurePassword123" > /etc/webadmin/passwd
chmod 600 /etc/webadmin/passwd

# Restart
rc-service webadmin start
```

**Prevention**: Store password hash in offline password manager; backup `/etc/webadmin/passwd`.

---

### 2. WebAdmin Service Down

**Symptom**: `curl -k https://localhost:8443/health` returns connection refused.

**Recovery**:

```bash
# Check if process exists
ps aux | grep webadmin

# Check logs for fatal error
tail -n 50 /var/log/webadmin/webadmin.log

# Common fixes:
# - Port conflict: change listen port in config
# - Permission error: fix /etc/webadmin permissions
# - Config syntax: validate with 'webadmin -validate'

# Emergency: start with minimal config
/usr/sbin/webadmin -listen :8080 -no-tls -config /etc/webadmin/config.json
```

---

### 3. RootHelper Service Down

**Symptom**: All API operations return "IPC error" or 503.

**Recovery**:

```bash
# Check roothelper status
rc-service roothelper status

# Check logs
tail -n 50 /var/log/webadmin/roothelper.log

# Verify socket permissions
ls -la /run/webadmin/roothelper.sock
# Should show: srw-rw---- root webadmin

# Fix permissions if wrong
chown root:webadmin /run/webadmin
chmod 750 /run/webadmin
rc-service roothelper restart
rc-service webadmin restart
```

---

### 4. TLS Certificate Expired

**Symptom**: Browser shows certificate error; curl returns `SSL certificate problem`.

**Recovery**:

```bash
# Generate emergency self-signed certificate
openssl req -x509 -newkey rsa:4096 -keyout /etc/webadmin/key.pem \
  -out /etc/webadmin/cert.pem -sha256 -days 30 -nodes \
  -subj "/CN=$(hostname)"
chmod 600 /etc/webadmin/key.pem

rc-service webadmin restart

# Then schedule real certificate renewal
```

---

### 5. Database / Config Corruption

**Symptom**: WebAdmin crashes on startup with config parse errors.

**Recovery**:

```bash
# List backups
ls -la /etc/webadmin.bak.*

# Restore from latest backup
LATEST=$(ls -d /etc/webadmin.bak.* | tail -1)
cp "$LATEST/config.json" /etc/webadmin/config.json

# Or generate minimal config
cat > /etc/webadmin/config.json <<'EOF'
{
  "listen": ":8443",
  "session_ttl": 3600,
  "ws_max_conns": 50,
  "cookie_secret": "EMERGENCY_SECRET_CHANGE_THIS",
  "log_level": "info"
}
EOF

rc-service webadmin restart
```

---

### 6. Goroutine Leak / Memory Exhaustion

**Symptom**: WebAdmin process uses excessive RAM; OOM kills.

**Recovery**:

```bash
# Emergency restart
rc-service webadmin restart

# If debug endpoint available, capture profile
curl -sfk https://localhost:8443/debug/pprof/heap > /tmp/heap.prof
curl -sfk https://localhost:8443/debug/pprof/goroutine?debug=2 > /tmp/goroutines.txt

# Analyze offline
go tool pprof /tmp/heap.prof
```

---

### 7. Compromised Session

**Symptom**: Unauthorized actions in logs; unknown sessions in `/api/sessions`.

**Recovery**:

```bash
# Revoke ALL sessions immediately
# (webadmin in-memory store is cleared on restart)
rc-service webadmin restart

# Rotate cookie secret (forces all sessions invalid)
openssl rand -base64 32 > /etc/webadmin/.cookie_secret
rc-service webadmin restart

# Review roothelper audit log for unauthorized commands
grep -i "unauthorized\|error\|fail" /var/log/webadmin/roothelper.log

# Check for filesystem modifications
apk verify
find /etc -type f -newer /etc/webadmin/config.json
```

---

### 8. Diskless Alpine: Data Loss on Reboot

**Symptom**: Configuration changes lost after reboot on diskless/ramdisk system.

**Recovery**:

```bash
# On Alpine diskless systems, use lbu to persist changes
lbu add /etc/webadmin/config.json
lbu add /etc/webadmin/passwd
lbu add /etc/init.d/webadmin
lbu add /etc/init.d/roothelper
lbu add /usr/sbin/webadmin
lbu add /usr/sbin/roothelper
lbu commit

# For overlay systems:
lbu commit -d
```

---

### 9. Network Isolation (Emergency Console Access)

**Symptom**: WebAdmin unreachable due to firewall / network misconfiguration.

**Recovery**:

```bash
# Physical or serial console required
# Stop firewall temporarily
rc-service iptables stop

# Bind webadmin to localhost only for emergency access via SSH tunnel
sed -i 's/"listen": ".*"/"listen": "127.0.0.1:8443"/' /etc/webadmin/config.json
rc-service webadmin restart

# From management workstation, create tunnel
ssh -L 8443:localhost:8443 root@server
# Access via https://localhost:8443 on workstation
```

---

### 10. Complete System Rebuild

**Scenario**: Hardware failure; fresh Alpine install.

**Recovery Steps**:

```bash
# 1. Install Alpine Linux base
# 2. Install dependencies
apk add openssl ca-certificates

# 3. Restore binaries from backup or rebuild
# (see deployment-guide.md for build instructions)

# 4. Restore configuration
tar -xzf /backup/webadmin-config-$(date +%Y%m%d).tar.gz -C /

# 5. Restore password hash
cp /backup/passwd /etc/webadmin/passwd
chmod 600 /etc/webadmin/passwd

# 6. Restore TLS certificates
cp /backup/cert.pem /backup/key.pem /etc/webadmin/
chmod 600 /etc/webadmin/key.pem

# 7. Install services and start
rc-update add roothelper default
rc-update add webadmin default
rc-service roothelper start
rc-service webadmin start
```

---

## Backup Procedures

### Daily Backup Script

```bash
#!/bin/sh
# /etc/periodic/daily/webadmin-backup

BACKUP_DIR="/var/backups/webadmin"
DATE=$(date +%Y%m%d_%H%M%S)
mkdir -p "$BACKUP_DIR"

tar -czf "$BACKUP_DIR/config-$DATE.tar.gz" \
    /etc/webadmin/config.json \
    /etc/webadmin/passwd \
    /etc/init.d/webadmin \
    /etc/init.d/roothelper \
    /etc/webadmin/cert.pem \
    /etc/webadmin/key.pem 2>/dev/null

# Keep last 30 backups
ls -1t "$BACKUP_DIR"/config-*.tar.gz | tail -n +31 | xargs -r rm -f
```

### Off-Site Backup

```bash
# Sync to remote storage
rsync -avz --delete /var/backups/webadmin/ backup@remote:/backups/webadmin/
```

---

## Emergency Contacts & Procedures

### Response Playbook

| Severity | Response Time | Action |
|----------|---------------|--------|
| Critical (down) | 15 min | Restart services → check logs → escalate |
| High (compromised) | 30 min | Revoke sessions → rotate secrets → investigate |
| Medium (degraded) | 2 hours | Profile → tune → restart |
| Low (warning) | 24 hours | Schedule maintenance window |

### Checklist: Post-Incident

- [ ] Root cause documented
- [ ] Sessions revoked and secrets rotated
- [ ] Configuration restored from known-good backup
- [ ] Binaries verified (checksum / recompile)
- [ ] Audit logs reviewed for scope of impact
- [ ] Monitoring alerts updated to catch recurrence
- [ ] Lessons learned documented

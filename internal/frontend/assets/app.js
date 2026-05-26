/* ============================================================
   Alpine WebAdmin — Frontend Application
   Alpine.js data components with WebSocket + SSE
   ============================================================ */

const VERSION = '1.0';
const WS_RECONNECT_DELAY = 3000;
const LOG_MAX_LINES = 500;
const PKG_POLL_INTERVAL = 2000;
const ALERT_DURATION = 5000;

function app() {
    return {
        // ── Auth ──────────────────────────────────────────
        authenticated: false,
        password: '',
        loginError: '',
        loginLoading: false,
        csrfToken: '',

        // ── Navigation ────────────────────────────────────
        tab: 'dashboard',
        sidebarOpen: false,
        version: VERSION,

        // ── Telemetry / Dashboard ─────────────────────────
        telemetry: {},
        cpuTrend: '',
        lastCpu: 0,

        // ── Services ──────────────────────────────────────
        services: [],
        serviceFilter: '',

        // ── Packages ──────────────────────────────────────
        packages: [],
        packageFilter: '',
        pkgLoading: false,
        pkgOp: null,
        pkgOpLoading: false,
        pkgPollTimer: null,
        confirmRemovePackage: '',

        // ── Network ───────────────────────────────────────
        networkData: [],

        // ── Storage ───────────────────────────────────────
        storageData: [],

        // ── Users ─────────────────────────────────────────
        users: [],
        showCreateUser: false,
        newUser: { username: '', home: '', shell: '' },
        userError: '',

        // ── Logs ──────────────────────────────────────────
        logLines: [],
        logFilter: '',
        logsRunning: false,
        logSource: null,
        logIdCounter: 0,

        // ── Sessions ──────────────────────────────────────
        sessions: [],

        // ── System ────────────────────────────────────────
        confirmReboot: false,
        confirmShutdown: false,

        // ── Alerts ────────────────────────────────────────
        alerts: [],
        alertIdCounter: 0,

        // ── WebSocket ─────────────────────────────────────
        ws: null,
        wsReconnectTimer: null,

        // ── Init ──────────────────────────────────────────
        initApp() {
            this.checkSession();
        },

        // ── Session ───────────────────────────────────────
        async checkSession() {
            try {
                const r = await fetch('/api/session', { credentials: 'same-origin' });
                if (r.ok) {
                    this.authenticated = true;
                    const data = await r.json();
                    this.readCSRFCookie();
                    this.initWS();
                    this.loadTabData();
                }
            } catch (e) {
                // not authenticated
            }
        },

        async login() {
            this.loginError = '';
            this.loginLoading = true;
            try {
                const r = await fetch('/api/login', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ password: this.password })
                });
                if (r.ok) {
                    this.authenticated = true;
                    this.password = '';
                    this.readCSRFCookie();
                    // Defer WS open one tick so the browser commits Set-Cookie
                    // before the WebSocket upgrade request is sent.
                    setTimeout(() => this.initWS(), 0);
                    this.loadTabData();
                } else {
                    this.loginError = r.status === 429 ? 'Rate limited. Try again later.' : 'Invalid password';
                }
            } catch (e) {
                this.loginError = 'Network error';
            } finally {
                this.loginLoading = false;
            }
        },

        async logout() {
            try {
                await fetch('/api/logout', {
                    method: 'DELETE',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken }
                });
            } catch (e) {}
            this.authenticated = false;
            this.cleanup();
        },

        readCSRFCookie() {
            const m = document.cookie.match(/webadmin-csrf=([^;]+)/);
            this.csrfToken = m ? m[1] : '';
        },

        cleanup() {
            if (this.ws) { this.ws.close(); this.ws = null; }
            if (this.wsReconnectTimer) { clearTimeout(this.wsReconnectTimer); this.wsReconnectTimer = null; }
            if (this.logSource) { this.logSource.close(); this.logSource = null; }
            if (this.pkgPollTimer) { clearInterval(this.pkgPollTimer); this.pkgPollTimer = null; }
            this.logsRunning = false;
            this.logLines = [];
        },

        // ── Navigation ────────────────────────────────────
        switchTab(name) {
            this.tab = name;
            this.sidebarOpen = false;
            this.loadTabData();
            // Focus main content for screen readers
            this.$nextTick(() => {
                const main = document.getElementById('main-content');
                if (main) main.focus();
            });
        },

        loadTabData() {
            switch (this.tab) {
                case 'services': this.loadServices(); break;
                case 'packages': this.loadPackages(); break;
                case 'network': this.loadNetwork(); break;
                case 'storage': this.loadStorage(); break;
                case 'users': this.loadUsers(); break;
                case 'logs': break; // handled by toggle
                case 'sessions': this.loadSessions(); break;
                case 'system': break; // static
            }
        },

        // ── WebSocket ─────────────────────────────────────
        initWS() {
            if (this.ws) return;
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            this.ws = new WebSocket(`${proto}//${location.host}/ws`);
            this.ws.onmessage = (ev) => this.handleWSMessage(ev);
            this.ws.onerror = () => {
                if (this.ws) this.ws.close();
            };
            this.ws.onclose = () => {
                this.ws = null;
                if (this.authenticated) {
                    this.wsReconnectTimer = setTimeout(() => this.initWS(), WS_RECONNECT_DELAY);
                }
            };
        },

        handleWSMessage(ev) {
            try {
                const msg = JSON.parse(ev.data);
                if (msg.type === 'telemetry') {
                    this.telemetry = msg.payload || {};
                    this.updateCpuTrend();
                }
            } catch (e) {}
        },

        updateCpuTrend() {
            const cpu = this.telemetry.cpu?.aggregate ?? 0;
            if (cpu > this.lastCpu + 5) this.cpuTrend = 'up';
            else if (cpu < this.lastCpu - 5) this.cpuTrend = 'down';
            else this.cpuTrend = '';
            this.lastCpu = cpu;
        },

        // ── Dashboard Helpers ─────────────────────────────
        diskUsedPercent() {
            const mounts = this.telemetry.mounts || [];
            if (!mounts.length) return 0;
            let total = 0, used = 0;
            for (const m of mounts) {
                total += m.total_kb || 0;
                used += m.used_kb || 0;
            }
            return total ? Math.round(used / total * 100) : 0;
        },

        diskBarClass(used, total) {
            const pct = total ? used / total * 100 : 0;
            return pct > 90 ? 'bar-danger' : pct > 75 ? 'bar-warning' : '';
        },

        // ── Services ──────────────────────────────────────
        async loadServices() {
            try {
                const r = await fetch('/api/services', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.services = (data.services || []).map(s => ({ ...s, _loading: false }));
            } catch (e) { this.showAlert('Failed to load services', 'error'); }
        },

        filteredServices() {
            const f = this.serviceFilter.toLowerCase();
            if (!f) return this.services;
            return this.services.filter(s => s.name.toLowerCase().includes(f));
        },

        async serviceAction(name, action) {
            const svc = this.services.find(s => s.name === name);
            if (svc) svc._loading = true;
            const urlMap = {
                start: `/api/services/${name}/start`,
                stop: `/api/services/${name}/stop`,
                restart: `/api/services/${name}/restart`,
                enable: '/api/services/enable',
                disable: '/api/services/disable',
            };
            try {
                const body = ['enable','disable'].includes(action) ? JSON.stringify({ name }) : null;
                const r = await fetch(urlMap[action], {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: body
                });
                if (r.ok) {
                    this.showAlert(`${name}: ${action} succeeded`, 'success');
                    setTimeout(() => this.loadServices(), 500);
                } else {
                    const err = await r.text();
                    this.showAlert(`${name}: ${action} failed — ${err}`, 'error');
                }
            } catch (e) {
                this.showAlert(`${name}: ${action} failed`, 'error');
            } finally {
                if (svc) svc._loading = false;
            }
        },

        // ── Packages ──────────────────────────────────────
        async loadPackages() {
            this.pkgLoading = true;
            this.packageFilter = '';
            try {
                const r = await fetch('/api/packages', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.packages = (data.packages || []).map(p => ({ ...p, installed: true }));
            } catch (e) { this.showAlert('Failed to load packages', 'error'); }
            this.pkgLoading = false;
        },

        async searchPackages() {
            const q = this.packageFilter.trim();
            if (!q) { this.loadPackages(); return; }
            this.pkgLoading = true;
            try {
                const r = await fetch(`/api/packages/search?q=${encodeURIComponent(q)}`, { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.packages = data.packages || [];
            } catch (e) { this.showAlert('Search failed', 'error'); }
            this.pkgLoading = false;
        },

        async installPackage(name) {
            await this.runPackageOp('install', [name]);
        },

        showRemoveConfirm(name) {
            this.confirmRemovePackage = name;
        },

        async removePackage(name) {
            this.confirmRemovePackage = '';
            await this.runPackageOp('remove', [name]);
        },

        async packageAction(action) {
            if (action === 'update' || action === 'upgrade') {
                await this.runPackageOp(action, []);
            }
        },

        async runPackageOp(type, packages) {
            this.pkgOpLoading = true;
            const endpoint = `/api/packages/${type}`;
            const body = packages.length ? JSON.stringify({ packages }) : '{}';
            try {
                const r = await fetch(endpoint, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body
                });
                if (!r.ok) { this.showAlert(`${type} failed`, 'error'); return; }
                const data = await r.json();
                this.pkgOp = { ...data, type, progress: data.progress || [] };
                this.startPkgPoll();
            } catch (e) {
                this.showAlert(`${type} failed`, 'error');
            } finally {
                this.pkgOpLoading = false;
            }
        },

        startPkgPoll() {
            if (this.pkgPollTimer) clearInterval(this.pkgPollTimer);
            this.pkgPollTimer = setInterval(async () => {
                if (!this.pkgOp || !this.pkgOp.op_id) return;
                try {
                    const r = await fetch(`/api/packages/op?op_id=${this.pkgOp.op_id}`, { credentials: 'same-origin' });
                    if (!r.ok) return;
                    const data = await r.json();
                    this.pkgOp = { ...this.pkgOp, ...data, progress: data.progress || this.pkgOp.progress };
                    if (data.state === 'completed' || data.state === 'failed') {
                        clearInterval(this.pkgPollTimer);
                        this.pkgPollTimer = null;
                        if (data.state === 'completed') this.showAlert('Package operation completed', 'success');
                        else this.showAlert('Package operation failed: ' + (data.error || ''), 'error');
                    }
                } catch (e) {}
            }, PKG_POLL_INTERVAL);
        },

        // ── Network ─────────────────────────────────────────
        async loadNetwork() {
            try {
                const r = await fetch('/api/network', { credentials: 'same-origin' });
                if (!r.ok) return;
                const text = await r.text();
                this.networkData = this.parseNetDev(text);
            } catch (e) { this.showAlert('Failed to load network data', 'error'); }
        },

        parseNetDev(text) {
            const lines = text.split('\n');
            const result = [];
            for (let i = 2; i < lines.length; i++) {
                const line = lines[i].trim();
                if (!line) continue;
                const [iface, rest] = line.split(':');
                if (!iface || !rest) continue;
                const vals = rest.trim().split(/\s+/).map(Number);
                result.push({
                    interface: iface.trim(),
                    rx_bytes: vals[0] || 0,
                    rx_packets: vals[1] || 0,
                    tx_bytes: vals[8] || 0,
                    tx_packets: vals[9] || 0,
                });
            }
            return result;
        },

        // ── Storage ─────────────────────────────────────────
        async loadStorage() {
            try {
                const r = await fetch('/api/storage', { credentials: 'same-origin' });
                if (!r.ok) return;
                const text = await r.text();
                this.storageData = this.parseDf(text);
            } catch (e) { this.showAlert('Failed to load storage data', 'error'); }
        },

        parseDf(text) {
            const lines = text.split('\n').slice(1);
            return lines.map(l => {
                const parts = l.trim().split(/\s+/);
                if (parts.length < 6) return null;
                const useStr = parts[4].replace('%', '');
                return {
                    filesystem: parts[0],
                    size: parts[1],
                    used: parts[2],
                    available: parts[3],
                    usePercent: parseInt(useStr) || 0,
                    mountpoint: parts[5],
                };
            }).filter(Boolean);
        },

        // ── Users ─────────────────────────────────────────
        async loadUsers() {
            try {
                const r = await fetch('/api/users', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.users = await r.json();
            } catch (e) { this.showAlert('Failed to load users', 'error'); }
        },

        async createUser() {
            this.userError = '';
            try {
                const r = await fetch('/api/users', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify(this.newUser)
                });
                if (r.ok) {
                    this.showAlert('User created', 'success');
                    this.showCreateUser = false;
                    this.newUser = { username: '', home: '', shell: '' };
                    this.loadUsers();
                } else {
                    this.userError = await r.text();
                }
            } catch (e) {
                this.userError = 'Network error';
            }
        },

        async resetPassword(username) {
            const u = this.users.find(x => x.username === username);
            if (u) u._loading = true;
            try {
                const r = await fetch(`/api/users/${username}/password`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken }
                });
                if (r.ok) this.showAlert(`Password reset for ${username}`, 'success');
                else this.showAlert(`Failed to reset password for ${username}`, 'error');
            } catch (e) {
                this.showAlert('Password reset failed', 'error');
            } finally {
                if (u) u._loading = false;
            }
        },

        // ── Logs ──────────────────────────────────────────
        toggleLogs() {
            if (this.logsRunning) {
                this.stopLogs();
            } else {
                this.startLogs();
            }
        },

        startLogs() {
            if (this.logSource) return;
            this.logsRunning = true;
            this.logSource = new EventSource('/api/logs');
            this.logSource.onmessage = (ev) => {
                this.addLogLine(ev.data, 'info');
            };
            this.logSource.onerror = () => {
                this.stopLogs();
                this.showAlert('Log stream disconnected', 'warning');
            };
        },

        stopLogs() {
            this.logsRunning = false;
            if (this.logSource) { this.logSource.close(); this.logSource = null; }
        },

        addLogLine(text, level) {
            this.logLines.push({ id: ++this.logIdCounter, text, level });
            if (this.logLines.length > LOG_MAX_LINES) {
                this.logLines = this.logLines.slice(-LOG_MAX_LINES);
            }
            // Auto-scroll
            this.$nextTick(() => {
                const el = this.$refs.logContainer;
                if (el) el.scrollTop = el.scrollHeight;
            });
        },

        clearLogs() {
            this.logLines = [];
            this.logIdCounter = 0;
        },

        filteredLogs() {
            const f = this.logFilter.toLowerCase();
            if (!f) return this.logLines;
            return this.logLines.filter(l => l.text.toLowerCase().includes(f));
        },

        // ── Sessions ──────────────────────────────────────
        async loadSessions() {
            try {
                const r = await fetch('/api/sessions', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                // Identify current session via cookie (best effort)
                const sidMatch = document.cookie.match(/webadmin-sid=([^;]+)/);
                const currentSid = sidMatch ? sidMatch[1] : '';
                this.sessions = (data || []).map(s => ({
                    ...s,
                    isCurrent: s.id === currentSid
                }));
            } catch (e) { this.showAlert('Failed to load sessions', 'error'); }
        },

        async revokeSession(id) {
            try {
                const r = await fetch(`/api/sessions/${id}`, {
                    method: 'DELETE',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken }
                });
                if (r.ok) {
                    this.showAlert('Session revoked', 'success');
                    this.loadSessions();
                }
            } catch (e) { this.showAlert('Failed to revoke session', 'error'); }
        },

        // ── System ────────────────────────────────────────
        async powerAction(action) {
            this.confirmReboot = false;
            this.confirmShutdown = false;
            const endpoint = action === 'reboot' ? '/api/reboot' : '/api/shutdown';
            try {
                const r = await fetch(endpoint, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: '{}'
                });
                if (r.ok) this.showAlert(`${action} initiated`, 'warning');
                else this.showAlert(`${action} failed`, 'error');
            } catch (e) {
                this.showAlert(`${action} failed`, 'error');
            }
        },

        // ── Alerts ────────────────────────────────────────
        showAlert(message, level = 'info') {
            const id = ++this.alertIdCounter;
            this.alerts.push({ id, message, level });
            setTimeout(() => this.dismissAlert(id), ALERT_DURATION);
        },

        dismissAlert(id) {
            this.alerts = this.alerts.filter(a => a.id !== id);
        },

        // ── Formatters ────────────────────────────────────
        formatKB(kb) {
            if (kb == null || isNaN(kb)) return '-';
            if (kb >= 1073741824) return (kb / 1073741824).toFixed(2) + ' TB';
            if (kb >= 1048576) return (kb / 1048576).toFixed(2) + ' GB';
            if (kb >= 1024) return (kb / 1024).toFixed(2) + ' MB';
            return kb + ' KB';
        },

        formatBytes(b) {
            if (b == null || isNaN(b)) return '-';
            if (b >= 1099511627776) return (b / 1099511627776).toFixed(2) + ' TiB';
            if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' GiB';
            if (b >= 1048576) return (b / 1048576).toFixed(2) + ' MiB';
            if (b >= 1024) return (b / 1024).toFixed(2) + ' KiB';
            return b + ' B';
        },

        formatNumber(n) {
            if (n == null || isNaN(n)) return '-';
            if (n >= 1e9) return (n / 1e9).toFixed(1) + 'G';
            if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
            if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K';
            return n.toString();
        },

        formatDuration(sec) {
            if (sec == null || isNaN(sec)) return '-';
            const d = Math.floor(sec / 86400);
            const h = Math.floor((sec % 86400) / 3600);
            const m = Math.floor((sec % 3600) / 60);
            const parts = [];
            if (d > 0) parts.push(d + 'd');
            if (h > 0) parts.push(h + 'h');
            if (m > 0) parts.push(m + 'm');
            return parts.join(' ') || '0m';
        },

        formatTime(ts) {
            if (!ts) return '-';
            const d = new Date(ts * 1000);
            return d.toLocaleString();
        }
    };
}

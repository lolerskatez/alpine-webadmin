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
        packageView: 'all', // 'all' | 'available' | 'installed'
        pkgLoading: false,
        pkgOp: null,
        pkgOpLoading: false,
        pkgPollTimer: null,
        confirmRemovePackage: '',
        pkgPageSize: 50,
        pkgVisibleCount: 50,

        // ── Network ───────────────────────────────────────
        networkData: [],

        // ── Storage ───────────────────────────────────────
        storageData: [],
        showMountForm: false,
        mountForm: { device: '', mountpoint: '', fstype: '' },

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
        sysInfo: {},
        newHostname: '',
        showHostnameEdit: false,
        updatesCount: 0,

        // ── Kernel Modules ────────────────────────────────
        kmodModules: [],
        showKModForm: false,
        kmodForm: { module: '' },

        // ── APK Repositories ──────────────────────────────
        apkRepos: '',
        showApkRepoEdit: false,

        // ── SSH Config ────────────────────────────────────
        sshConfig: '',
        showSshConfigEdit: false,

        // ── Cron ──────────────────────────────────────────
        cronContent: '',
        showCronEdit: false,

        // ── Processes ─────────────────────────────────────
        processes: [],
        processFilter: '',

        // ── Log History ───────────────────────────────────
        logHistory: [],
        logHistoryTotal: 0,
        logHistoryFilter: '',
        logHistoryOffset: 0,
        logHistoryLimit: 100,

        // ── Network Interfaces ────────────────────────────
        networkInterfaces: [],

        // ── fstab ─────────────────────────────────────────
        fstabContent: '',
        showFstabEdit: false,

        // ── Timezone / NTP ────────────────────────────────
        timezone: '',
        ntpEnabled: false,
        showTimezoneEdit: false,

        // ── Service Log ───────────────────────────────────
        serviceLogName: '',
        serviceLogContent: '',
        serviceLogStatus: '',
        showServiceLog: false,

        // ── User Groups ───────────────────────────────────
        groupManageUser: '',
        userGroups: [],
        availableGroups: [],
        newGroupName: '',
        showGroupManage: false,

        // ── System Alerts ─────────────────────────────────
        systemAlerts: [],
        systemAlertsLoading: false,

        // ── Block Devices ─────────────────────────────────
        blockDevices: [],

        // ── Shell Change ────────────────────────────────────
        newShell: '',
        showShellChange: false,
        shellChangeUser: '',

        // ── LBU ───────────────────────────────────────────
        lbuStatus: '',
        lbuBackups: [],
        showLbuRestore: false,
        lbuRestoreBackup: '',

        // ── Audit Log ─────────────────────────────────────
        auditLog: [],
        auditLogLoading: false,

        // ── SSH Keys ──────────────────────────────────────
        sshKeysContent: '',
        showSshKeys: false,
        sshKeysUser: '',

        // ── Resolv ────────────────────────────────────────
        resolvContent: '',
        showResolvEdit: false,

        // ── Firewall ──────────────────────────────────────
        firewallContent: '',

        // ── Routes ────────────────────────────────────────
        routesContent: '',

        // ── Clock ─────────────────────────────────────────
        clockDatetime: '',

        // ── File Viewer ───────────────────────────────────
        fileViewerPath: '',
        fileViewerContent: '',

        // ── Log Search ────────────────────────────────────
        logSearchQuery: '',
        logSearchResults: [],
        logSearchCount: 0,

        // ── WiFi ────────────────────────────────────────────
        wifiScanContent: '',

        // ── CPU Info ──────────────────────────────────────
        cpuInfo: {},

        // ── Memory Info ─────────────────────────────────
        memInfo: {},

        // ── dmesg ─────────────────────────────────────────
        dmesgContent: '',

        // ── Network Addresses ───────────────────────────
        netAddrContent: '',

        // ── SMART ─────────────────────────────────────────
        smartDev: '/dev/sda',
        smartContent: '',

        // ── SSH Host Keys ─────────────────────────────────
        sshHostKeys: [],

        // ── MOTD ──────────────────────────────────────────
        motdContent: '',
        issueContent: '',

        // ── Boot Parameters ───────────────────────────────
        cmdlineContent: '',

        // ── Swaps ─────────────────────────────────────────
        swapsContent: '',

        // ── USB Devices ───────────────────────────────────
        usbDevicesContent: '',

        // ── PCI Devices ───────────────────────────────────
        pciDevicesContent: '',

        // ── Network Connections ───────────────────────────
        netConnectionsContent: '',

        // ── Environment Variables ─────────────────────────
        environVars: {},

        // ── Package Depends ───────────────────────────────
        packageDepends: {},
        packageDependsName: '',

        // ── ARP Table ─────────────────────────────────────
        arpContent: '',

        // ── Kernel Version ────────────────────────────────
        kernelVersionContent: '',

        // ── Disk Partitions ───────────────────────────────
        partitionsContent: '',

        // ── Services Status ───────────────────────────────
        servicesStatusContent: '',

        // ── Alerts ────────────────────────────────────────
        alerts: [],
        alertIdCounter: 0,

        // ── WebSocket ─────────────────────────────────────
        ws: null,
        wsReconnectTimer: null,

        // ── Init ──────────────────────────────────────────
        initApp() {
            this.checkSession();
            this.loadSystemAlerts();
        },

        async loadUpdates() {
            try {
                const r = await fetch('/api/system/updates', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.updatesCount = data.count || 0;
            } catch (e) { /* silent */ }
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
                    this.loadUpdates();
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
                    this.loadUpdates();
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
                case 'dashboard': this.loadSystemAlerts(); break;
                case 'services': this.loadServices(); this.loadServicesStatus(); break;
                case 'packages': this.searchPackages(); this.loadApkRepos(); break;
                case 'network': this.loadNetwork(); this.loadNetworkInterfaces(); this.loadFirewall(); this.loadRoutes(); this.loadWifiScan(); this.loadNetAddr(); this.loadNetConnections(); this.loadArp(); break;
                case 'storage': this.loadStorage(); this.loadFstab(); this.loadBlockDevices(); this.loadSmart(); this.loadPartitions(); break;
                case 'users': this.loadUsers(); break;
                case 'logs': this.loadLogHistory(); break;
                case 'sessions': this.loadSessions(); break;
                case 'system': this.loadSystemInfo(); this.loadSshConfig(); this.loadTimezone(); this.loadNtp(); this.loadLbu(); this.loadCpuInfo(); this.loadMemInfo(); this.loadDmesg(); this.loadSshHostKeys(); this.loadMotd(); this.loadCmdline(); this.loadSwaps(); this.loadUsbDevices(); this.loadPciDevices(); this.loadEnviron(); this.loadKernelVersion(); break;
                case 'modules': this.loadKMod(); break;
                case 'cron': this.loadCron(); break;
                case 'processes': this.loadProcesses(); break;
                case 'audit': this.loadAuditLog(); break;
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
        setPackageView(view) {
            this.packageView = view;
            this.searchPackages();
        },

        async loadPackages() {
            this.pkgLoading = true;
            try {
                const r = await fetch('/api/packages', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.packages = (data.packages || []).map(p => ({ ...p, installed: true }));
                this.pkgVisibleCount = this.pkgPageSize;
            } catch (e) { this.showAlert('Failed to load packages', 'error'); }
            this.pkgLoading = false;
        },

        async searchPackages() {
            const q = this.packageFilter.trim();
            const view = this.packageView;

            // Installed view with no query: use the fast installed-only endpoint
            if (view === 'installed' && !q) {
                await this.loadPackages();
                return;
            }

            this.pkgLoading = true;
            try {
                const r = await fetch(`/api/packages/search?q=${encodeURIComponent(q)}`, { credentials: 'same-origin' });
                if (!r.ok) { this.showAlert('Search failed', 'error'); return; }
                const data = await r.json();
                let pkgs = data.packages || [];
                if (view === 'installed') {
                    pkgs = pkgs.filter(p => p.installed);
                } else if (view === 'available') {
                    pkgs = pkgs.filter(p => !p.installed);
                }
                this.packages = pkgs;
                this.pkgVisibleCount = this.pkgPageSize;
            } catch (e) {
                this.showAlert('Search failed', 'error');
            } finally {
                this.pkgLoading = false;
            }
        },

        loadMorePackages() {
            this.pkgVisibleCount = Math.min(this.pkgVisibleCount + this.pkgPageSize, this.packages.length);
        },

        get visiblePackages() {
            return this.packages.slice(0, this.pkgVisibleCount);
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
            let errorCount = 0;
            const stop = () => {
                clearInterval(this.pkgPollTimer);
                this.pkgPollTimer = null;
            };
            this.pkgPollTimer = setInterval(async () => {
                if (!this.pkgOp || !this.pkgOp.op_id) { stop(); return; }
                try {
                    const r = await fetch(`/api/packages/op?op_id=${this.pkgOp.op_id}`, { credentials: 'same-origin' });
                    if (!r.ok) {
                        errorCount++;
                        if (errorCount >= 3) {
                            stop();
                            this.showAlert('Package operation completed (status unavailable)', 'success');
                            this.pkgOp = null;
                            this.loadPackages();
                        }
                        return;
                    }
                    errorCount = 0;
                    const data = await r.json();
                    this.pkgOp = { ...this.pkgOp, ...data, progress: data.progress || this.pkgOp.progress };
                    if (data.state === 'completed' || data.state === 'failed') {
                        stop();
                        if (data.state === 'completed') {
                            this.showAlert('Package operation completed', 'success');
                            this.loadPackages();
                        } else {
                            this.showAlert('Package operation failed: ' + (data.error || ''), 'error');
                        }
                    }
                } catch (e) {
                    errorCount++;
                    if (errorCount >= 5) stop();
                }
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

        async mountDevice() {
            const { device, mountpoint, fstype } = this.mountForm;
            if (!device.trim() || !mountpoint.trim()) {
                this.showAlert('Device and mountpoint are required', 'error');
                return;
            }
            try {
                const r = await fetch('/api/mount', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ device: device.trim(), mountpoint: mountpoint.trim(), fstype: fstype.trim() || undefined })
                });
                if (r.ok) {
                    this.showAlert('Device mounted', 'success');
                    this.showMountForm = false;
                    this.mountForm = { device: '', mountpoint: '', fstype: '' };
                    this.loadStorage();
                } else {
                    const err = await r.text();
                    this.showAlert('Mount failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Mount failed', 'error'); }
        },

        async unmountDevice(mountpoint) {
            if (!confirm(`Unmount ${mountpoint}?`)) return;
            try {
                const r = await fetch('/api/unmount', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ mountpoint })
                });
                if (r.ok) {
                    this.showAlert('Device unmounted', 'success');
                    this.loadStorage();
                } else {
                    const err = await r.text();
                    this.showAlert('Unmount failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Unmount failed', 'error'); }
        },

        // ── Users ─────────────────────────────────────────
        async loadUsers() {
            try {
                const r = await fetch('/api/users', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.users = await r.json();
            } catch (e) { this.showAlert('Failed to load users', 'error'); }
        },

        async deleteUser(username) {
            if (!confirm(`Delete user ${username}?`)) return;
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(username)}/delete`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username })
                });
                if (r.ok) {
                    this.showAlert('User deleted', 'success');
                    this.loadUsers();
                } else {
                    const err = await r.text();
                    this.showAlert('Delete failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Delete failed', 'error'); }
        },

        async lockUser(username) {
            if (!confirm(`Lock user ${username}?`)) return;
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(username)}/lock`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username })
                });
                if (r.ok) {
                    this.showAlert('User locked', 'success');
                    this.loadUsers();
                } else {
                    const err = await r.text();
                    this.showAlert('Lock failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Lock failed', 'error'); }
        },

        async unlockUser(username) {
            if (!confirm(`Unlock user ${username}?`)) return;
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(username)}/unlock`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username })
                });
                if (r.ok) {
                    this.showAlert('User unlocked', 'success');
                    this.loadUsers();
                } else {
                    const err = await r.text();
                    this.showAlert('Unlock failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Unlock failed', 'error'); }
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
        async loadSystemInfo() {
            try {
                const r = await fetch('/api/system', { credentials: 'same-origin' });
                if (!r.ok) { this.showAlert('Failed to load system info', 'error'); return; }
                this.sysInfo = await r.json();
                this.newHostname = this.sysInfo.hostname || this.telemetry.hostname || '';
            } catch (e) { this.showAlert('Failed to load system info', 'error'); }
        },

        async setHostname() {
            if (!this.newHostname.trim()) return;
            try {
                const r = await fetch('/api/system/hostname', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ hostname: this.newHostname.trim() })
                });
                if (r.ok) {
                    this.showAlert('Hostname updated', 'success');
                    this.showHostnameEdit = false;
                    this.loadSystemInfo();
                } else {
                    const err = await r.text();
                    this.showAlert('Hostname update failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Hostname update failed', 'error'); }
        },

        async loadKMod() {
            try {
                const r = await fetch('/api/kmod/list', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.kmodModules = await r.json();
            } catch (e) { /* silent */ }
        },

        async loadModule() {
            if (!this.kmodForm.module.trim()) return;
            try {
                const r = await fetch('/api/kmod/load', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ module: this.kmodForm.module.trim() })
                });
                if (r.ok) {
                    this.showAlert('Module loaded', 'success');
                    this.showKModForm = false;
                    this.kmodForm = { module: '' };
                    this.loadKMod();
                } else {
                    const err = await r.text();
                    this.showAlert('Load failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Load failed', 'error'); }
        },

        async unloadModule(module) {
            if (!confirm(`Unload module ${module}?`)) return;
            try {
                const r = await fetch('/api/kmod/unload', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ module })
                });
                if (r.ok) {
                    this.showAlert('Module unloaded', 'success');
                    this.loadKMod();
                } else {
                    const err = await r.text();
                    this.showAlert('Unload failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Unload failed', 'error'); }
        },

        async loadApkRepos() {
            try {
                const r = await fetch('/api/apk/repositories', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.apkRepos = data.content || '';
            } catch (e) { this.showAlert('Failed to load repositories', 'error'); }
        },

        async saveApkRepos() {
            try {
                const r = await fetch('/api/apk/repositories', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: this.apkRepos })
                });
                if (r.ok) {
                    this.showAlert('Repositories saved', 'success');
                    this.showApkRepoEdit = false;
                } else {
                    const err = await r.text();
                    this.showAlert('Save failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Save failed', 'error'); }
        },

        async loadSshConfig() {
            try {
                const r = await fetch('/api/ssh/config', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.sshConfig = data.content || '';
            } catch (e) { this.showAlert('Failed to load SSH config', 'error'); }
        },

        async saveSshConfig() {
            try {
                const r = await fetch('/api/ssh/config', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: this.sshConfig })
                });
                if (r.ok) {
                    this.showAlert('SSH config saved', 'success');
                    this.showSshConfigEdit = false;
                } else {
                    const err = await r.text();
                    this.showAlert('Save failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Save failed', 'error'); }
        },

        async loadCron() {
            try {
                const r = await fetch('/api/cron', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.cronContent = data.content || '';
            } catch (e) { this.showAlert('Failed to load cron', 'error'); }
        },

        async saveCron() {
            try {
                const r = await fetch('/api/cron', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: this.cronContent })
                });
                if (r.ok) {
                    this.showAlert('Cron saved', 'success');
                    this.showCronEdit = false;
                } else {
                    const err = await r.text();
                    this.showAlert('Save failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Save failed', 'error'); }
        },

        async loadProcesses() {
            try {
                const r = await fetch('/api/processes', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.processes = await r.json();
            } catch (e) { this.showAlert('Failed to load processes', 'error'); }
        },

        filteredProcesses() {
            if (!this.processFilter) return this.processes;
            const f = this.processFilter.toLowerCase();
            return this.processes.filter(p =>
                (p.name && p.name.toLowerCase().includes(f)) ||
                (p.user && p.user.toLowerCase().includes(f)) ||
                (p.command && p.command.toLowerCase().includes(f))
            );
        },

        async killProcess(pid) {
            if (!confirm(`Kill process ${pid}?`)) return;
            try {
                const r = await fetch(`/api/processes/${pid}`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ pid })
                });
                if (r.ok) {
                    this.showAlert('Process killed', 'success');
                    this.loadProcesses();
                } else {
                    const err = await r.text();
                    this.showAlert('Kill failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Kill failed', 'error'); }
        },

        async loadLogHistory() {
            try {
                const params = new URLSearchParams();
                if (this.logHistoryFilter) params.set('filter', this.logHistoryFilter);
                params.set('offset', this.logHistoryOffset);
                params.set('limit', this.logHistoryLimit);
                const r = await fetch('/api/logs/history?' + params.toString(), { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.logHistory = data.lines || [];
                this.logHistoryTotal = data.total || 0;
            } catch (e) { this.showAlert('Failed to load log history', 'error'); }
        },

        async searchLogHistory() {
            this.logHistoryOffset = 0;
            await this.loadLogHistory();
        },

        async nextLogHistory() {
            if (this.logHistoryOffset + this.logHistoryLimit < this.logHistoryTotal) {
                this.logHistoryOffset += this.logHistoryLimit;
                await this.loadLogHistory();
            }
        },

        async prevLogHistory() {
            if (this.logHistoryOffset > 0) {
                this.logHistoryOffset = Math.max(0, this.logHistoryOffset - this.logHistoryLimit);
                await this.loadLogHistory();
            }
        },

        // ── Network Interfaces ────────────────────────────
        async loadNetworkInterfaces() {
            try {
                const r = await fetch('/api/network/interfaces', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.networkInterfaces = await r.json();
            } catch (e) { this.showAlert('Failed to load interface states', 'error'); }
        },

        async netIfUp(iface) {
            try {
                const r = await fetch(`/api/network/${encodeURIComponent(iface)}/up`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' }
                });
                if (r.ok) { this.showAlert(`${iface} brought up`, 'success'); this.loadNetworkInterfaces(); }
                else { const err = await r.text(); this.showAlert(`Up failed: ${err}`, 'error'); }
            } catch (e) { this.showAlert('Up failed', 'error'); }
        },

        async netIfDown(iface) {
            if (!confirm(`Bring down ${iface}? This may disconnect your session.`)) return;
            try {
                const r = await fetch(`/api/network/${encodeURIComponent(iface)}/down`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' }
                });
                if (r.ok) { this.showAlert(`${iface} brought down`, 'success'); this.loadNetworkInterfaces(); }
                else { const err = await r.text(); this.showAlert(`Down failed: ${err}`, 'error'); }
            } catch (e) { this.showAlert('Down failed', 'error'); }
        },

        // ── fstab ─────────────────────────────────────────
        async loadFstab() {
            try {
                const r = await fetch('/api/fstab', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.fstabContent = data.content || '';
            } catch (e) { this.showAlert('Failed to load fstab', 'error'); }
        },

        async saveFstab() {
            try {
                const r = await fetch('/api/fstab', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: this.fstabContent })
                });
                if (r.ok) { this.showAlert('fstab saved', 'success'); this.showFstabEdit = false; }
                else { const err = await r.text(); this.showAlert('Save failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('Save failed', 'error'); }
        },

        // ── Timezone / NTP ────────────────────────────────
        async loadTimezone() {
            try {
                const r = await fetch('/api/system/timezone', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.timezone = data.timezone || '';
            } catch (e) { /* silent */ }
        },

        async saveTimezone() {
            try {
                const r = await fetch('/api/system/timezone', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ timezone: this.timezone })
                });
                if (r.ok) { this.showAlert('Timezone saved', 'success'); this.showTimezoneEdit = false; this.loadTimezone(); }
                else { const err = await r.text(); this.showAlert('Save failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('Save failed', 'error'); }
        },

        async loadNtp() {
            try {
                const r = await fetch('/api/system/ntp', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.ntpEnabled = data.enabled || false;
            } catch (e) { /* silent */ }
        },

        async toggleNtp() {
            const enabled = !this.ntpEnabled;
            try {
                const r = await fetch('/api/system/ntp', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ enabled })
                });
                if (r.ok) { this.showAlert(`NTP ${enabled ? 'enabled' : 'disabled'}`, 'success'); this.loadNtp(); }
                else { const err = await r.text(); this.showAlert('NTP toggle failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('NTP toggle failed', 'error'); }
        },

        // ── Service Log ───────────────────────────────────
        async showServiceLogModal(name) {
            this.serviceLogName = name;
            this.serviceLogContent = '';
            this.serviceLogStatus = '';
            this.showServiceLog = true;
            try {
                const r = await fetch(`/api/services/${encodeURIComponent(name)}/log`, { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.serviceLogContent = data.log || '';
                this.serviceLogStatus = data.status || '';
            } catch (e) { this.showAlert('Failed to load service log', 'error'); }
        },

        closeServiceLog() {
            this.showServiceLog = false;
            this.serviceLogName = '';
            this.serviceLogContent = '';
            this.serviceLogStatus = '';
        },

        // ── User Groups ───────────────────────────────────
        async showGroupManageModal(username) {
            this.groupManageUser = username;
            this.userGroups = [];
            this.newGroupName = '';
            this.showGroupManage = true;
            await this.loadUserGroups(username);
        },

        async loadUserGroups(username) {
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(username)}/groups`, { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.userGroups = data.groups || [];
            } catch (e) { this.showAlert('Failed to load user groups', 'error'); }
        },

        async addUserGroup() {
            const group = this.newGroupName.trim();
            if (!group) return;
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(this.groupManageUser)}/groups`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ group })
                });
                if (r.ok) {
                    this.showAlert('Group added', 'success');
                    this.newGroupName = '';
                    await this.loadUserGroups(this.groupManageUser);
                } else {
                    const err = await r.text();
                    this.showAlert('Add failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Add failed', 'error'); }
        },

        async removeUserGroup(group) {
            if (!confirm(`Remove ${this.groupManageUser} from group ${group}?`)) return;
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(this.groupManageUser)}/groups/${encodeURIComponent(group)}?action=remove`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' }
                });
                if (r.ok) {
                    this.showAlert('Group removed', 'success');
                    await this.loadUserGroups(this.groupManageUser);
                } else {
                    const err = await r.text();
                    this.showAlert('Remove failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Remove failed', 'error'); }
        },

        closeGroupManage() {
            this.showGroupManage = false;
            this.groupManageUser = '';
            this.userGroups = [];
            this.newGroupName = '';
        },

        // ── System Alerts ─────────────────────────────────
        async loadSystemAlerts() {
            this.systemAlertsLoading = true;
            try {
                const r = await fetch('/api/alerts', { credentials: 'same-origin' });
                if (!r.ok) { this.systemAlerts = []; return; }
                this.systemAlerts = await r.json();
            } catch (e) { this.systemAlerts = []; }
            finally { this.systemAlertsLoading = false; }
        },

        // ── Block Devices ─────────────────────────────────
        async loadBlockDevices() {
            try {
                const r = await fetch('/api/storage/devices', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.blockDevices = this.parseLsblk(data);
            } catch (e) { this.showAlert('Failed to load block devices', 'error'); }
        },

        parseLsblk(data) {
            if (!data || !data.blockdevices) return [];
            const result = [];
            const walk = (devices) => {
                for (const d of devices) {
                    result.push({
                        name: d.name || '',
                        size: d.size || '',
                        type: d.type || '',
                        mountpoint: d.mountpoint || '',
                        model: d.model || '',
                    });
                    if (d.children) walk(d.children);
                }
            };
            walk(data.blockdevices);
            return result;
        },

        // ── Shell Change ────────────────────────────────────
        openShellChange(username, currentShell) {
            this.shellChangeUser = username;
            this.newShell = currentShell || '/bin/sh';
            this.showShellChange = true;
        },

        closeShellChange() {
            this.showShellChange = false;
            this.shellChangeUser = '';
            this.newShell = '';
        },

        async saveShell() {
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(this.shellChangeUser)}/shell`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ shell: this.newShell })
                });
                if (r.ok) {
                    this.showAlert('Shell changed', 'success');
                    this.showShellChange = false;
                    this.loadUsers();
                } else {
                    const err = await r.text();
                    this.showAlert('Shell change failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('Shell change failed', 'error'); }
        },

        // ── LBU ───────────────────────────────────────────
        async loadLbu() {
            try {
                const r1 = await fetch('/api/lbu/status', { credentials: 'same-origin' });
                if (r1.ok) {
                    const data = await r1.json();
                    this.lbuStatus = data.output || data.status || '';
                }
                const r2 = await fetch('/api/lbu/list', { credentials: 'same-origin' });
                if (r2.ok) {
                    const data = await r2.json();
                    this.lbuBackups = data.backups || [];
                }
            } catch (e) { /* silent */ }
        },

        async lbuCommit() {
            if (!confirm('Commit current configuration to LBU backup?')) return;
            try {
                const r = await fetch('/api/lbu/commit', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: '{}'
                });
                if (r.ok) { this.showAlert('LBU commit succeeded', 'success'); this.loadLbu(); }
                else { const err = await r.text(); this.showAlert('LBU commit failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('LBU commit failed', 'error'); }
        },

        async lbuRestore(backup) {
            if (!confirm(`Restore LBU backup ${backup}?`)) return;
            try {
                const r = await fetch('/api/lbu/restore', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ backup })
                });
                if (r.ok) { this.showAlert('LBU restore succeeded', 'success'); this.loadLbu(); }
                else { const err = await r.text(); this.showAlert('LBU restore failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('LBU restore failed', 'error'); }
        },

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

        // ── Audit Log ─────────────────────────────────────
        async loadAuditLog() {
            this.auditLogLoading = true;
            try {
                const r = await fetch('/api/audit?limit=500', { credentials: 'same-origin' });
                if (!r.ok) { this.auditLog = []; return; }
                this.auditLog = await r.json();
            } catch (e) { this.auditLog = []; }
            finally { this.auditLogLoading = false; }
        },

        // ── SSH Keys ──────────────────────────────────────
        openSshKeys(username) {
            this.sshKeysUser = username;
            this.sshKeysContent = '';
            this.showSshKeys = true;
            this.loadSshKeys(username);
        },

        closeSshKeys() {
            this.showSshKeys = false;
            this.sshKeysUser = '';
            this.sshKeysContent = '';
        },

        async loadSshKeys(username) {
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(username)}/ssh-keys`, { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.sshKeysContent = data.content || '';
            } catch (e) { /* silent */ }
        },

        async saveSshKeys() {
            try {
                const r = await fetch(`/api/users/${encodeURIComponent(this.sshKeysUser)}/ssh-keys`, {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: this.sshKeysContent })
                });
                if (r.ok) {
                    this.showAlert('SSH keys saved', 'success');
                    this.showSshKeys = false;
                } else {
                    const err = await r.text();
                    this.showAlert('SSH keys save failed: ' + err, 'error');
                }
            } catch (e) { this.showAlert('SSH keys save failed', 'error'); }
        },

        // ── Resolv ────────────────────────────────────────
        async loadResolv() {
            try {
                const r = await fetch('/api/system/resolv', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.resolvContent = data.content || '';
            } catch (e) { /* silent */ }
        },

        async saveResolv() {
            try {
                const r = await fetch('/api/system/resolv', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: this.resolvContent })
                });
                if (r.ok) { this.showAlert('resolv.conf saved', 'success'); this.showResolvEdit = false; }
                else { const err = await r.text(); this.showAlert('resolv.conf save failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('resolv.conf save failed', 'error'); }
        },

        // ── Firewall ──────────────────────────────────────
        async loadFirewall() {
            try {
                const r = await fetch('/api/firewall', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.firewallContent = data.content || '';
            } catch (e) { /* silent */ }
        },

        // ── Routes ────────────────────────────────────────
        async loadRoutes() {
            try {
                const r = await fetch('/api/network/routes', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.routesContent = data.content || '';
            } catch (e) { /* silent */ }
        },

        // ── Clock ─────────────────────────────────────────
        async saveClock() {
            if (!this.clockDatetime) return;
            try {
                const r = await fetch('/api/system/clock', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ datetime: this.clockDatetime })
                });
                if (r.ok) { this.showAlert('System clock updated', 'success'); }
                else { const err = await r.text(); this.showAlert('Clock update failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('Clock update failed', 'error'); }
        },

        // ── WiFi Scan ─────────────────────────────────────
        async loadWifiScan() {
            try {
                const r = await fetch('/api/network/wifi', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.wifiScanContent = data.content || '';
            } catch (e) { /* silent */ }
        },

        // ── CPU Info ──────────────────────────────────────
        async loadCpuInfo() {
            try {
                const r = await fetch('/api/system/cpuinfo', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.cpuInfo = await r.json();
            } catch (e) { this.cpuInfo = {}; }
        },

        // ── Memory Info ─────────────────────────────────
        async loadMemInfo() {
            try {
                const r = await fetch('/api/system/meminfo', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.memInfo = await r.json();
            } catch (e) { this.memInfo = {}; }
        },

        // ── dmesg ─────────────────────────────────────────
        async loadDmesg() {
            try {
                const r = await fetch('/api/system/dmesg', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.dmesgContent = data.content || '';
            } catch (e) { this.dmesgContent = ''; }
        },

        // ── Network Addresses ───────────────────────────
        async loadNetAddr() {
            try {
                const r = await fetch('/api/network/addr', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.netAddrContent = data.content || '';
            } catch (e) { this.netAddrContent = ''; }
        },

        // ── SMART ─────────────────────────────────────────
        async loadSmart() {
            try {
                const r = await fetch(`/api/storage/smart?dev=${encodeURIComponent(this.smartDev)}`, { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.smartContent = data.content || '';
            } catch (e) { this.smartContent = ''; }
        },

        // ── SSH Host Keys ─────────────────────────────────
        async loadSshHostKeys() {
            try {
                const r = await fetch('/api/system/ssh-host-keys', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.sshHostKeys = await r.json();
            } catch (e) { this.sshHostKeys = []; }
        },

        // ── MOTD ──────────────────────────────────────────
        async loadMotd() {
            try {
                const r = await fetch('/api/system/motd', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.motdContent = data.motd || '';
                this.issueContent = data.issue || '';
            } catch (e) { this.motdContent = ''; this.issueContent = ''; }
        },

        // ── Boot Parameters ───────────────────────────────
        async loadCmdline() {
            try {
                const r = await fetch('/api/system/cmdline', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.cmdlineContent = data.cmdline || '';
            } catch (e) { this.cmdlineContent = ''; }
        },

        // ── Swaps ─────────────────────────────────────────
        async loadSwaps() {
            try {
                const r = await fetch('/api/system/swaps', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.swapsContent = data.content || '';
            } catch (e) { this.swapsContent = ''; }
        },

        // ── DHCP Toggle ───────────────────────────────────
        async toggleDhcp(iface, action) {
            try {
                const r = await fetch('/api/network/dhcp', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: JSON.stringify({ iface, action })
                });
                if (r.ok) { this.showAlert(`DHCP ${action}ed on ${iface}`, 'success'); }
                else { const err = await r.text(); this.showAlert(`DHCP toggle failed: ${err}`, 'error'); }
            } catch (e) { this.showAlert('DHCP toggle failed', 'error'); }
        },

        // ── USB Devices ───────────────────────────────────
        async loadUsbDevices() {
            try {
                const r = await fetch('/api/hardware/usb', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.usbDevicesContent = data.content || '';
            } catch (e) { this.usbDevicesContent = ''; }
        },

        // ── PCI Devices ─────────────────────────────────
        async loadPciDevices() {
            try {
                const r = await fetch('/api/hardware/pci', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.pciDevicesContent = data.content || '';
            } catch (e) { this.pciDevicesContent = ''; }
        },

        // ── Network Connections ───────────────────────────
        async loadNetConnections() {
            try {
                const r = await fetch('/api/network/connections', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.netConnectionsContent = data.content || '';
            } catch (e) { this.netConnectionsContent = ''; }
        },

        // ── Environment Variables ───────────────────────────
        async loadEnviron() {
            try {
                const r = await fetch('/api/system/environ', { credentials: 'same-origin' });
                if (!r.ok) return;
                this.environVars = await r.json();
            } catch (e) { this.environVars = {}; }
        },

        // ── Package Depends ─────────────────────────────────
        async loadPackageDepends(name) {
            if (!name) return;
            this.packageDependsName = name;
            try {
                const r = await fetch(`/api/packages/depends?name=${encodeURIComponent(name)}`, { credentials: 'same-origin' });
                if (!r.ok) return;
                this.packageDepends = await r.json();
            } catch (e) { this.packageDepends = {}; }
        },

        // ── ARP Table ─────────────────────────────────────
        async loadArp() {
            try {
                const r = await fetch('/api/network/arp', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.arpContent = data.content || '';
            } catch (e) { this.arpContent = ''; }
        },

        // ── Kernel Version ────────────────────────────────
        async loadKernelVersion() {
            try {
                const r = await fetch('/api/system/version', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.kernelVersionContent = data.content || '';
            } catch (e) { this.kernelVersionContent = ''; }
        },

        // ── Disk Partitions ───────────────────────────────
        async loadPartitions() {
            try {
                const r = await fetch('/api/storage/partitions', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.partitionsContent = data.content || '';
            } catch (e) { this.partitionsContent = ''; }
        },

        // ── Services Status ───────────────────────────────
        async loadServicesStatus() {
            try {
                const r = await fetch('/api/services/status', { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.servicesStatusContent = data.content || '';
            } catch (e) { this.servicesStatusContent = ''; }
        },

        // ── File Viewer ───────────────────────────────────
        async loadFileViewer() {
            if (!this.fileViewerPath) return;
            try {
                const r = await fetch(`/api/files?path=${encodeURIComponent(this.fileViewerPath)}`, { credentials: 'same-origin' });
                if (!r.ok) { this.fileViewerContent = ''; return; }
                const data = await r.json();
                this.fileViewerContent = data.content || '';
            } catch (e) { this.fileViewerContent = ''; }
        },

        // ── Log Search ────────────────────────────────────
        async searchLogs() {
            if (!this.logSearchQuery) return;
            try {
                const r = await fetch(`/api/logs/search?query=${encodeURIComponent(this.logSearchQuery)}&limit=100`, { credentials: 'same-origin' });
                if (!r.ok) return;
                const data = await r.json();
                this.logSearchResults = data.lines || [];
                this.logSearchCount = data.count || 0;
            } catch (e) { this.logSearchResults = []; this.logSearchCount = 0; }
        },

        // ── Package Cache Clean ───────────────────────────
        async cleanPackageCache() {
            try {
                const r = await fetch('/api/packages/cache-clean', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: '{}'
                });
                if (r.ok) { this.showAlert('Package cache cleaned', 'success'); }
                else { const err = await r.text(); this.showAlert('Cache clean failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('Cache clean failed', 'error'); }
        },

        // ── Config Export/Import ──────────────────────────
        async exportConfig() {
            try {
                const r = await fetch('/api/config/export', { credentials: 'same-origin' });
                if (!r.ok) { this.showAlert('Export failed', 'error'); return; }
                const blob = await r.blob();
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = 'config.json';
                a.click();
                URL.revokeObjectURL(url);
            } catch (e) { this.showAlert('Export failed', 'error'); }
        },

        async importConfig(event) {
            const file = event.target.files[0];
            if (!file) return;
            try {
                const text = await file.text();
                const r = await fetch('/api/config/import', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: { 'X-CSRF-Token': this.csrfToken, 'Content-Type': 'application/json' },
                    body: text
                });
                if (r.ok) { this.showAlert('Config imported', 'success'); }
                else { const err = await r.text(); this.showAlert('Import failed: ' + err, 'error'); }
            } catch (e) { this.showAlert('Import failed', 'error'); }
            finally { event.target.value = ''; }
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

        formatUptime(sec) {
            if (sec == null || isNaN(sec) || sec <= 0) return '-';
            const d = Math.floor(sec / 86400);
            const h = Math.floor((sec % 86400) / 3600);
            const m = Math.floor((sec % 3600) / 60);
            const parts = [];
            if (d) parts.push(d + 'd');
            if (h) parts.push(h + 'h');
            if (m || (!d && !h)) parts.push(m + 'm');
            return parts.join(' ');
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

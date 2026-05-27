package ipc

import "encoding/json"

// MaxMessageSize is the hard cap for a single IPC message.
// 8 MiB is large enough for a full Alpine package listing (~10k entries
// with descriptions, version, etc.) while still bounding memory usage.
const MaxMessageSize = 8 * 1024 * 1024

// Protocol version.
const Version = 1

// Envelope is the length-prefixed wire message exchanged over the Unix socket.
type Envelope struct {
	Version     int             `json:"v"`
	RequestID   string          `json:"rid"`
	MessageType string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
}

// ── Request Payloads ───────────────────────────────

type ServiceListReq struct{}

type ServiceStatusReq struct {
	Name string `json:"name"`
}

type ServiceControlReq struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type ServiceEnableReq struct {
	Name string `json:"name"`
}

type ServiceDisableReq struct {
	Name string `json:"name"`
}

type SystemInfoReq struct{}

type PackageListReq struct{}

type PackageSearchReq struct {
	Query string `json:"query"`
}

type PackageInfoReq struct {
	Name string `json:"name"`
}

type PackageInstallReq struct {
	Packages []string `json:"packages"`
}

type PackageRemoveReq struct {
	Packages []string `json:"packages"`
}

type PackageUpdateReq struct{}

type PackageUpgradeReq struct{}

type PackageOpQueryReq struct {
	OpID string `json:"op_id"`
}

type RebootReq struct {
	DelaySec int `json:"delay_sec,omitempty"`
}

type ShutdownReq struct {
	DelaySec int `json:"delay_sec,omitempty"`
}

type UserCreateReq struct {
	Username string `json:"username"`
	Home     string `json:"home,omitempty"`
	Shell    string `json:"shell,omitempty"`
}

type PasswordResetReq struct {
	Username string `json:"username"`
}

type LoginVerifyReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginVerifyResp struct {
	Valid  bool     `json:"valid"`
	Role   string   `json:"role"`
	Groups []string `json:"groups"`
}

type UserDeleteReq struct {
	Username string `json:"username"`
}

type UserLockReq struct {
	Username string `json:"username"`
}

type UserUnlockReq struct {
	Username string `json:"username"`
}

type ApkRepoReadReq struct{}

type ApkRepoWriteReq struct {
	Content string `json:"content"`
}

type SshConfigReadReq struct{}

type SshConfigWriteReq struct {
	Content string `json:"content"`
}

type CronReadReq struct{}

type CronWriteReq struct {
	Content string `json:"content"`
}

type ProcessKillReq struct {
	PID int `json:"pid"`
}

type LogReadReq struct {
	Filter string `json:"filter,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type NetIfReq struct {
	Interface string `json:"interface"`
}

type FstabReadReq struct{}

type FstabWriteReq struct {
	Content string `json:"content"`
}

type TimezoneSetReq struct {
	Timezone string `json:"timezone"`
}

type NtpSetReq struct {
	Enabled bool `json:"enabled"`
}

type ServiceLogReadReq struct {
	Name string `json:"name"`
}

type UserGroupReq struct {
	Username string `json:"username"`
	Group    string `json:"group"`
}

type ShellChangeReq struct {
	Username string `json:"username"`
	Shell    string `json:"shell"`
}

type LbuCommitReq struct{}

type LbuStatusReq struct{}

type LbuListReq struct{}

type LbuRestoreReq struct {
	Backup string `json:"backup"`
}

type MountReq struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
	FSType     string `json:"fstype,omitempty"`
}

type UnmountReq struct {
	Mountpoint string `json:"mountpoint"`
}

type KModLoadReq struct {
	Module string `json:"module"`
}

type KModUnloadReq struct {
	Module string `json:"module"`
}

type HostnameSetReq struct {
	Hostname string `json:"hostname"`
}

type SshKeysReadReq struct {
	Username string `json:"username"`
}

type SshKeysWriteReq struct {
	Username string `json:"username"`
	Content  string `json:"content"`
}

type ResolvReadReq struct{}

type ResolvWriteReq struct {
	Content string `json:"content"`
}

type ClockSetReq struct {
	DateTime string `json:"datetime"`
}

// ── OK Response Payloads ───────────────────────────

type ServiceListData struct {
	Services []ServiceEntry `json:"services"`
}

type ServiceEntry struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Runlevel string `json:"runlevel,omitempty"`
}

type ServiceStatusData struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	PID     int    `json:"pid,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Runlevel string `json:"runlevel,omitempty"`
}

type SystemInfoData struct {
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`
	Kernel     string `json:"kernel"`
	UptimeSec  int64  `json:"uptime_sec"`
	TotalMemKB int64  `json:"total_mem_kb"`
	CpuCount   int    `json:"cpu_count"`
}

type PackageListData struct {
	Packages []PackageInfo `json:"packages"`
}

type PackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
	Installed   bool   `json:"installed,omitempty"`
}

type PackageSearchData struct {
	Packages []PackageInfo `json:"packages"`
}

type PackageOpData struct {
	OpID      string           `json:"op_id"`
	Type      string           `json:"type"`
	State     string           `json:"state"`
	Progress  []ProgressLine   `json:"progress,omitempty"`
	Error     string           `json:"error,omitempty"`
	CreatedAt int64            `json:"created_at"`
	StartedAt *int64           `json:"started_at,omitempty"`
	EndedAt   *int64           `json:"ended_at,omitempty"`
}

type ProgressLine struct {
	Timestamp int64  `json:"ts"`
	Line      string `json:"line"`
}

// ── Error Response ─────────────────────────────────

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error codes.
const (
	ErrInvalidRequest   = "InvalidRequest"
	ErrCapabilityDenied = "CapabilityDenied"
	ErrServiceNotFound  = "ServiceNotFound"
	ErrActionNotAllowed = "ActionNotAllowed"
	ErrExecutionFailed  = "ExecutionFailed"
	ErrInternalError    = "InternalError"
)

// ── Message Types ──────────────────────────────────

const (
	TypeServiceList    = "cap.service.list"
	TypeServiceStatus  = "cap.service.status"
	TypeServiceControl = "cap.service.control"
	TypeServiceEnable  = "cap.service.enable"
	TypeServiceDisable = "cap.service.disable"
	TypeSystemInfo     = "cap.system.info"
	TypePackageList    = "cap.package.list"
	TypePackageSearch  = "cap.package.search"
	TypePackageInfo    = "cap.package.info"
	TypePackageInstall = "cap.package.install"
	TypePackageRemove  = "cap.package.remove"
	TypePackageUpdate  = "cap.package.update"
	TypePackageUpgrade = "cap.package.upgrade"
	TypePackageOpQuery = "cap.package.opquery"
	TypeReboot         = "cap.system.reboot"
	TypeShutdown       = "cap.system.shutdown"
	TypeUserCreate     = "cap.user.create"
	TypePasswordReset  = "cap.user.password"
	TypeLoginVerify    = "cap.auth.login"
	TypeMount          = "cap.fs.mount"
	TypeUnmount        = "cap.fs.unmount"
	TypeKModLoad       = "cap.kmod.load"
	TypeKModUnload     = "cap.kmod.unload"
	TypeHostnameSet    = "cap.system.hostname"
	TypeUserDelete     = "cap.user.delete"
	TypeUserLock       = "cap.user.lock"
	TypeUserUnlock     = "cap.user.unlock"
	TypeApkRepoRead    = "cap.apk.repo.read"
	TypeApkRepoWrite   = "cap.apk.repo.write"
	TypeSshConfigRead  = "cap.ssh.config.read"
	TypeSshConfigWrite = "cap.ssh.config.write"
	TypeCronRead       = "cap.cron.read"
	TypeCronWrite      = "cap.cron.write"
	TypeProcessKill    = "cap.process.kill"
	TypeLogRead        = "cap.log.read"
	TypeNetIfUp        = "cap.net.if.up"
	TypeNetIfDown      = "cap.net.if.down"
	TypeFstabRead      = "cap.fstab.read"
	TypeFstabWrite     = "cap.fstab.write"
	TypeTimezoneSet    = "cap.timezone.set"
	TypeNtpSet         = "cap.ntp.set"
	TypeServiceLogRead = "cap.service.log.read"
	TypeUserGroupAdd    = "cap.user.group.add"
	TypeUserGroupRemove = "cap.user.group.remove"
	TypeShellChange     = "cap.user.shell"
	TypeLbuCommit       = "cap.lbu.commit"
	TypeLbuStatus       = "cap.lbu.status"
	TypeLbuList         = "cap.lbu.list"
	TypeLbuRestore      = "cap.lbu.restore"
	TypeSshKeysRead     = "cap.ssh.keys.read"
	TypeSshKeysWrite    = "cap.ssh.keys.write"
	TypeResolvRead      = "cap.resolv.read"
	TypeResolvWrite     = "cap.resolv.write"
	TypeClockSet         = "cap.clock.set"
	TypePackageCacheClean = "cap.package.cache.clean"
	TypeDhcpToggle        = "cap.network.dhcp.toggle"
	TypeResponseOK        = "response.ok"
	TypeResponseError     = "response.error"
)

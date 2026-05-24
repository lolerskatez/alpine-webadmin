package ipc

import "encoding/json"

// MaxMessageSize is the hard cap for a single IPC message.
const MaxMessageSize = 65536

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
	TypeMount          = "cap.fs.mount"
	TypeUnmount        = "cap.fs.unmount"
	TypeKModLoad       = "cap.kmod.load"
	TypeKModUnload     = "cap.kmod.unload"
	TypeResponseOK     = "response.ok"
	TypeResponseError  = "response.error"
)

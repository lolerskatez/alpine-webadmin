package telemetry

// Snapshot holds a single telemetry sample.
type Snapshot struct {
	Timestamp int64        `json:"ts"`
	CPU       CPUStats     `json:"cpu"`
	Memory    MemStats     `json:"memory"`
	Load      LoadStats    `json:"load"`
	Disks     []DiskStats  `json:"disks,omitempty"`
	Net       []NetStats   `json:"network,omitempty"`
	Mounts    []MountStats `json:"mounts,omitempty"`
	UptimeSec int64        `json:"uptime_sec"`
	TempC     *float64     `json:"temp_c,omitempty"`
	Hostname  string       `json:"hostname"`
}

type CPUStats struct {
	Aggregate float64   `json:"aggregate"`
	PerCore   []float64 `json:"per_core,omitempty"`
}

type MemStats struct {
	TotalKB     int64 `json:"total_kb"`
	AvailableKB int64 `json:"available_kb"`
	UsedKB      int64 `json:"used_kb"`
	SwapTotalKB int64 `json:"swap_total_kb"`
	SwapUsedKB  int64 `json:"swap_used_kb"`
}

type LoadStats struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type DiskStats struct {
	Device    string  `json:"device"`
	ReadIOPS  uint64  `json:"read_iops"`
	WriteIOPS uint64  `json:"write_iops"`
	ReadMBs   float64 `json:"read_mbs"`
	WriteMBs  float64 `json:"write_mbs"`
}

type NetStats struct {
	Interface string `json:"interface"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
	RxErrs    uint64 `json:"rx_errs"`
	TxErrs    uint64 `json:"tx_errs"`
}

type MountStats struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
	TotalKB    uint64 `json:"total_kb"`
	UsedKB     uint64 `json:"used_kb"`
}

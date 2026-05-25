package telemetry

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/proc"
)

// Collector gathers /proc metrics on a ticker and broadcasts to a Hub.
type Collector struct {
	hub        *Hub
	interval   time.Duration
	prevCPU    proc.CPUStat
	prevPerCore []proc.CPUStat
	prevDisks  []proc.DiskStat
	mu         sync.Mutex
	bufPool    sync.Pool
}

// NewCollector creates a telemetry collector.
func NewCollector(hub *Hub, interval time.Duration) *Collector {
	return &Collector{
		hub:      hub,
		interval: interval,
		bufPool: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 0, 4096)
				return &b
			},
		},
	}
}

// Run starts the collection loop. Blocks until stopCh is closed.
func (c *Collector) Run(stopCh <-chan struct{}) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.collect()
		case <-stopCh:
			return
		}
	}
}

func (c *Collector) collect() {
	snap := c.buildSnapshot()

	// Encode once into pooled buffer
	b := c.bufPool.Get().(*[]byte)
	*b = (*b)[:0]
	enc := json.NewEncoder(&sliceWriter{buf: b})
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]interface{}{
		"type":    "telemetry",
		"payload": snap,
	}); err != nil {
		c.bufPool.Put(b)
		return
	}

	msg := make([]byte, len(*b))
	copy(msg, *b)
	c.bufPool.Put(b)

	c.hub.Broadcast(msg)
}

type sliceWriter struct {
	buf *[]byte
}

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

func (c *Collector) buildSnapshot() *Snapshot {
	now := time.Now().Unix()
	var snap Snapshot
	snap.Timestamp = now

	// CPU — resilient: continue with partial data
	if statF, err := os.Open("/proc/stat"); err == nil {
		agg, perCore, err := proc.ParseStat(statF)
		statF.Close()
		if err == nil {
			c.mu.Lock()
			prevAgg := c.prevCPU
			prevPer := c.prevPerCore
			c.prevCPU = agg
			c.prevPerCore = perCore
			c.mu.Unlock()

			snap.CPU.Aggregate = calcCPUPct(prevAgg, agg)
			if n := len(perCore); n > 0 && n == len(prevPer) {
				snap.CPU.PerCore = make([]float64, n)
				for i := 0; i < n; i++ {
					snap.CPU.PerCore[i] = calcCPUPct(prevPer[i], perCore[i])
				}
			}
		}
	}

	// Memory
	if memF, err := os.Open("/proc/meminfo"); err == nil {
		mem, err := proc.ParseMemInfo(memF)
		memF.Close()
		if err == nil {
			snap.Memory.TotalKB = mem.Total
			snap.Memory.AvailableKB = mem.Available
			snap.Memory.UsedKB = mem.Total - mem.Available
			snap.Memory.SwapTotalKB = mem.SwapTotal
			snap.Memory.SwapUsedKB = mem.SwapTotal - mem.SwapFree
		}
	}

	// Load
	if loadF, err := os.Open("/proc/loadavg"); err == nil {
		load, err := proc.ParseLoadAvg(loadF)
		loadF.Close()
		if err == nil {
			snap.Load.Load1 = load.Load1
			snap.Load.Load5 = load.Load5
			snap.Load.Load15 = load.Load15
		}
	}

	// Uptime
	if upF, err := os.Open("/proc/uptime"); err == nil {
		upt, err := proc.ParseUptime(upF)
		upF.Close()
		if err == nil {
			snap.UptimeSec = int64(upt.UptimeSec)
		}
	}

	// Disk I/O
	var disks []proc.DiskStat
	if diskF, err := os.Open("/proc/diskstats"); err == nil {
		disks, _ = proc.ParseDiskStats(diskF)
		diskF.Close()
	}

	// Network
	var netDevs []proc.NetDev
	if netF, err := os.Open("/proc/net/dev"); err == nil {
		netDevs, _ = proc.ParseNetDev(netF)
		netF.Close()
	}

	// Mounts
	var mounts []proc.MountStat
	if mountF, err := os.Open("/proc/mounts"); err == nil {
		mounts, _ = proc.ParseMounts(mountF)
		mountF.Close()
	}

	// Thermal
	snap.TempC, _ = proc.ParseThermal("/sys/class/thermal")

	// Disk deltas — O(N) using map, reuse prevDisks capacity
	intervalSec := c.interval.Seconds()
	if intervalSec > 0 && len(disks) > 0 {
		c.mu.Lock()
		prevByDev := make(map[string]proc.DiskStat, len(c.prevDisks))
		for _, p := range c.prevDisks {
			prevByDev[p.Device] = p
		}
		// Reuse underlying array if capacity allows
		if cap(c.prevDisks) >= len(disks) {
			c.prevDisks = c.prevDisks[:len(disks)]
			copy(c.prevDisks, disks)
		} else {
			c.prevDisks = make([]proc.DiskStat, len(disks))
			copy(c.prevDisks, disks)
		}
		c.mu.Unlock()

		snap.Disks = make([]DiskStats, 0, len(disks))
		for _, d := range disks {
			prev := prevByDev[d.Device]
			snap.Disks = append(snap.Disks, DiskStats{
				Device:    d.Device,
				ReadIOPS:  uint64(float64(d.Reads-prev.Reads) / intervalSec),
				WriteIOPS: uint64(float64(d.Writes-prev.Writes) / intervalSec),
				ReadMBs:   float64(d.ReadSectors-prev.ReadSectors) * 512 / 1024 / 1024 / intervalSec,
				WriteMBs:  float64(d.WriteSectors-prev.WriteSectors) * 512 / 1024 / 1024 / intervalSec,
			})
		}
	}

	// Network
	if len(netDevs) > 0 {
		snap.Net = make([]NetStats, 0, len(netDevs))
		for _, n := range netDevs {
			snap.Net = append(snap.Net, NetStats{
				Interface: n.Interface,
				RxBytes:   n.RxBytes,
				TxBytes:   n.TxBytes,
				RxPackets: n.RxPackets,
				TxPackets: n.TxPackets,
				RxErrs:    n.RxErrs,
				TxErrs:    n.TxErrs,
			})
		}
	}

	// Mounts
	if len(mounts) > 0 {
		snap.Mounts = make([]MountStats, 0, len(mounts))
		for _, m := range mounts {
			snap.Mounts = append(snap.Mounts, MountStats{
				Device:     m.Device,
				Mountpoint: m.Mountpoint,
				TotalKB:    m.TotalKB,
				UsedKB:     m.UsedKB,
			})
		}
	}

	return &snap
}

func calcCPUPct(prev, curr proc.CPUStat) float64 {
	prevTotal := prev.Total()
	currTotal := curr.Total()
	if currTotal <= prevTotal {
		return 0.0
	}
	diffTotal := float64(currTotal - prevTotal)
	diffIdle := float64(curr.Idle - prev.Idle)
	pct := 100.0 * (1.0 - diffIdle/diffTotal)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

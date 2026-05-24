package proc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DiskStat holds per-device I/O counters.
type DiskStat struct {
	Device       string
	Reads        uint64
	ReadSectors  uint64
	ReadMs       uint64
	Writes       uint64
	WriteSectors uint64
	WriteMs      uint64
}

// ParseDiskStats reads /proc/diskstats and returns counters for non-partition block devices.
func ParseDiskStats(r io.Reader) ([]DiskStat, error) {
	var stats []DiskStat
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		device := fields[2]
		// Skip partitions (e.g., sda1) — heuristic: names ending in digits
		if len(device) > 0 && device[len(device)-1] >= '0' && device[len(device)-1] <= '9' {
			continue
		}

		reads, _ := strconv.ParseUint(fields[3], 10, 64)
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		readMs, _ := strconv.ParseUint(fields[6], 10, 64)
		writes, _ := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		writeMs, _ := strconv.ParseUint(fields[10], 10, 64)

		stats = append(stats, DiskStat{
			Device:       device,
			Reads:        reads,
			ReadSectors:  readSectors,
			ReadMs:       readMs,
			Writes:       writes,
			WriteSectors: writeSectors,
			WriteMs:      writeMs,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("proc/diskstats: scan: %w", err)
	}
	return stats, nil
}

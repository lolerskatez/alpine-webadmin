package proc

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"syscall"
)

// MountStat holds filesystem usage for a mount point.
type MountStat struct {
	Device     string
	Mountpoint string
	FSType     string
	TotalKB    uint64
	FreeKB     uint64
	UsedKB     uint64
}

// ParseMounts reads /proc/mounts and returns usage for real filesystems.
func ParseMounts(r io.Reader) ([]MountStat, error) {
	var stats []MountStat
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountpoint := fields[1]
		fsType := fields[2]

		// Skip virtual filesystems
		switch fsType {
		case "proc", "sysfs", "tmpfs", "devtmpfs", "devpts", "cgroup", "cgroup2",
			"overlay", "squashfs", "autofs", "debugfs", "tracefs", "fusectl",
			"securityfs", "pstore", "bpf", "configfs", "fuse":
			continue
		}

		var buf syscall.Statfs_t
		if err := syscall.Statfs(mountpoint, &buf); err != nil {
			continue
		}
		bs := uint64(buf.Bsize)
		total := buf.Blocks * bs / 1024
		free := buf.Bfree * bs / 1024
		avail := buf.Bavail * bs / 1024
		used := total - avail

		stats = append(stats, MountStat{
			Device:     device,
			Mountpoint: mountpoint,
			FSType:     fsType,
			TotalKB:    total,
			FreeKB:     free,
			UsedKB:     used,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("proc/mounts: scan: %w", err)
	}
	return stats, nil
}

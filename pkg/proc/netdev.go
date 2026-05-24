package proc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// NetDev holds per-interface network counters.
type NetDev struct {
	Interface string
	RxBytes   uint64
	RxPackets uint64
	RxErrs    uint64
	RxDrop    uint64
	TxBytes   uint64
	TxPackets uint64
	TxErrs    uint64
	TxDrop    uint64
}

// ParseNetDev reads /proc/net/dev.
func ParseNetDev(r io.Reader) ([]NetDev, error) {
	var devices []NetDev
	scanner := bufio.NewScanner(r)
	// Skip header lines
	for i := 0; i < 2 && scanner.Scan(); i++ {
	}
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		parse := func(idx int) uint64 {
			v, _ := strconv.ParseUint(fields[idx], 10, 64)
			return v
		}
		devices = append(devices, NetDev{
			Interface: iface,
			RxBytes:   parse(0),
			RxPackets: parse(1),
			RxErrs:    parse(2),
			RxDrop:    parse(3),
			TxBytes:   parse(8),
			TxPackets: parse(9),
			TxErrs:    parse(10),
			TxDrop:    parse(11),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("proc/net/dev: scan: %w", err)
	}
	return devices, nil
}

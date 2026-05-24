package proc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// MemInfo holds parsed /proc/meminfo values in KB.
type MemInfo struct {
	Total     int64
	Free      int64
	Available int64
	Buffers   int64
	Cached    int64
	SwapTotal int64
	SwapFree  int64
}

// ParseMemInfo reads /proc/meminfo and extracts key fields.
func ParseMemInfo(r io.Reader) (MemInfo, error) {
	var m MemInfo
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			m.Total = val
		case "MemFree":
			m.Free = val
		case "MemAvailable":
			m.Available = val
		case "Buffers":
			m.Buffers = val
		case "Cached":
			m.Cached = val
		case "SwapTotal":
			m.SwapTotal = val
		case "SwapFree":
			m.SwapFree = val
		}
	}
	if err := scanner.Err(); err != nil {
		return MemInfo{}, fmt.Errorf("proc/meminfo: scan: %w", err)
	}
	return m, nil
}

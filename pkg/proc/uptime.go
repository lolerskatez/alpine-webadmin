package proc

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Uptime holds parsed /proc/uptime values.
type Uptime struct {
	UptimeSec float64
	IdleSec   float64
}

// ParseUptime reads /proc/uptime.
func ParseUptime(r io.Reader) (Uptime, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Uptime{}, fmt.Errorf("proc/uptime: read: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return Uptime{}, fmt.Errorf("proc/uptime: too few fields")
	}
	up, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return Uptime{}, fmt.Errorf("proc/uptime: parse uptime: %w", err)
	}
	idle, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return Uptime{}, fmt.Errorf("proc/uptime: parse idle: %w", err)
	}
	return Uptime{UptimeSec: up, IdleSec: idle}, nil
}

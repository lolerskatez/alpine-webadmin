package proc

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// LoadAvg holds parsed /proc/loadavg values.
type LoadAvg struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

// ParseLoadAvg reads /proc/loadavg.
func ParseLoadAvg(r io.Reader) (LoadAvg, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("proc/loadavg: read: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return LoadAvg{}, fmt.Errorf("proc/loadavg: too few fields")
	}
	l1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("proc/loadavg: parse load1: %w", err)
	}
	l5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("proc/loadavg: parse load5: %w", err)
	}
	l15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return LoadAvg{}, fmt.Errorf("proc/loadavg: parse load15: %w", err)
	}
	return LoadAvg{Load1: l1, Load5: l5, Load15: l15}, nil
}

package proc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CPUStat holds parsed /proc/stat CPU line values.
type CPUStat struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
	Guest   uint64
}

// Total returns the sum of all jiffies fields.
func (c CPUStat) Total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal + c.Guest
}

// ParseStat reads /proc/stat and returns aggregate + per-core CPU stats.
func ParseStat(r io.Reader) (aggregate CPUStat, perCore []CPUStat, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			aggregate, err = parseCPULine(line)
			if err != nil {
				return CPUStat{}, nil, fmt.Errorf("proc/stat: parse aggregate: %w", err)
			}
		} else if strings.HasPrefix(line, "cpu") {
			core, err := parseCPULine(line)
			if err != nil {
				return CPUStat{}, nil, fmt.Errorf("proc/stat: parse core: %w", err)
			}
			perCore = append(perCore, core)
		}
	}
	if err := scanner.Err(); err != nil {
		return CPUStat{}, nil, fmt.Errorf("proc/stat: scan: %w", err)
	}
	return aggregate, perCore, nil
}

func parseCPULine(line string) (CPUStat, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return CPUStat{}, fmt.Errorf("too few fields")
	}
	var c CPUStat
	var err error
	parse := func(idx int, dst *uint64) {
		if err != nil || idx >= len(fields) {
			return
		}
		*dst, err = strconv.ParseUint(fields[idx], 10, 64)
	}
	parse(1, &c.User)
	parse(2, &c.Nice)
	parse(3, &c.System)
	parse(4, &c.Idle)
	parse(5, &c.IOWait)
	parse(6, &c.IRQ)
	parse(7, &c.SoftIRQ)
	parse(8, &c.Steal)
	parse(9, &c.Guest)
	if err != nil {
		return CPUStat{}, err
	}
	return c, nil
}

package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseThermal reads thermal zone temperatures from /sys/class/thermal.
// Returns the first available temperature in degrees C, or nil if none found.
func ParseThermal(base string) (*float64, error) {
	zones, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("proc/thermal: read dir: %w", err)
	}
	for _, zone := range zones {
		if !strings.HasPrefix(zone.Name(), "thermal_zone") {
			continue
		}
		path := filepath.Join(base, zone.Name(), "temp")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		temp := val / 1000.0
		return &temp, nil
	}
	return nil, nil // no thermal data available
}

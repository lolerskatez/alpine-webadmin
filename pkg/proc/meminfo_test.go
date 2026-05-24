package proc

import (
	"strings"
	"testing"
)

const fixtureMemInfo = `MemTotal:       2048000 kB
MemFree:         512000 kB
MemAvailable:   1536000 kB
Buffers:          64000 kB
Cached:          256000 kB
SwapTotal:       524288 kB
SwapFree:        524288 kB
Active:          512000 kB
Inactive:        128000 kB
`

func TestParseMemInfo(t *testing.T) {
	m, err := ParseMemInfo(strings.NewReader(fixtureMemInfo))
	if err != nil {
		t.Fatalf("ParseMemInfo failed: %v", err)
	}

	if m.Total != 2048000 {
		t.Errorf("Total = %d, want 2048000", m.Total)
	}
	if m.Available != 1536000 {
		t.Errorf("Available = %d, want 1536000", m.Available)
	}
	if m.SwapTotal != 524288 {
		t.Errorf("SwapTotal = %d, want 524288", m.SwapTotal)
	}
	if m.SwapFree != 524288 {
		t.Errorf("SwapFree = %d, want 524288", m.SwapFree)
	}
}

func TestParseMemInfoEmpty(t *testing.T) {
	m, err := ParseMemInfo(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseMemInfo empty failed: %v", err)
	}
	if m.Total != 0 {
		t.Errorf("expected zero values for empty input, got %+v", m)
	}
}

func BenchmarkParseMemInfo(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := ParseMemInfo(strings.NewReader(fixtureMemInfo))
		if err != nil {
			b.Fatal(err)
		}
	}
}

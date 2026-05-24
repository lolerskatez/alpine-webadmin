package proc

import (
	"strings"
	"testing"
)

const fixtureStat = `cpu  1234567 23456 345678 45678901 5678 6789 7890 890 123
cpu0 123456 2345 34567 4567890 567 678 789 89 12
cpu1 111111 21111 311111 4111111 5111 6111 7111 801 111
intr 12345678 1 2 3 4 5
ctxt 9876543210
btime 1609459200
processes 12345
procs_running 2
procs_blocked 1
`

func TestParseStat(t *testing.T) {
	agg, cores, err := ParseStat(strings.NewReader(fixtureStat))
	if err != nil {
		t.Fatalf("ParseStat failed: %v", err)
	}

	wantAgg := CPUStat{User: 1234567, Nice: 23456, System: 345678, Idle: 45678901, IOWait: 5678, IRQ: 6789, SoftIRQ: 7890, Steal: 890, Guest: 123}
	if agg != wantAgg {
		t.Errorf("aggregate = %+v, want %+v", agg, wantAgg)
	}

	if len(cores) != 2 {
		t.Fatalf("expected 2 cores, got %d", len(cores))
	}
	want0 := CPUStat{User: 123456, Nice: 2345, System: 34567, Idle: 4567890, IOWait: 567, IRQ: 678, SoftIRQ: 789, Steal: 89, Guest: 12}
	if cores[0] != want0 {
		t.Errorf("core[0] = %+v, want %+v", cores[0], want0)
	}
}

func TestCPUStatTotal(t *testing.T) {
	c := CPUStat{User: 1, Nice: 2, System: 3, Idle: 4, IOWait: 5, IRQ: 6, SoftIRQ: 7, Steal: 8, Guest: 9}
	if got := c.Total(); got != 45 {
		t.Errorf("Total() = %d, want 45", got)
	}
}

func TestParseStatMissingAggregate(t *testing.T) {
	_, _, err := ParseStat(strings.NewReader("cpu0 1 2 3 4\n"))
	if err == nil {
		t.Fatal("expected error for missing aggregate cpu line")
	}
}

func BenchmarkParseStat(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _, err := ParseStat(strings.NewReader(fixtureStat))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseCPULine(b *testing.B) {
	line := "cpu  1234567 23456 345678 45678901 5678 6789 7890 890 123"
	for i := 0; i < b.N; i++ {
		_, err := parseCPULine(line)
		if err != nil {
			b.Fatal(err)
		}
	}
}

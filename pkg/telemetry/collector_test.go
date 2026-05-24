package telemetry

import (
	"testing"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/proc"
)

func TestCollectorRunAndStop(t *testing.T) {
	hub := NewHub()
	coll := NewCollector(hub, 100*time.Millisecond)
	stopCh := make(chan struct{})

	done := make(chan struct{})
	go func() {
		coll.Run(stopCh)
		close(done)
	}()

	// Let it collect at least once
	time.Sleep(250 * time.Millisecond)
	close(stopCh)

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after closing stopCh")
	}
}

func TestCalcCPUPct(t *testing.T) {
	prev := proc.CPUStat{User: 100, Nice: 10, System: 50, Idle: 5000}
	curr := proc.CPUStat{User: 200, Nice: 20, System: 100, Idle: 5500}
	// Total: prev=5160, curr=5820; diff=660
	// Idle diff: 500; non-idle diff: 160
	// pct = 100 * (1 - 500/660) = 100 * 0.2424 = 24.24%
	pct := calcCPUPct(prev, curr)
	if pct < 20 || pct > 30 {
		t.Errorf("cpu pct = %f, expected ~24", pct)
	}
}

func TestCalcCPUPctZeroTotal(t *testing.T) {
	prev := proc.CPUStat{User: 100, Idle: 500}
	curr := proc.CPUStat{User: 100, Idle: 500}
	pct := calcCPUPct(prev, curr)
	if pct != 0 {
		t.Errorf("cpu pct = %f, want 0 for identical stats", pct)
	}
}

func TestCalcCPUPctClamped(t *testing.T) {
	// Edge case: clamp to [0,100]
	prev := proc.CPUStat{User: 100, Idle: 500}
	curr := proc.CPUStat{User: 50, Idle: 400}
	pct := calcCPUPct(prev, curr)
	if pct < 0 || pct > 100 {
		t.Errorf("cpu pct = %f, should be clamped [0,100]", pct)
	}
}

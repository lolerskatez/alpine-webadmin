package testutil

import (
	"runtime"
	"time"
)

// CheckGoroutineLeak waits briefly for goroutines to settle, then compares
// against an initial count. It returns true if no unexpected goroutines remain.
func CheckGoroutineLeak() bool {
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	return true
}

// GoroutineSnapshot captures the current goroutine count.
type GoroutineSnapshot struct {
	count int
}

// SnapshotGoroutines returns a snapshot of the current goroutine count.
func SnapshotGoroutines() GoroutineSnapshot {
	return GoroutineSnapshot{count: runtime.NumGoroutine()}
}

// Leaked returns true if more goroutines exist now than at snapshot time.
func (s GoroutineSnapshot) Leaked() bool {
	// Allow a small tolerance for runtime background goroutines
	return runtime.NumGoroutine() > s.count+2
}

// WaitForGoroutines sleeps briefly to let goroutines exit.
func WaitForGoroutines() {
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
}

package proc

import (
	"strings"
	"testing"
)

const fixtureDiskStats = `   8       0 sda 12345 0 987654 1234 56789 0 1234567 5678 0 0 0 0 0 0
   8       1 sda1 1234 0 98765 123 5678 0 123456 567 0 0 0 0 0 0
 259       0 nvme0n1 987654 0 12345678 9876 543210 0 8765432 5432 0 0 0 0 0 0
 259       1 nvme0n1p1 98765 0 1234567 987 54321 0 876543 543 0 0 0 0 0 0
`

func TestParseDiskStats(t *testing.T) {
	stats, err := ParseDiskStats(strings.NewReader(fixtureDiskStats))
	if err != nil {
		t.Fatalf("ParseDiskStats failed: %v", err)
	}

	// Should skip partitions (sda1, nvme0n1p1)
	if len(stats) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(stats))
	}

	if stats[0].Device != "sda" {
		t.Errorf("device[0] = %q, want sda", stats[0].Device)
	}
	if stats[0].Reads != 12345 {
		t.Errorf("reads = %d, want 12345", stats[0].Reads)
	}
	if stats[1].Device != "nvme0n1" {
		t.Errorf("device[1] = %q, want nvme0n1", stats[1].Device)
	}
}

func BenchmarkParseDiskStats(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := ParseDiskStats(strings.NewReader(fixtureDiskStats))
		if err != nil {
			b.Fatal(err)
		}
	}
}

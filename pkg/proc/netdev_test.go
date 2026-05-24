package proc

import (
	"strings"
	"testing"
)

const fixtureNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567    1234    0    0    0     0          0         0  1234567    1234    0    0    0     0       0          0
  eth0: 98765432  567890  12   3    0     0          0         0  87654321  456789   5   1    0     0       0          0
wlan0:       0       0    0    0    0     0          0         0        0       0    0    0    0     0       0          0
`

func TestParseNetDev(t *testing.T) {
	devs, err := ParseNetDev(strings.NewReader(fixtureNetDev))
	if err != nil {
		t.Fatalf("ParseNetDev failed: %v", err)
	}

	if len(devs) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devs))
	}

	lo := devs[0]
	if lo.Interface != "lo" {
		t.Errorf("interface = %q, want lo", lo.Interface)
	}
	if lo.RxBytes != 1234567 {
		t.Errorf("lo rx_bytes = %d, want 1234567", lo.RxBytes)
	}
	if lo.TxBytes != 1234567 {
		t.Errorf("lo tx_bytes = %d, want 1234567", lo.TxBytes)
	}

	eth0 := devs[1]
	if eth0.Interface != "eth0" {
		t.Errorf("interface = %q, want eth0", eth0.Interface)
	}
	if eth0.RxErrs != 12 {
		t.Errorf("eth0 rx_errs = %d, want 12", eth0.RxErrs)
	}
	if eth0.TxDrop != 1 {
		t.Errorf("eth0 tx_drop = %d, want 1", eth0.TxDrop)
	}
}

func BenchmarkParseNetDev(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := ParseNetDev(strings.NewReader(fixtureNetDev))
		if err != nil {
			b.Fatal(err)
		}
	}
}

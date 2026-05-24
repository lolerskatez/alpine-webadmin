package proc

import (
	"strings"
	"testing"
)

func TestParseUptime(t *testing.T) {
	u, err := ParseUptime(strings.NewReader("12345.67 89012.34\n"))
	if err != nil {
		t.Fatalf("ParseUptime failed: %v", err)
	}
	if u.UptimeSec != 12345.67 {
		t.Errorf("UptimeSec = %f, want 12345.67", u.UptimeSec)
	}
	if u.IdleSec != 89012.34 {
		t.Errorf("IdleSec = %f, want 89012.34", u.IdleSec)
	}
}

func TestParseUptimeTooFew(t *testing.T) {
	_, err := ParseUptime(strings.NewReader("12345.67\n"))
	if err == nil {
		t.Fatal("expected error for too few fields")
	}
}

func BenchmarkParseUptime(b *testing.B) {
	fixture := "12345.67 89012.34\n"
	for i := 0; i < b.N; i++ {
		_, err := ParseUptime(strings.NewReader(fixture))
		if err != nil {
			b.Fatal(err)
		}
	}
}

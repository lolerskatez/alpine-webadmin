package proc

import (
	"strings"
	"testing"
)

func TestParseLoadAvg(t *testing.T) {
	fixture := "0.52 0.31 0.18 2/1234 56789\n"
	l, err := ParseLoadAvg(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("ParseLoadAvg failed: %v", err)
	}

	if l.Load1 != 0.52 {
		t.Errorf("Load1 = %f, want 0.52", l.Load1)
	}
	if l.Load5 != 0.31 {
		t.Errorf("Load5 = %f, want 0.31", l.Load5)
	}
	if l.Load15 != 0.18 {
		t.Errorf("Load15 = %f, want 0.18", l.Load15)
	}
}

func TestParseLoadAvgTooFew(t *testing.T) {
	_, err := ParseLoadAvg(strings.NewReader("0.52\n"))
	if err == nil {
		t.Fatal("expected error for too few fields")
	}
}

func BenchmarkParseLoadAvg(b *testing.B) {
	fixture := "0.52 0.31 0.18 2/1234 56789\n"
	for i := 0; i < b.N; i++ {
		_, err := ParseLoadAvg(strings.NewReader(fixture))
		if err != nil {
			b.Fatal(err)
		}
	}
}

package openrc

import (
	"testing"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

func TestParseServiceList(t *testing.T) {
	m := NewManager(nil)
	fixture := `
		crond        started
		sshd         started
		nginx        stopped
		mysql        crashed
		webadmin     started   default
		roothelper   started   default
	`

	svcs := m.parseServiceList(fixture)
	if len(svcs) != 6 {
		t.Fatalf("expected 6 services, got %d", len(svcs))
	}

	if svcs[0].Name != "crond" || svcs[0].State != StateStarted {
		t.Errorf("crond: got %s/%s", svcs[0].Name, svcs[0].State)
	}
	if svcs[2].Name != "nginx" || svcs[2].State != StateStopped {
		t.Errorf("nginx: got %s/%s", svcs[2].Name, svcs[2].State)
	}
	if svcs[3].Name != "mysql" || svcs[3].State != StateCrashed {
		t.Errorf("mysql: got %s/%s", svcs[3].Name, svcs[3].State)
	}
	if svcs[4].Name != "webadmin" || svcs[4].Runlevel != "default" {
		t.Errorf("webadmin: got runlevel %q", svcs[4].Runlevel)
	}
}

func TestParseState(t *testing.T) {
	tests := []struct {
		input string
		want  ServiceState
	}{
		{"started", StateStarted},
		{"stopped", StateStopped},
		{"crashed", StateCrashed},
		{"starting", StateStarting},
		{"stopping", StateStopping},
		{"inboot", StateInBoot},
		{"unknown", StateUnknown},
		{"garbage", StateUnknown},
	}
	for _, tt := range tests {
		if got := parseState(tt.input); got != tt.want {
			t.Errorf("parseState(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateServiceName(t *testing.T) {
	if err := validateServiceName(""); err == nil {
		t.Error("empty name should be rejected")
	}
	if err := validateServiceName("nginx; rm -rf /"); err == nil {
		t.Error("injected name should be rejected")
	}
	if err := validateServiceName("nginx"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
}

func TestParseDependencies(t *testing.T) {
	m := NewManager(nil)
	script := `#!/sbin/openrc-run
command="/usr/sbin/nginx"

depend() {
	need net
	use logger dns
	after firewall
	before apache
	provide httpd
}
`
	deps := m.parseDependencies(script)
	want := []Dependency{
		{Name: "net", Type: "need"},
		{Name: "logger", Type: "use"},
		{Name: "dns", Type: "use"},
		{Name: "firewall", Type: "after"},
		{Name: "apache", Type: "before"},
		{Name: "httpd", Type: "provide"},
	}
	if len(deps) != len(want) {
		t.Fatalf("expected %d deps, got %d", len(want), len(deps))
	}
	for i, d := range deps {
		if d.Name != want[i].Name || d.Type != want[i].Type {
			t.Errorf("dep[%d] = %+v, want %+v", i, d, want[i])
		}
	}
}

func TestAudit(t *testing.T) {
	m := NewManager(log.New(log.Error))
	// ensure no panic
	m.audit("test", "nginx")
}

func BenchmarkParseServiceList(b *testing.B) {
	m := NewManager(nil)
	fixture := `
		crond        started
		sshd         started
		nginx        stopped
		mysql        crashed
		webadmin     started   default
		roothelper   started   default
	`
	for i := 0; i < b.N; i++ {
		m.parseServiceList(fixture)
	}
}

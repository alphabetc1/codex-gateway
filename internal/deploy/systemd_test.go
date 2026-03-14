package deploy

import (
	"strings"
	"testing"
)

func TestNormalizeServiceScope(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default", input: "", want: ServiceScopeUser},
		{name: "user", input: "user", want: ServiceScopeUser},
		{name: "system", input: "system", want: ServiceScopeSystem},
		{name: "invalid", input: "weird", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeServiceScope(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeServiceScope(%q) error = nil, want error", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeServiceScope(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeServiceScope(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestSystemdInstallArgsRespectScope(t *testing.T) {
	userInstall, err := newSystemdInstall(ServiceScopeUser, "codex-gateway")
	if err != nil {
		t.Fatalf("newSystemdInstall(user) error = %v", err)
	}
	if got := strings.Join(userInstall.systemctlArgs("daemon-reload"), " "); got != "--user daemon-reload" {
		t.Fatalf("user systemctl args = %q", got)
	}
	if userInstall.wantedBy() != "default.target" {
		t.Fatalf("user wantedBy = %q, want %q", userInstall.wantedBy(), "default.target")
	}

	systemInstall, err := newSystemdInstall(ServiceScopeSystem, "codex-gateway")
	if err != nil {
		t.Fatalf("newSystemdInstall(system) error = %v", err)
	}
	if got := strings.Join(systemInstall.systemctlArgs("daemon-reload"), " "); got != "daemon-reload" {
		t.Fatalf("system systemctl args = %q", got)
	}
	if systemInstall.wantedBy() != "multi-user.target" {
		t.Fatalf("system wantedBy = %q, want %q", systemInstall.wantedBy(), "multi-user.target")
	}
	if !strings.HasPrefix(systemInstall.servicePath, "/etc/systemd/system/") {
		t.Fatalf("system service path = %q", systemInstall.servicePath)
	}
}

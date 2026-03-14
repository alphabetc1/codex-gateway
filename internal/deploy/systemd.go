package deploy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	ServiceScopeUser   = "user"
	ServiceScopeSystem = "system"
)

type systemdInstall struct {
	scope      string
	servicePath string
}

func normalizeServiceScope(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ServiceScopeUser:
		return ServiceScopeUser, nil
	case ServiceScopeSystem:
		return ServiceScopeSystem, nil
	default:
		return "", fmt.Errorf("unsupported service_scope %q; expected %q or %q", raw, ServiceScopeUser, ServiceScopeSystem)
	}
}

func newSystemdInstall(scope, name string) (systemdInstall, error) {
	scope, err := normalizeServiceScope(scope)
	if err != nil {
		return systemdInstall{}, err
	}

	servicePath, err := servicePathForScope(scope, name)
	if err != nil {
		return systemdInstall{}, err
	}

	return systemdInstall{
		scope:      scope,
		servicePath: servicePath,
	}, nil
}

func (i systemdInstall) wantedBy() string {
	if i.scope == ServiceScopeSystem {
		return "multi-user.target"
	}
	return "default.target"
}

func (i systemdInstall) systemctlArgs(args ...string) []string {
	if i.scope == ServiceScopeUser {
		return append([]string{"--user"}, args...)
	}
	return append([]string{}, args...)
}

func (i systemdInstall) preflight(writeOnly bool) error {
	if i.scope == ServiceScopeSystem && os.Geteuid() != 0 {
		return fmt.Errorf("service_scope %q requires root because the unit is written to %s", ServiceScopeSystem, i.servicePath)
	}
	if writeOnly {
		return nil
	}

	if err := requireCommand("systemctl"); err != nil {
		return err
	}

	if i.scope == ServiceScopeUser {
		if err := ensureUserSystemdManager(); err != nil {
			return err
		}
	}

	return nil
}

func servicePathForScope(scope, name string) (string, error) {
	switch scope {
	case ServiceScopeUser:
		return userServicePath(name)
	case ServiceScopeSystem:
		return filepath.Join("/etc/systemd/system", name+".service"), nil
	default:
		return "", fmt.Errorf("unsupported service scope %q", scope)
	}
}

func userServicePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", name+".service"), nil
}

func requireCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return nil
}

func ensureUserSystemdManager() error {
	cmd := exec.Command("systemctl", "--user", "show-environment")
	if out, err := cmd.CombinedOutput(); err != nil {
		details := strings.TrimSpace(string(out))
		if details == "" {
			details = err.Error()
		}
		return fmt.Errorf("systemd user manager is unavailable: %s; start a user session, run `loginctl enable-linger %s`, or set service_scope: %s and rerun as root", details, currentUsernameHint(), ServiceScopeSystem)
	}
	return nil
}

func currentUsernameHint() string {
	for _, key := range []string{"SUDO_USER", "USER", "LOGNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "<user>"
}

func ensureGoToolchain() error {
	if err := requireCommand("go"); err != nil {
		return errors.New("go toolchain not found in PATH; install Go or rerun with --write-only")
	}
	return nil
}

package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type VPSInstallResult struct {
	EnvPath     string
	UsersPath   string
	BinaryPath  string
	ServicePath string
}

type ClientInstallResult struct {
	EnvPath     string
	WrapperPath string
	ServicePath string
}

func InstallVPS(spec VPSConfig) (VPSInstallResult, error) {
	envPath := filepath.Join(spec.ProjectRoot, ".env")
	usersPath := filepath.Join(spec.ProjectRoot, "config", "users.txt")
	serviceInstall, err := newSystemdInstall(spec.ServiceScope, spec.ServiceName)
	if err != nil {
		return VPSInstallResult{}, err
	}
	if err := serviceInstall.preflight(spec.WriteOnly); err != nil {
		return VPSInstallResult{}, err
	}

	envContent, err := RenderVPSEnv(spec)
	if err != nil {
		return VPSInstallResult{}, err
	}
	usersContent, err := RenderVPSUsers(spec)
	if err != nil {
		return VPSInstallResult{}, err
	}
	serviceContent := RenderVPSService(spec)

	if err := os.MkdirAll(filepath.Dir(usersPath), 0o755); err != nil {
		return VPSInstallResult{}, fmt.Errorf("mkdir users dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(spec.BinaryOutput), 0o755); err != nil {
		return VPSInstallResult{}, fmt.Errorf("mkdir binary dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(serviceInstall.servicePath), 0o755); err != nil {
		return VPSInstallResult{}, fmt.Errorf("mkdir service dir: %w", err)
	}

	if err := os.WriteFile(envPath, envContent, 0o600); err != nil {
		return VPSInstallResult{}, fmt.Errorf("write env: %w", err)
	}
	if err := os.WriteFile(usersPath, usersContent, 0o600); err != nil {
		return VPSInstallResult{}, fmt.Errorf("write users: %w", err)
	}
	if err := os.WriteFile(serviceInstall.servicePath, serviceContent, 0o644); err != nil {
		return VPSInstallResult{}, fmt.Errorf("write service: %w", err)
	}

	if !spec.WriteOnly {
		if err := ensureGoToolchain(); err != nil {
			return VPSInstallResult{}, err
		}
		if err := buildBinary(spec.ProjectRoot, spec.BinaryOutput); err != nil {
			return VPSInstallResult{}, err
		}
		if err := runCommand("systemctl", serviceInstall.systemctlArgs("daemon-reload")...); err != nil {
			return VPSInstallResult{}, err
		}
		if err := runCommand("systemctl", serviceInstall.systemctlArgs("enable", "--now", spec.ServiceName+".service")...); err != nil {
			return VPSInstallResult{}, err
		}
	}

	return VPSInstallResult{
		EnvPath:     envPath,
		UsersPath:   usersPath,
		BinaryPath:  spec.BinaryOutput,
		ServicePath: serviceInstall.servicePath,
	}, nil
}

func InstallClient(spec ClientConfig) (ClientInstallResult, error) {
	envPath := filepath.Join(spec.InstallDir, "proxy.env")
	wrapperPath, err := clientWrapperPath(spec.WrapperName)
	if err != nil {
		return ClientInstallResult{}, err
	}
	serviceInstall, err := newSystemdInstall(spec.ServiceScope, spec.ServiceName)
	if err != nil {
		return ClientInstallResult{}, err
	}
	if err := serviceInstall.preflight(spec.WriteOnly); err != nil {
		return ClientInstallResult{}, err
	}

	if err := os.MkdirAll(spec.InstallDir, 0o755); err != nil {
		return ClientInstallResult{}, fmt.Errorf("mkdir install dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
		return ClientInstallResult{}, fmt.Errorf("mkdir wrapper dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(serviceInstall.servicePath), 0o755); err != nil {
		return ClientInstallResult{}, fmt.Errorf("mkdir service dir: %w", err)
	}

	if err := os.WriteFile(envPath, RenderClientEnv(spec), 0o600); err != nil {
		return ClientInstallResult{}, fmt.Errorf("write client env: %w", err)
	}
	if err := os.WriteFile(wrapperPath, RenderClientWrapper(spec, envPath), 0o755); err != nil {
		return ClientInstallResult{}, fmt.Errorf("write wrapper: %w", err)
	}
	if err := os.WriteFile(serviceInstall.servicePath, RenderClientService(spec), 0o644); err != nil {
		return ClientInstallResult{}, fmt.Errorf("write service: %w", err)
	}

	if !spec.WriteOnly {
		if err := runCommand("systemctl", serviceInstall.systemctlArgs("daemon-reload")...); err != nil {
			return ClientInstallResult{}, err
		}
		if err := runCommand("systemctl", serviceInstall.systemctlArgs("enable", "--now", spec.ServiceName+".service")...); err != nil {
			return ClientInstallResult{}, err
		}
	}

	return ClientInstallResult{
		EnvPath:     envPath,
		WrapperPath: wrapperPath,
		ServicePath: serviceInstall.servicePath,
	}, nil
}

func buildBinary(projectRoot, output string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-o", output, "./cmd/codex-gateway")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build binary: %w: %s", err, string(out))
	}
	return nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", name, args, err, string(out))
	}
	return nil
}

func clientWrapperPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".local", "bin", name), nil
}

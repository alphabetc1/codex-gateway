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
	EnvPath                string
	EnvPaths               []string
	WrapperPath            string
	ServicePath            string
	ServicePaths           []string
	ClaudeEnvPath          string
	ClaudeWrapperPath      string
	ClaudeConfigPath       string
	ClaudeServicePath      string
	ClaudeAdminServicePath string
	ClaudeBinaryPath       string
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
	endpoints := clientEndpoints(spec)
	envPath := filepath.Join(spec.InstallDir, "proxy.env")
	serviceInstall, err := newSystemdInstall(spec.ServiceScope, clientServiceName(spec, endpoints[0]))
	if err != nil {
		return ClientInstallResult{}, err
	}
	if err := serviceInstall.preflight(spec.WriteOnly); err != nil {
		return ClientInstallResult{}, err
	}

	if err := os.MkdirAll(spec.InstallDir, 0o755); err != nil {
		return ClientInstallResult{}, fmt.Errorf("mkdir install dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(serviceInstall.servicePath), 0o755); err != nil {
		return ClientInstallResult{}, fmt.Errorf("mkdir service dir: %w", err)
	}

	var wrapperPath string
	if clientModeIncludes(spec.Mode, ClientModeProxy) {
		wrapperPath, err = clientWrapperPath(spec.WrapperName)
		if err != nil {
			return ClientInstallResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("mkdir wrapper dir: %w", err)
		}
	}

	envPaths := make([]string, 0, len(endpoints))
	servicePaths := make([]string, 0, len(endpoints))
	wrapperEndpoints := make([]clientWrapperEndpoint, 0, len(endpoints))
	for index, endpoint := range endpoints {
		if clientModeIncludes(spec.Mode, ClientModeProxy) {
			endpointEnvPath := envPath
			if len(endpoints) > 1 {
				endpointEnvPath = filepath.Join(spec.InstallDir, "proxy-"+endpoint.Name+".env")
			}
			if err := os.WriteFile(endpointEnvPath, RenderClientEnvForEndpoint(spec.Proxy, endpoint), 0o600); err != nil {
				return ClientInstallResult{}, fmt.Errorf("write client env for %s: %w", endpoint.Name, err)
			}
			if len(endpoints) > 1 && index == 0 {
				if err := os.WriteFile(envPath, RenderClientEnvForEndpoint(spec.Proxy, endpoint), 0o600); err != nil {
					return ClientInstallResult{}, fmt.Errorf("write default client env: %w", err)
				}
			}
			envPaths = append(envPaths, endpointEnvPath)
			wrapperEndpoints = append(wrapperEndpoints, clientWrapperEndpoint{
				Name:      endpoint.Name,
				EnvPath:   endpointEnvPath,
				LocalHost: endpoint.Tunnel.LocalHost,
				LocalPort: endpoint.Tunnel.LocalPort,
			})
		}

		endpointServiceName := clientServiceName(spec, endpoint)
		endpointServiceInstall, err := newSystemdInstall(spec.ServiceScope, endpointServiceName)
		if err != nil {
			return ClientInstallResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(endpointServiceInstall.servicePath), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("mkdir service dir for %s: %w", endpoint.Name, err)
		}
		if err := os.WriteFile(endpointServiceInstall.servicePath, RenderClientServiceForEndpoint(spec, endpoint), 0o644); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write service for %s: %w", endpoint.Name, err)
		}
		servicePaths = append(servicePaths, endpointServiceInstall.servicePath)
	}

	if clientModeIncludes(spec.Mode, ClientModeProxy) {
		if err := os.WriteFile(wrapperPath, RenderClientWrapperForEndpoints(spec, wrapperEndpoints), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write wrapper: %w", err)
		}
	}

	result := ClientInstallResult{
		ServicePath:  servicePaths[0],
		ServicePaths: servicePaths,
	}
	if clientModeIncludes(spec.Mode, ClientModeProxy) {
		result.EnvPath = envPath
		result.EnvPaths = envPaths
		result.WrapperPath = wrapperPath
	}

	if clientModeIncludes(spec.Mode, ClientModeClaude) {
		endpoint := endpoints[0]
		claudeEnvPath := filepath.Join(spec.InstallDir, "claude.env")
		claudeConfigPath := filepath.Join(spec.InstallDir, "claude-client.yaml")
		claudeWrapperPath, err := clientWrapperPath(spec.ClaudeCode.WrapperName)
		if err != nil {
			return ClientInstallResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(claudeWrapperPath), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("mkdir claude wrapper dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(spec.ClaudeCode.BinaryOutput), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("mkdir claude binary dir: %w", err)
		}

		claudeAdminInstall, err := newSystemdInstall(spec.ServiceScope, spec.ClaudeCode.AdminServiceName)
		if err != nil {
			return ClientInstallResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(claudeAdminInstall.servicePath), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("mkdir claude admin service dir: %w", err)
		}

		claudeServiceInstall, err := newSystemdInstall(spec.ServiceScope, spec.ClaudeCode.ServiceName)
		if err != nil {
			return ClientInstallResult{}, err
		}
		if err := os.MkdirAll(filepath.Dir(claudeServiceInstall.servicePath), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("mkdir claude service dir: %w", err)
		}

		claudeConfigContent, err := RenderClaudeClientConfig(spec, endpoint)
		if err != nil {
			return ClientInstallResult{}, fmt.Errorf("render claude client config: %w", err)
		}
		if err := os.WriteFile(claudeEnvPath, RenderClaudeClientEnv(spec), 0o600); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write claude env: %w", err)
		}
		if err := os.WriteFile(claudeConfigPath, claudeConfigContent, 0o600); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write claude config: %w", err)
		}
		if err := os.WriteFile(claudeWrapperPath, RenderClaudeClientWrapper(spec, claudeEnvPath), 0o755); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write claude wrapper: %w", err)
		}
		if err := os.WriteFile(claudeAdminInstall.servicePath, RenderClaudeAdminService(spec, endpoint), 0o644); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write claude admin service: %w", err)
		}
		if err := os.WriteFile(claudeServiceInstall.servicePath, RenderClaudeClientService(spec, claudeConfigPath, clientServiceName(spec, endpoint), spec.ClaudeCode.AdminServiceName), 0o644); err != nil {
			return ClientInstallResult{}, fmt.Errorf("write claude client service: %w", err)
		}

		result.ClaudeEnvPath = claudeEnvPath
		result.ClaudeWrapperPath = claudeWrapperPath
		result.ClaudeConfigPath = claudeConfigPath
		result.ClaudeServicePath = claudeServiceInstall.servicePath
		result.ClaudeAdminServicePath = claudeAdminInstall.servicePath
		result.ClaudeBinaryPath = spec.ClaudeCode.BinaryOutput
	}

	if !spec.WriteOnly {
		if clientModeIncludes(spec.Mode, ClientModeClaude) {
			if err := ensureGoToolchain(); err != nil {
				return ClientInstallResult{}, err
			}
			if err := buildBinary(spec.ProjectRoot, spec.ClaudeCode.BinaryOutput); err != nil {
				return ClientInstallResult{}, err
			}
		}
		if err := runCommand("systemctl", serviceInstall.systemctlArgs("daemon-reload")...); err != nil {
			return ClientInstallResult{}, err
		}
		for _, endpoint := range endpoints {
			endpointServiceName := clientServiceName(spec, endpoint)
			if err := runCommand("systemctl", serviceInstall.systemctlArgs("enable", "--now", endpointServiceName+".service")...); err != nil {
				return ClientInstallResult{}, err
			}
		}
		if clientModeIncludes(spec.Mode, ClientModeClaude) {
			if err := runCommand("systemctl", serviceInstall.systemctlArgs("enable", "--now", spec.ClaudeCode.AdminServiceName+".service")...); err != nil {
				return ClientInstallResult{}, err
			}
			if err := runCommand("systemctl", serviceInstall.systemctlArgs("enable", "--now", spec.ClaudeCode.ServiceName+".service")...); err != nil {
				return ClientInstallResult{}, err
			}
		}
	}

	return result, nil
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

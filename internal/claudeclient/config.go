package claudeclient

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr            string         `yaml:"listen_addr"`
	UpstreamURL           string         `yaml:"upstream_url"`
	ProxyURL              string         `yaml:"proxy_url"`
	OAuthBrokerURL        string         `yaml:"oauth_broker_url"`
	OAuthUsername         string         `yaml:"oauth_username"`
	OAuthPassword         string         `yaml:"oauth_password"`
	TLSInsecureSkipVerify bool           `yaml:"tls_insecure_skip_verify"`
	LogLevel              string         `yaml:"log_level"`
	LogFormat             string         `yaml:"log_format"`
	Identity              Identity       `yaml:"identity"`
	Env                   EnvProfile     `yaml:"env"`
	PromptEnv             PromptProfile  `yaml:"prompt_env"`
	Process               ProcessProfile `yaml:"process"`
}

type Identity struct {
	DeviceID string `yaml:"device_id"`
	Email    string `yaml:"email"`
}

type EnvProfile struct {
	Platform              string `yaml:"platform"`
	PlatformRaw           string `yaml:"platform_raw"`
	Arch                  string `yaml:"arch"`
	NodeVersion           string `yaml:"node_version"`
	Terminal              string `yaml:"terminal"`
	PackageManagers       string `yaml:"package_managers"`
	Runtimes              string `yaml:"runtimes"`
	IsRunningWithBun      bool   `yaml:"is_running_with_bun"`
	IsClaudeAIAuth        bool   `yaml:"is_claude_ai_auth"`
	Version               string `yaml:"version"`
	VersionBase           string `yaml:"version_base"`
	BuildTime             string `yaml:"build_time"`
	DeploymentEnvironment string `yaml:"deployment_environment"`
	VCS                   string `yaml:"vcs"`
}

type PromptProfile struct {
	Platform   string `yaml:"platform"`
	Shell      string `yaml:"shell"`
	OSVersion  string `yaml:"os_version"`
	WorkingDir string `yaml:"working_dir"`
}

type ProcessProfile struct {
	ConstrainedMemory int64    `yaml:"constrained_memory"`
	RSSRange          [2]int64 `yaml:"rss_range"`
	HeapTotalRange    [2]int64 `yaml:"heap_total_range"`
	HeapUsedRange     [2]int64 `yaml:"heap_used_range"`
}

func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}

	applyClaudeDefaults(&config)
	if err := config.Validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func applyClaudeDefaults(config *Config) {
	if strings.TrimSpace(config.ListenAddr) == "" {
		config.ListenAddr = "127.0.0.1:11443"
	}
	if strings.TrimSpace(config.UpstreamURL) == "" {
		config.UpstreamURL = "https://api.anthropic.com"
	}
	if strings.TrimSpace(config.LogLevel) == "" {
		config.LogLevel = "info"
	}
	if strings.TrimSpace(config.LogFormat) == "" {
		config.LogFormat = "json"
	}
	if strings.TrimSpace(config.Env.PlatformRaw) == "" {
		config.Env.PlatformRaw = config.Env.Platform
	}
	if strings.TrimSpace(config.Env.VersionBase) == "" {
		config.Env.VersionBase = config.Env.Version
	}
	if !config.Env.IsClaudeAIAuth {
		config.Env.IsClaudeAIAuth = true
	}
}

func (c Config) Validate() error {
	var problems []string

	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		problems = append(problems, "listen_addr must be host:port")
	}
	if _, err := parseURL(c.UpstreamURL); err != nil {
		problems = append(problems, "invalid upstream_url: "+err.Error())
	}
	if _, err := parseURL(c.ProxyURL); err != nil {
		problems = append(problems, "invalid proxy_url: "+err.Error())
	}
	if _, err := parseURL(c.OAuthBrokerURL); err != nil {
		problems = append(problems, "invalid oauth_broker_url: "+err.Error())
	}
	if strings.TrimSpace(c.OAuthUsername) == "" || strings.TrimSpace(c.OAuthPassword) == "" {
		problems = append(problems, "oauth_username and oauth_password are required")
	}
	if strings.TrimSpace(c.Identity.DeviceID) == "" {
		problems = append(problems, "identity.device_id is required")
	}
	if strings.TrimSpace(c.Env.Version) == "" {
		problems = append(problems, "env.version is required")
	}
	if strings.TrimSpace(c.PromptEnv.WorkingDir) == "" {
		problems = append(problems, "prompt_env.working_dir is required")
	}
	if c.Process.RSSRange[1] < c.Process.RSSRange[0] {
		problems = append(problems, "process.rss_range must be [min,max]")
	}
	if c.Process.HeapTotalRange[1] < c.Process.HeapTotalRange[0] {
		problems = append(problems, "process.heap_total_range must be [min,max]")
	}
	if c.Process.HeapUsedRange[1] < c.Process.HeapUsedRange[0] {
		problems = append(problems, "process.heap_used_range must be [min,max]")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func parseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("expected absolute URL")
	}
	return parsed, nil
}

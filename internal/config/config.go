// Package config defines the immutable deployment layout and update workflow configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Config describes one remote application and its fixed release layout.
type Config struct {
	Remote RemoteConfig `json:"remote"`
	App    AppConfig    `json:"app"`
	Policy PolicyConfig `json:"policy"`
}

// RemoteConfig configures the existing SimpleWebShell endpoint.
type RemoteConfig struct {
	URL            string        `json:"url"`
	Key            string        `json:"key,omitempty"`
	Timeout        time.Duration `json:"-"`
	TimeoutText    string        `json:"timeout,omitempty"`
	InsecureTLS    bool          `json:"insecure_tls,omitempty"`
	SessionEnabled bool          `json:"session_enabled,omitempty"`
}

// AppConfig configures the application layout and lifecycle commands.
type AppConfig struct {
	Name          string `json:"name"`
	Root          string `json:"root"`
	ArtifactType  string `json:"artifact_type,omitempty"` // tar.gz, tar, zip, file
	InstallSubdir string `json:"install_subdir,omitempty"`
	PreSwitch     string `json:"pre_switch,omitempty"`
	PostSwitch    string `json:"post_switch,omitempty"`
	HealthCheck   string `json:"health_check,omitempty"`
	Rollback      string `json:"rollback,omitempty"`
}

// PolicyConfig controls retention and verification.
type PolicyConfig struct {
	KeepReleases        int  `json:"keep_releases,omitempty"`
	AutoRollback        bool `json:"auto_rollback,omitempty"`
	DisableAutoRollback bool `json:"disable_auto_rollback,omitempty"`
	RequireHealthCheck  bool `json:"require_health_check,omitempty"`
}

// Load reads a JSON configuration file and applies safe defaults.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	cfg.ApplyDefaults()
	if envKey := os.Getenv("SIMPLEWEBSHELL_KEY"); envKey != "" {
		cfg.Remote.Key = envKey
	}
	return &cfg, cfg.Validate()
}

// ApplyDefaults applies workflow defaults without mutating explicit values.
func (c *Config) ApplyDefaults() {
	c.Remote.URL = strings.TrimRight(strings.TrimSpace(c.Remote.URL), "/")
	if c.Remote.TimeoutText == "" {
		c.Remote.TimeoutText = "10m"
	}
	if d, err := time.ParseDuration(c.Remote.TimeoutText); err == nil {
		c.Remote.Timeout = d
	}
	if c.App.ArtifactType == "" {
		c.App.ArtifactType = "tar.gz"
	}
	if c.Policy.KeepReleases <= 0 {
		c.Policy.KeepReleases = 5
	}
	c.Policy.AutoRollback = !c.Policy.DisableAutoRollback
}

// Validate rejects unsafe or ambiguous deployment configurations.
func (c *Config) Validate() error {
	if c.Remote.URL == "" {
		return errors.New("remote.url 不能为空")
	}
	if !strings.HasPrefix(c.Remote.URL, "http://") && !strings.HasPrefix(c.Remote.URL, "https://") {
		return errors.New("remote.url 必须以 http:// 或 https:// 开头")
	}
	if c.Remote.Key == "" {
		return errors.New("SimpleWebShell key 未设置：请配置 remote.key 或 SIMPLEWEBSHELL_KEY")
	}
	if c.Remote.Timeout <= 0 {
		return fmt.Errorf("remote.timeout 非法: %q", c.Remote.TimeoutText)
	}
	if c.App.Name == "" {
		return errors.New("app.name 不能为空")
	}
	if err := ValidateRemotePath(c.App.Root); err != nil {
		return fmt.Errorf("app.root: %w", err)
	}
	switch c.App.ArtifactType {
	case "tar.gz", "tgz", "tar", "zip", "file":
	default:
		return fmt.Errorf("不支持的 artifact_type: %s", c.App.ArtifactType)
	}
	if c.App.InstallSubdir != "" {
		clean := path.Clean(c.App.InstallSubdir)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.ContainsAny(clean, "\x00\r\n") {
			return errors.New("app.install_subdir 必须是 releases/<version> 内的安全相对路径")
		}
		c.App.InstallSubdir = clean
	}
	return nil
}

// ValidateRemotePath ensures a path is absolute and free from control characters.
func ValidateRemotePath(p string) error {
	if p == "" {
		return errors.New("路径不能为空")
	}
	if strings.ContainsAny(p, "\x00\r\n") {
		return errors.New("路径包含控制字符")
	}
	if !filepath.IsAbs(p) {
		return errors.New("必须使用绝对路径")
	}
	return nil
}

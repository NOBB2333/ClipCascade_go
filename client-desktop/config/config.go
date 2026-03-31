// Package config 管理 desktop client config。
package config

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/clipcascade/pkg/constants"
)

// Config 保存 client config。
type Config struct {
	ServerURL      string `json:"server_url"` // e.g. "http://localhost:8080"
	Username       string `json:"username"`
	Password       string `json:"password"`
	E2EEEnabled    bool   `json:"e2ee_enabled"`
	P2PEnabled     bool   `json:"p2p_enabled"`
	StunURL        string `json:"stun_url"`
	AutoReconnect  bool   `json:"auto_reconnect"`
	ReconnectDelay int    `json:"reconnect_delay_sec"` // seconds
	SendText       bool   `json:"-"`                   // 运行期发送过滤：文本
	SendImage      bool   `json:"-"`                   // 运行期发送过滤：图片
	SendFile       bool   `json:"-"`                   // 运行期发送过滤：文件
	FilePath       string `json:"-"`                   // config 文件路径，不序列化
}

// DefaultConfig 返回带有合理默认值的 config。
func DefaultConfig() *Config {
	return &Config{
		ServerURL:      "http://localhost:" + strconv.Itoa(constants.DefaultPort),
		Username:       "",
		Password:       "",
		E2EEEnabled:    true,
		P2PEnabled:     true,
		StunURL:        constants.DefaultStunURL,
		AutoReconnect:  true,
		ReconnectDelay: constants.DefaultReconnectDelay,
		SendText:       true,
		SendImage:      true,
		SendFile:       true,
	}
}

// ConfigDir 返回特定于 OS 的 config 目录。
func ConfigDir() string {
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("APPDATA")
	case "darwin":
		dir, _ = os.UserHomeDir()
		dir = filepath.Join(dir, "Library", "Application Support")
	default: // linux
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir, _ = os.UserHomeDir()
			dir = filepath.Join(dir, ".config")
		}
	}
	return filepath.Join(dir, "ClipCascade")
}

// Load 从文件中读取 config，如果未找到则返回默认值。
func Load() *Config {
	cfg := DefaultConfig()
	cfgPath := filepath.Join(ConfigDir(), "config.json")
	cfg.FilePath = cfgPath

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, cfg)
	cfg.FilePath = cfgPath
	cfg.ServerURL = NormalizeServerURL(cfg.ServerURL)
	if envPassword := os.Getenv("CLIPCASCADE_PASSWORD"); envPassword != "" {
		cfg.Password = envPassword
	}
	return cfg
}

// Save 将 config 写入磁盘。
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.FilePath), 0755); err != nil {
		return err
	}
	toSave := *c
	toSave.ServerURL = NormalizeServerURL(toSave.ServerURL)
	// 当使用环境变量注入密码时，避免将密码明文持久化到磁盘。
	if os.Getenv("CLIPCASCADE_PASSWORD") != "" {
		toSave.Password = ""
	}
	data, err := json.MarshalIndent(toSave, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.FilePath, data, 0600)
}

// SaveServerURLOnly 仅持久化 server_url，避免无意覆盖用户名/密码等配置。
func (c *Config) SaveServerURLOnly(serverURL string) error {
	cfgPath := c.FilePath
	if cfgPath == "" {
		cfgPath = filepath.Join(ConfigDir(), "config.json")
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return err
	}

	// 优先保留现有文件中的其余字段，只替换 server_url。
	existing := DefaultConfig()
	existing.FilePath = cfgPath
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, existing)
	}
	existing.ServerURL = NormalizeServerURL(serverURL)

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0600)
}

// NormalizeServerURL 规范化 server 地址，支持裸 host:port 自动补 http://。
func NormalizeServerURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return strings.TrimRight(s, "/")
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	normalized := u.String()
	return strings.TrimRight(normalized, "/")
}

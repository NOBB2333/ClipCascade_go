// Package config 管理 desktop client config。
package config

import (
	"encoding/json"
	"os"
	"strconv"
	"path/filepath"
	"runtime"

	"github.com/clipcascade/pkg/constants"
)

// Config 保存 client config。
type Config struct {
	ServerURL      string `json:"server_url"`  // e.g. "http://localhost:8080"
	Username       string `json:"username"`
	Password       string `json:"password"`
	E2EEEnabled    bool   `json:"e2ee_enabled"`
	P2PEnabled     bool   `json:"p2p_enabled"`
	StunURL        string `json:"stun_url"`
	AutoReconnect  bool   `json:"auto_reconnect"`
	ReconnectDelay int    `json:"reconnect_delay_sec"` // seconds
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
	return cfg
}

// Save 将 config 写入磁盘。
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.FilePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.FilePath, data, 0600)
}

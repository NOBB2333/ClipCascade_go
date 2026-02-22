// Package config 为 ClipCascade server 提供基于环境变量的 config，
// 将 CC_* 环境变量映射到 Go struct。
package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/clipcascade/pkg/constants"
)

// Config 保存所有 server config。
type Config struct {
	Port                    int
	MaxMessageSizeMiB       int
	MaxMessageSizeBytes     int64
	P2PEnabled              bool
	P2PStunURL              string
	AllowedOrigins          string
	SignupEnabled           bool
	MaxUserAccounts         int
	AccountPurgeTimeout     int64 // seconds, -1 = disabled
	SessionTimeoutMinutes   int
	MaxUniqueIPAttempts     int
	MaxAttemptsPerIP        int
	LockTimeoutSeconds      int
	LockScalingFactor       int
	BFACacheEnabled         bool
	DatabasePath            string
	ExternalBrokerEnabled   bool
	BrokerHost              string
	BrokerPort              int
}

// Load 从带有合理默认值的环境变量中读取 config。
func Load() *Config {
	c := &Config{
		Port:                  envInt("CC_PORT", constants.DefaultPort),
		MaxMessageSizeMiB:    envInt("CC_MAX_MESSAGE_SIZE_IN_MiB", constants.DefaultMaxMessageSizeMiB),
		MaxMessageSizeBytes:  envInt64("CC_MAX_MESSAGE_SIZE_IN_BYTES", 0),
		P2PEnabled:           envBool("CC_P2P_ENABLED", false),
		P2PStunURL:           envStr("CC_P2P_STUN_URL", constants.DefaultStunURL),
		AllowedOrigins:       envStr("CC_ALLOWED_ORIGINS", "*"),
		SignupEnabled:        envBool("CC_SIGNUP_ENABLED", false),
		MaxUserAccounts:      envInt("CC_MAX_USER_ACCOUNTS", -1),
		AccountPurgeTimeout:  envInt64("CC_ACCOUNT_PURGE_TIMEOUT_SECONDS", -1),
		SessionTimeoutMinutes: envInt("CC_SESSION_TIMEOUT", constants.DefaultSessionTimeoutMin),
		MaxUniqueIPAttempts:  envInt("CC_MAX_UNIQUE_IP_ATTEMPTS", constants.DefaultMaxUniqueIPAttempts),
		MaxAttemptsPerIP:     envInt("CC_MAX_ATTEMPTS_PER_IP", constants.DefaultMaxAttemptsPerIP),
		LockTimeoutSeconds:   envInt("CC_LOCK_TIMEOUT_SECONDS", constants.DefaultLockTimeoutSeconds),
		LockScalingFactor:    envInt("CC_LOCK_TIMEOUT_SCALING_FACTOR", constants.DefaultLockScalingFactor),
		BFACacheEnabled:      envBool("CC_BFA_CACHE_ENABLED", true),
		DatabasePath:         envStr("CC_DATABASE_PATH", "./database/clipcascade.db"),
		ExternalBrokerEnabled: envBool("CC_EXTERNAL_BROKER_ENABLED", false),
		BrokerHost:           envStr("CC_BROKER_HOST", "localhost"),
		BrokerPort:           envInt("CC_BROKER_PORT", 61613),
	}
	return c
}

// EffectiveMaxMessageBytes 返回以字节为单位的有效最大消息大小。
func (c *Config) EffectiveMaxMessageBytes() int64 {
	if c.P2PEnabled {
		return 0 // P2P mode 下不限制大小
	}
	if c.MaxMessageSizeBytes > 0 {
		return c.MaxMessageSizeBytes
	}
	return int64(c.MaxMessageSizeMiB) * 1024 * 1024
}

// AllowedOriginsList 将允许的 origins 字符串解析为切片。
func (c *Config) AllowedOriginsList() []string {
	if c.AllowedOrigins == "*" {
		return []string{"*"}
	}
	origins := strings.Split(c.AllowedOrigins, ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	return origins
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

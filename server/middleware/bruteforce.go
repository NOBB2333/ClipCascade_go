// Package middleware 为 ClipCascade server 提供自定义的 Fiber 中间件。
package middleware

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/clipcascade/server/config"
)

// BruteForceProtection 跟踪每个 IP 的失败登录尝试并执行锁定。
type BruteForceProtection struct {
	mu       sync.Mutex
	attempts map[string]*attemptInfo // IP → 尝试跟踪
	cfg      *config.Config
}

type attemptInfo struct {
	Count     int
	LockedAt  time.Time
	LockDur   time.Duration
}

// NewBruteForceProtection 创建一个新的蛮力攻击保护中间件。
func NewBruteForceProtection(cfg *config.Config) *BruteForceProtection {
	return &BruteForceProtection{
		attempts: make(map[string]*attemptInfo),
		cfg:      cfg,
	}
}

// Check 是 Fiber 中间件，保护 login 端点免受蛮力攻击。
func (bf *BruteForceProtection) Check(c *fiber.Ctx) error {
	ip := c.IP()

	bf.mu.Lock()
	info, exists := bf.attempts[ip]
	bf.mu.Unlock()

	if exists && !info.LockedAt.IsZero() {
		remaining := time.Until(info.LockedAt.Add(info.LockDur))
		if remaining > 0 {
			slog.Warn("暴力破解防护：IP 已锁定", "IP", ip, "剩余时间", remaining)
			return c.Status(fiber.StatusTooManyRequests).SendString(
				"Too many failed attempts. Retry after " + remaining.Round(time.Second).String())
		}
		// 锁定过期，重置
		bf.mu.Lock()
		delete(bf.attempts, ip)
		bf.mu.Unlock()
	}

	return c.Next()
}

// RecordFailure 记录给定 IP 的失败登录尝试。
func (bf *BruteForceProtection) RecordFailure(ip string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	info, exists := bf.attempts[ip]
	if !exists {
		info = &attemptInfo{LockDur: time.Duration(bf.cfg.LockTimeoutSeconds) * time.Second}
		bf.attempts[ip] = info
	}

	info.Count++

	if info.Count >= bf.cfg.MaxAttemptsPerIP {
		info.LockedAt = time.Now()
		// 缩放锁定持续时间
		info.LockDur *= time.Duration(bf.cfg.LockScalingFactor)
		slog.Warn("暴力破解防护：IP 被封禁", "IP", ip,
			"尝试次数", info.Count, "锁定时长", info.LockDur)
	}
}

// RecordSuccess 清除成功登录的尝试计数器。
func (bf *BruteForceProtection) RecordSuccess(ip string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	delete(bf.attempts, ip)
}

// Cleanup 定期移除过期的锁定条目。
func (bf *BruteForceProtection) Cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		bf.mu.Lock()
		now := time.Now()
		for ip, info := range bf.attempts {
			if !info.LockedAt.IsZero() && now.After(info.LockedAt.Add(info.LockDur)) {
				delete(bf.attempts, ip)
			}
		}
		bf.mu.Unlock()
	}
}

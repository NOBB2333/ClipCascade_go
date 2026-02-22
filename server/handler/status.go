package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/clipcascade/server/config"
)

// StatusHandler 提供健康检查和状态端点。
type StatusHandler struct {
	Config *config.Config
}

func NewStatusHandler(cfg *config.Config) *StatusHandler {
	return &StatusHandler{Config: cfg}
}

// Health 为负载均衡器健康检查返回 "OK"。
func (h *StatusHandler) Health(c *fiber.Ctx) error {
	return c.SendString("OK")
}

// Ping 为连接性测试返回 "pong"。
func (h *StatusHandler) Ping(c *fiber.Ctx) error {
	return c.SendString("pong")
}

// ServerMode 返回当前 server 模式 (p2p 或 server)。
func (h *StatusHandler) ServerMode(c *fiber.Ctx) error {
	mode := "server"
	if h.Config.P2PEnabled {
		mode = "p2p"
	}
	return c.JSON(fiber.Map{"mode": mode})
}

// GetStunURL 返回 P2P mode 的 STUN server URL。
func (h *StatusHandler) GetStunURL(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"stun_url": h.Config.P2PStunURL})
}

// GetMaxSizeAllowed 返回以字节为单位的最大消息大小。
func (h *StatusHandler) GetMaxSizeAllowed(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"max_size": h.Config.EffectiveMaxMessageBytes()})
}

package handler

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/clipcascade/server/config"
	"github.com/clipcascade/pkg/constants"
	"github.com/clipcascade/server/model"
)

// AdminHandler 处理仅限 admin 的 API 端点。
type AdminHandler struct {
	DB     *gorm.DB
	Config *config.Config
	Hub    *WSHub
	P2P    *P2PSignaling
}

// NewAdminHandler 创建一个新的 AdminHandler。
func NewAdminHandler(db *gorm.DB, cfg *config.Config, hub *WSHub, p2p *P2PSignaling) *AdminHandler {
	return &AdminHandler{DB: db, Config: cfg, Hub: hub, P2P: p2p}
}

// AdminOnly 是限制 admin 用户访问的中间件。
func AdminOnly(c *fiber.Ctx) error {
	role, _ := c.Locals("role").(string)
	if role != "ADMIN" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin access required"})
	}
	return c.Next()
}

// ListUsers 返回所有注册的 users。
func (h *AdminHandler) ListUsers(c *fiber.Ctx) error {
	var users []model.User
	h.DB.Find(&users)

	result := make([]fiber.Map, 0, len(users))
	for _, u := range users {
		result = append(result, fiber.Map{
			"id":         u.ID,
			"username":   u.Username,
			"role":       u.Role,
			"enabled":    u.Enabled,
			"created_at": u.CreatedAt,
		})
	}
	return c.JSON(result)
}

// DeleteUser 根据 ID 删除 user。
func (h *AdminHandler) DeleteUser(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	var user model.User
	if err := h.DB.First(&user, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	// 防止删除你自己
	currentUsername, _ := c.Locals("username").(string)
	if user.Username == currentUsername {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "cannot delete yourself"})
	}

	// 删除关联的 UserInfo
	h.DB.Where("user_id = ?", user.ID).Delete(&model.UserInfo{})
	h.DB.Delete(&user)

	slog.Info("管理员：用户已删除", "用户名", user.Username, "操作人", currentUsername)
	return c.JSON(fiber.Map{"message": "user deleted", "username": user.Username})
}

// ResetPassword 重置 user 的 password。
func (h *AdminHandler) ResetPassword(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil || body.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "password required"})
	}
	if len(body.Password) < 4 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "password too short (min 4)"})
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "hash error"})
	}

	if err := h.DB.Model(&model.User{}).Where("id = ?", id).Update("password", string(hashed)).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
	}

	slog.Info("管理员：密码已重置", "用户ID", id, "操作人", c.Locals("username"))
	return c.JSON(fiber.Map{"message": "password updated"})
}

// SetRole 更改 user 的角色。
func (h *AdminHandler) SetRole(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := c.BodyParser(&body); err != nil || (body.Role != "USER" && body.Role != "ADMIN") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "role must be USER or ADMIN"})
	}

	h.DB.Model(&model.User{}).Where("id = ?", id).Update("role", body.Role)
	slog.Info("管理员：角色已更改", "用户ID", id, "新角色", body.Role, "操作人", c.Locals("username"))
	return c.JSON(fiber.Map{"message": "role updated"})
}

// ServerStats 返回全面的 server 统计信息。
func (h *AdminHandler) ServerStats(c *fiber.Ctx) error {
	var userCount int64
	h.DB.Model(&model.User{}).Count(&userCount)

	wsStats := h.Hub.Stats()
	p2pStats := h.P2P.Stats()

	return c.JSON(fiber.Map{
		"users":       userCount,
		"ws":          wsStats,
		"p2p":         p2pStats,
		"p2p_enabled": h.Config.P2PEnabled,
		"signup":      h.Config.SignupEnabled,
	})
}

// AdvancePage 渲染 admin 管理页面。
func (h *AdminHandler) AdvancePage(c *fiber.Ctx) error {
	return c.Render("web/templates/advance", fiber.Map{
		"username": c.Locals("username"),
	})
}

// ToggleUserStatus 启用或禁用 user 账户。
func (h *AdminHandler) ToggleUserStatus(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
	}

	h.DB.Model(&model.User{}).Where("id = ?", id).Update("enabled", body.Enabled)
	status := "disabled"
	if body.Enabled {
		status = "enabled"
	}
	slog.Info("管理员：用户状态已更改", "用户ID", id, "状态", status, "操作人", c.Locals("username"))
	return c.JSON(fiber.Map{"message": "user " + status})
}

// RegisterUser 允许 admin 创建新 user。
func (h *AdminHandler) RegisterUser(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil || body.Username == "" || body.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username and password required"})
	}
	if len(body.Password) < 4 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "password too short (min 4)"})
	}

	// 检查 user 是否已存在
	var existing model.User
	if h.DB.Where("username = ?", body.Username).First(&existing).Error == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "user already exists"})
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "hash error"})
	}

	user := model.User{
		Username: body.Username,
		Password: string(hashed),
		Role:     "USER",
		Enabled:  true,
	}
	if err := h.DB.Create(&user).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "create failed"})
	}

	h.DB.Create(&model.UserInfo{UserID: user.ID, Salt: body.Username, HashRounds: 100000})
	slog.Info("管理员：用户已注册", "用户名", body.Username, "操作人", c.Locals("username"))
	return c.JSON(fiber.Map{"message": "user registered", "username": body.Username})
}

// UpdateUsername 更改 user 的 username。
func (h *AdminHandler) UpdateUsername(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid user ID"})
	}

	var body struct {
		Username string `json:"username"`
	}
	if err := c.BodyParser(&body); err != nil || body.Username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username required"})
	}

	// 检查唯一性
	var existing model.User
	if h.DB.Where("username = ?", body.Username).First(&existing).Error == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "username already taken"})
	}

	h.DB.Model(&model.User{}).Where("id = ?", id).Update("username", body.Username)
	slog.Info("管理员：用户名已更改", "用户ID", id, "新用户名", body.Username, "操作人", c.Locals("username"))
	return c.JSON(fiber.Map{"message": "username updated"})
}

// ServerVersion 返回当前 server 版本。
func (h *AdminHandler) ServerVersion(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"version": constants.AppVersion})
}

// ServerTime 返回当前 server 时间戳。
func (h *AdminHandler) ServerTime(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"time": time.Now().Unix(), "formatted": time.Now().Format(time.RFC3339)})
}

// UpdatePasswordSelf 允许 user 更改自己的 password。
func UpdatePasswordSelf(db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(uint)

		var body struct {
			NewPassword string `json:"newPassword"`
		}
		if err := c.BodyParser(&body); err != nil || body.NewPassword == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "newPassword required"})
		}
		if len(body.NewPassword) < 4 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "password too short (min 4)"})
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "hash error"})
		}

		db.Model(&model.User{}).Where("id = ?", userID).Update("password", string(hashed))
		slog.Info("用户：密码已更新", "用户ID", userID)
		return c.JSON(fiber.Map{"message": "password updated"})
	}
}

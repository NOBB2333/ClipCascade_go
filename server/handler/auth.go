// Package handler 为 server 提供 HTTP 和 WebSocket 请求 handlers。
package handler

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/clipcascade/server/config"
	"github.com/clipcascade/server/middleware"
	"github.com/clipcascade/server/model"
)

// AuthHandler 处理 user 身份验证端点。
type AuthHandler struct {
	DB     *gorm.DB
	Config *config.Config
	BFA    *middleware.BruteForceProtection
}

// NewAuthHandler 创建一个新的 AuthHandler。
func NewAuthHandler(db *gorm.DB, cfg *config.Config, bfa *middleware.BruteForceProtection) *AuthHandler {
	return &AuthHandler{DB: db, Config: cfg, BFA: bfa}
}

// LoginPage 渲染 login 页面。
func (h *AuthHandler) LoginPage(c *fiber.Ctx) error {
	return c.Render("web/templates/login", fiber.Map{
		"error":          c.Query("error"),
		"logout":         c.Query("logout"),
		"signup_enabled": h.Config.SignupEnabled,
	})
}

// LoginPost 处理基于表单的 login (username + password)。
func (h *AuthHandler) LoginPost(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return c.Redirect("/login?error=missing_credentials")
	}

	var user model.User
	if err := h.DB.Where("username = ?", username).First(&user).Error; err != nil {
		slog.Warn("登录失败：用户未找到", "用户名", username)
		if h.BFA != nil {
			h.BFA.RecordFailure(c.IP())
		}
		return c.Redirect("/login?error=invalid")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		slog.Warn("登录失败：密码错误", "用户名", username)
		if h.BFA != nil {
			h.BFA.RecordFailure(c.IP())
		}
		return c.Redirect("/login?error=invalid")
	}
	if !user.Enabled {
		slog.Warn("登录失败：用户被禁用", "用户名", username)
		if h.BFA != nil {
			h.BFA.RecordFailure(c.IP())
		}
		return c.Redirect("/login?error=disabled")
	}

	if h.BFA != nil {
		h.BFA.RecordSuccess(c.IP())
	}

	// 在 session 中存储 user 信息
	sess, err := Store.Get(c)
	if err != nil {
		slog.Error("会话错误", "错误", err)
		return c.Redirect("/login?error=session_error")
	}

	sess.Set("user_id", user.ID)
	sess.Set("username", user.Username)
	sess.Set("role", user.Role)

	if err := sess.Save(); err != nil {
		slog.Error("会话保存错误", "错误", err)
		return c.Redirect("/login?error=session_error")
	}

	slog.Info("用户已登录", "用户名", username)
	return c.Redirect("/")
}

// SignupPage 渲染 signup 页面。
func (h *AuthHandler) SignupPage(c *fiber.Ctx) error {
	if !h.Config.SignupEnabled {
		return c.Status(fiber.StatusForbidden).SendString("Signup is disabled")
	}
	return c.Render("web/templates/signup", fiber.Map{
		"error": c.Query("error"),
	})
}

// SignupPost 处理 user 注册。
func (h *AuthHandler) SignupPost(c *fiber.Ctx) error {
	if !h.Config.SignupEnabled {
		return c.Status(fiber.StatusForbidden).SendString("Signup is disabled")
	}

	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return c.Redirect("/signup?error=missing_fields")
	}
	if len(username) > 50 || len(password) < 4 {
		return c.Redirect("/signup?error=invalid_length")
	}

	// 检查最大 accounts 数量
	if h.Config.MaxUserAccounts > 0 {
		var count int64
		h.DB.Model(&model.User{}).Count(&count)
		if count >= int64(h.Config.MaxUserAccounts) {
			return c.Redirect("/signup?error=max_accounts_reached")
		}
	}

	// 检查重复项
	var existing model.User
	if err := h.DB.Where("username = ?", username).First(&existing).Error; err == nil {
		return c.Redirect("/signup?error=user_exists")
	}

	// 对 password 进行 hash 处理
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt 错误", "错误", err)
		return c.Redirect("/signup?error=internal")
	}

	user := model.User{
		Username: username,
		Password: string(hashed),
		Role:     "USER",
	}
	if err := h.DB.Create(&user).Error; err != nil {
		slog.Error("创建用户错误", "错误", err)
		return c.Redirect("/signup?error=internal")
	}

	// 创建默认 UserInfo
	info := model.UserInfo{
		UserID:     user.ID,
		Salt:       username, // 默认 salt
		HashRounds: 100000,
	}
	h.DB.Create(&info)

	slog.Info("用户已注册", "用户名", username)
	return c.Redirect("/login")
}

// Logout 销毁 session 并重定向到 login。
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	sess, err := Store.Get(c)
	if err == nil {
		_ = sess.Destroy()
	}
	return c.Redirect("/login?logout=true")
}

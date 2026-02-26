// ClipCascade server - 一个用 Go 编写的轻量级剪贴板同步 server。
//
// 这取代了原来的 Java Spring Boot server，只有一个 ~10MB 的二进制文件。
// 使用 Fiber 框架处理 HTTP/WebSocket，GORM+SQLite 进行持久化。
package main

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite" // 纯 Go 实现的 SQLite (无需 CGO)
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/template/html/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/clipcascade/server/config"
	"github.com/clipcascade/server/handler"
	"github.com/clipcascade/server/middleware"
	"github.com/clipcascade/server/model"
	"github.com/grandcat/zeroconf"
	"net"
	"os/signal"
	"syscall"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

func main() {
	// 设置结构化日志
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// 加载 config
	cfg := config.Load()
	slog.Info("配置已加载",
		"port", cfg.Port,
		"p2p", cfg.P2PEnabled,
		"signup", cfg.SignupEnabled,
		"max_msg_mib", cfg.MaxMessageSizeMiB,
	)

	// 初始化界面数据库
	dbDir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		slog.Error("创建数据库目录失败", "error", err)
		os.Exit(1)
	}

	db, err := gorm.Open(sqlite.Open(cfg.DatabasePath), &gorm.Config{})
	if err != nil {
		slog.Error("打开数据库失败", "error", err)
		os.Exit(1)
	}

	if err := model.InitDB(db); err != nil {
		slog.Error("数据库迁移失败", "error", err)
		os.Exit(1)
	}

	// 如果不存在，播种默认 admin user
	seedAdminUser(db)

	// 初始化 session 存储
	handler.Store = session.New(session.Config{
		Expiration:     time.Duration(cfg.SessionTimeoutMinutes) * time.Minute,
		CookieHTTPOnly: true,
		CookieSameSite: "Lax",
	})

	// 使用嵌入的 HTML 模板初始化 Fiber 模板引擎
	engine := html.NewFileSystem(http.FS(templatesFS), ".html")

	// 创建带有模板引擎的 Fiber app
	app := fiber.New(fiber.Config{
		Views:     engine,
		BodyLimit: int(cfg.EffectiveMaxMessageBytes()) + 1024*1024, // max msg + 1MB 开销
	})

	// 全局中间件
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} ${path}\n",
		TimeFormat: "15:04:05",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowCredentials: cfg.AllowedOrigins != "*",
	}))

	// 提供嵌入的静态文件服务
	app.Use("/assets", filesystem.New(filesystem.Config{
		Root:       http.FS(staticFS),
		PathPrefix: "web/static",
	}))

	// 创建 handlers 和中间件
	wsHub := handler.NewWSHub()
	bfa := middleware.NewBruteForceProtection(cfg)
	go bfa.Cleanup() // 启动后台清理任务

	authHandler := handler.NewAuthHandler(db, cfg, bfa)
	statusHandler := handler.NewStatusHandler(cfg)

	// ---- 公共路由 (无需 auth) ----
	// 应用蛮力攻击保护中间件到登录和注册
	app.Get("/login", bfa.Check, authHandler.LoginPage)
	app.Post("/login", bfa.Check, authHandler.LoginPost)
	app.Get("/signup", bfa.Check, authHandler.SignupPage)
	app.Post("/signup", bfa.Check, authHandler.SignupPost)
	app.Get("/health", statusHandler.Health)
	app.Get("/ping", statusHandler.Ping)

	// ---- 所有后续路由的 auth 中间件 ----
	app.Use(func(c *fiber.Ctx) error {
		// 跳过公共路径的 auth
		path := c.Path()
		if path == "/login" || path == "/signup" || path == "/health" || path == "/ping" {
			return c.Next()
		}

		sess, err := handler.Store.Get(c)
		if err != nil || sess.Get("username") == nil {
			// API 路由返回 JSON 401；页面路由重定向
			if strings.HasPrefix(path, "/api/") {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
			}
			return c.Redirect("/login")
		}

		// 在 local 中为下游 handlers 设置 user 信息
		c.Locals("username", sess.Get("username"))
		c.Locals("user_id", sess.Get("user_id"))
		c.Locals("role", sess.Get("role"))
		return c.Next()
	})

	// ---- 受保护的路由 ----
	app.Get("/", func(c *fiber.Ctx) error {
		mode := "Server-Based (STOMP)"
		if cfg.P2PEnabled {
			mode = "Peer-to-Peer (WebRTC)"
		}
		return c.Render("web/templates/home", fiber.Map{
			"username":     c.Locals("username"),
			"mode":         mode,
			"port":         cfg.Port,
			"max_size_mib": cfg.MaxMessageSizeMiB,
			"is_admin":     c.Locals("role") == "ADMIN",
		})
	})
	app.Get("/logout", authHandler.Logout)

	// API 端点
	app.Get("/api/server-mode", statusHandler.ServerMode)
	app.Get("/api/stun-url", statusHandler.GetStunURL)
	app.Get("/api/max-size", statusHandler.GetMaxSizeAllowed)
	app.Get("/api/validate-session", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"valid": true})
	})
	app.Get("/api/whoami", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"username": c.Locals("username"),
			"role":     c.Locals("role"),
		})
	})
	app.Get("/api/ws-stats", func(c *fiber.Ctx) error {
		return c.JSON(wsHub.Stats())
	})
	app.Get("/api/user-info", func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(uint)
		var info model.UserInfo
		if err := db.Where("user_id = ?", userID).First(&info).Error; err != nil {
			return c.JSON(fiber.Map{"salt": c.Locals("username"), "hash_rounds": 100000})
		}
		return c.JSON(fiber.Map{"salt": info.Salt, "hash_rounds": info.HashRounds})
	})

	// WebSocket 端点 (STOMP)
	app.Use("/clipsocket", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/clipsocket", websocket.New(func(c *websocket.Conn) {
		wsHub.HandleWebSocket(c)
	}))

	// P2P WebSocket signaling 端点
	p2pSignaling := handler.NewP2PSignaling()
	app.Use("/p2p", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/p2p", websocket.New(func(c *websocket.Conn) {
		p2pSignaling.HandleP2P(c)
	}))

	// 仅限 admin 的路由
	adminHandler := handler.NewAdminHandler(db, cfg, wsHub, p2pSignaling)
	admin := app.Group("/api/admin", handler.AdminOnly)
	admin.Get("/users", adminHandler.ListUsers)
	admin.Delete("/users/:id", adminHandler.DeleteUser)
	admin.Put("/users/:id/password", adminHandler.ResetPassword)
	admin.Put("/users/:id/role", adminHandler.SetRole)
	admin.Put("/users/:id/status", adminHandler.ToggleUserStatus)
	admin.Put("/users/:id/username", adminHandler.UpdateUsername)
	admin.Post("/users", adminHandler.RegisterUser)
	admin.Get("/stats", adminHandler.ServerStats)
	admin.Get("/server-version", adminHandler.ServerVersion)
	admin.Get("/server-time", adminHandler.ServerTime)
	app.Get("/advance", handler.AdminOnly, adminHandler.AdvancePage)

	// 自助端点 (任何经过身份验证的 user)
	app.Put("/api/update-password", handler.UpdatePasswordSelf(db))

	// 找到可用的端口并启动 server
	var ln net.Listener
	startPort := cfg.Port
	for {
		addr := fmt.Sprintf(":%d", cfg.Port)
		l, err := net.Listen("tcp", addr)
		if err == nil {
			ln = l
			break
		}
		slog.Warn("端口已被占用，正在尝试下一个端口", "port", cfg.Port)
		cfg.Port++
		if cfg.Port > startPort+100 {
			slog.Error("在范围内无法找到可用端口", "start", startPort)
			os.Exit(1)
		}
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port
	slog.Info("🚀 ClipCascade 服务正在启动", "port", actualPort)

	// 显示所有可用的本地 IP 地址
	fmt.Printf("\n  ClipCascade Server v2 dev server running at:\n\n")
	fmt.Printf("  ➜  Local:   http://localhost:%d/\n", actualPort)
	ips := getLocalIPs()
	for _, ip := range ips {
		fmt.Printf("  ➜  Network: http://%s:%d/\n", ip, actualPort)
	}
	fmt.Printf("\n")

	// 启动 mDNS 服务广播
	serverName := fmt.Sprintf("ClipCascade-%d", actualPort)
	mdnsServer, err := zeroconf.Register(serverName, "_clipcascade._tcp", "local.", actualPort, []string{"txtv=1", "app=clipcascade"}, nil)
	if err != nil {
		slog.Warn("注册 mDNS 服务失败", "error", err)
	} else {
		defer mdnsServer.Shutdown()
		slog.Info("📢 局域网发现已激活 (mDNS)", "service", "_clipcascade._tcp", "name", serverName)
	}

	// 优雅关机处理
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		slog.Info("正在关闭服务器...")
		if mdnsServer != nil {
			mdnsServer.Shutdown()
		}
		_ = app.Shutdown()
	}()

	if err := app.Listener(ln); err != nil {
		slog.Error("服务器启动失败", "error", err)
		os.Exit(1)
	}
}

// seedAdminUser 如果不存在 user，则创建默认 admin user。
func seedAdminUser(db *gorm.DB) {
	var count int64
	db.Model(&model.User{}).Count(&count)
	if count > 0 {
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("对默认管理员密码进行哈希处理失败", "error", err)
		return
	}

	admin := model.User{
		Username: "admin",
		Password: string(hashed),
		Role:     "ADMIN",
		Enabled:  true,
	}
	if err := db.Create(&admin).Error; err != nil {
		slog.Error("创建管理员用户失败", "error", err)
		return
	}

	db.Create(&model.UserInfo{
		UserID:     admin.ID,
		Salt:       "admin",
		HashRounds: 100000,
	})

	slog.Info("✅ 默认管理员用户已创建 (admin / admin123)")
}

// getLocalIPs 返回所有非回环 IPv4 地址。
func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

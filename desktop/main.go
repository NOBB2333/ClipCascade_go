// ClipCascade Desktop Client - 一个用 Go 编写的跨平台剪贴板同步 client。
//
// 通过 STOMP-over-WebSocket 连接到 ClipCascade server，监控本地剪贴板，
// 并在不同 devices 之间同步更改。
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/clipcascade/desktop/app"
	"github.com/clipcascade/desktop/config"
)

func main() {
	// 为初始设置解析命令行参数 (flags)
	serverURL := flag.String("server", "", "Server URL (e.g. http://localhost:8080)")
	username := flag.String("username", "", "Login username")
	password := flag.String("password", "", "Login password")
	noE2EE := flag.Bool("no-e2ee", false, "Disable end-to-end encryption")
	saveConfig := flag.Bool("save", false, "Save provided flags to config file")
	debugLog := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// 设置日志
	level := slog.LevelInfo
	if *debugLog {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})))

	// 加载 config
	cfg := config.Load()

	// 如果提供了 flags，则进行覆盖
	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}
	if *username != "" {
		cfg.Username = *username
	}
	if *password != "" {
		cfg.Password = *password
	}
	if *noE2EE {
		cfg.E2EEEnabled = false
	}

	// 如果有请求，则保存 config
	if *saveConfig {
		if err := cfg.Save(); err != nil {
			slog.Error("failed to save config", "error", err)
			os.Exit(1)
		}
		fmt.Println("✅ Config saved to:", cfg.FilePath)
	}

	// 如果没有凭据，打印帮助并自动创建默认 config，但仍然启动 tray
	if cfg.Username == "" || cfg.Password == "" {
		cfgPath := config.ConfigDir() + "/config.json"
		fmt.Println("ClipCascade Desktop Client")
		fmt.Println()
		fmt.Println("⚠️  No credentials configured. The tray icon will start, but")
		fmt.Println("   you need to configure credentials before connecting.")
		fmt.Println()
		fmt.Println("Quick start:")
		fmt.Println("  clipcascade-desktop --server http://localhost:8080 --username admin --password admin123 --save")
		fmt.Println()
		fmt.Println("Or edit config file at:", cfgPath)
		fmt.Println()

		// 如果默认 config 不存在，则自动创建它
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			_ = cfg.Save()
			fmt.Println("📝 Default config created at:", cfgPath)
		}

		fmt.Println("🔲 正在启动系统 tray...")
	}

	slog.Info("starting ClipCascade desktop client",
		"server", cfg.ServerURL,
		"user", cfg.Username,
		"e2ee", cfg.E2EEEnabled,
	)

	// 创建并运行 application (在通过 tray 退出前保持阻塞)
	application := app.New(cfg)
	application.Run()
}

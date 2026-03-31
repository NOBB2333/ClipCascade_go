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
	"strings"

	"github.com/clipcascade/desktop/app"
	"github.com/clipcascade/desktop/config"
)

func main() {
	// 为初始设置解析命令行参数 (flags)
	serverURL := flag.String("server", "", "Server URL (e.g. http://localhost:8080)")
	username := flag.String("username", "", "Login username")
	password := flag.String("password", "", "Login password")
	noE2EE := flag.Bool("no-e2ee", false, "Disable end-to-end encryption")
	sendFilter := flag.String("send-filter", "all", "One-shot send filter: all|none|text|image|file or comma list like text,file")
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
		cfg.ServerURL = config.NormalizeServerURL(*serverURL)
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

	sendText, sendImage, sendFile, err := parseSendFilter(*sendFilter)
	if err != nil {
		slog.Error("invalid --send-filter", "value", *sendFilter, "error", err)
		os.Exit(2)
	}
	cfg.SendText = sendText
	cfg.SendImage = sendImage
	cfg.SendFile = sendFile

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
		"send_text", cfg.SendText,
		"send_image", cfg.SendImage,
		"send_file", cfg.SendFile,
	)

	// 创建并运行 application (在通过 tray 退出前保持阻塞)
	application := app.New(cfg)
	application.Run()
}

func parseSendFilter(raw string) (sendText bool, sendImage bool, sendFile bool, err error) {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" || v == "all" {
		return true, true, true, nil
	}
	if v == "none" {
		return false, false, false, nil
	}

	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		seen[token] = true
	}
	if len(seen) == 0 {
		return false, false, false, fmt.Errorf("empty filter")
	}
	for token := range seen {
		switch token {
		case "text":
		case "image":
		case "file":
		default:
			return false, false, false, fmt.Errorf("unsupported token %q", token)
		}
	}
	return seen["text"], seen["image"], seen["file"], nil
}

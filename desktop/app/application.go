// Package app 管理 desktop client 生命周期：login、connect、监控、reconnect。
package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/clipcascade/pkg/constants"
	pkgcrypto "github.com/clipcascade/pkg/crypto"
	"github.com/clipcascade/pkg/protocol"

	"github.com/clipcascade/desktop/clipboard"
	"github.com/clipcascade/desktop/config"
	"github.com/clipcascade/desktop/transport"
	"github.com/clipcascade/desktop/ui"
	"github.com/grandcat/zeroconf"
)

// Application 是主要的 desktop client 控制器。
type Application struct {
	cfg        *config.Config
	httpClient *http.Client
	stomp      *transport.StompClient
	p2p        *transport.P2PClient
	clip       *clipboard.Manager
	tray       *ui.Tray
	ctx        context.Context
	cancel     context.CancelFunc
	encKey     []byte // 从 password 派生的 AES-256-GCM 密钥
	reconnecting bool
	connecting   bool // 防止用户重复点击连接产生并发泄漏
	connMu       sync.Mutex

	lastRecvMu   sync.Mutex
	lastRecvHash string
	lastRecvTime time.Time
}

// New 创建一个新的 Application 实例。
func New(cfg *config.Config) *Application {
	ctx, cancel := context.WithCancel(context.Background())
	jar, _ := cookiejar.New(nil)

	return &Application{
		cfg:  cfg,
		clip: clipboard.NewManager(),
		tray: ui.NewTray(),
		ctx:  ctx,
		cancel: cancel,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // 不要跟随重定向
			},
		},
	}
}

// Run 启动 application。在 macOS 上必须从 main goroutine 调用。
func (a *Application) Run() {
	// 设置 tray 回调
	a.tray.OnConnect(func() {
		go a.connect()
	})
	a.tray.OnDisconnect(func() {
		a.disconnect()
	})
	a.tray.OnQuit(func() {
		a.shutdown()
	})

	// 如果配置了凭据，则自动连接
	if a.cfg.Username != "" && a.cfg.Password != "" {
		go a.connect()
	}

	// Run tray (blocks until quit)
	a.tray.Run()
}

// discoverServer 尝试在局域网中通过 mDNS 查找 ClipCascade server。
func (a *Application) discoverServer() (string, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return "", err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()

	err = resolver.Browse(ctx, "_clipcascade._tcp", "local.", entries)
	if err != nil {
		return "", err
	}

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("server discovery timed out")
		case entry := <-entries:
			if entry != nil && len(entry.AddrIPv4) > 0 {
				addr := fmt.Sprintf("http://%s:%d", entry.AddrIPv4[0], entry.Port)
				slog.Info("应用：通过 mDNS 发现服务器", "名称", entry.Instance, "地址", addr)
				return addr, nil
			}
		}
	}
}

// connect 执行 login → 获取加密密钥 → 启动 WebSocket → 开始剪贴板监控。
func (a *Application) connect() {
	a.connMu.Lock()
	if a.connecting || (a.stomp != nil && a.stomp.IsConnected()) {
		a.connMu.Unlock()
		slog.Info("应用：已在连接中或已连接，忽略重复请求")
		return
	}
	a.connecting = true
	a.connMu.Unlock()

	defer func() {
		a.connMu.Lock()
		a.connecting = false
		a.connMu.Unlock()
	}()

	a.tray.SetStatus("Connecting...")

	// 如果未配置 ServerURL，或者使用的是 localhost（可能被占用），尝试自动发现
	if a.cfg.ServerURL == "" || strings.Contains(a.cfg.ServerURL, "localhost") {
		slog.Info("应用：正在尝试局域网服务器发现...")
		discovered, err := a.discoverServer()
		if err == nil {
			a.cfg.ServerURL = discovered
			slog.Info("应用：自动发现成功", "URL", discovered)
		} else {
			slog.Warn("应用：自动发现失败，将使用默认配置", "错误", err, "URL", a.cfg.ServerURL)
		}
	}

	// 步骤 1: HTTP login
	cookies, err := a.login()
	if err != nil {
		slog.Error("登录失败", "错误", err)
		a.tray.SetStatus("Login Failed")
		ui.Notify("ClipCascade", "Login failed: "+err.Error())
		return
	}

	// 步骤 2: 获取用于加密密钥派生的 user 信息技巧。
	if a.cfg.E2EEEnabled {
		if err := a.deriveEncryptionKey(); err != nil {
			slog.Error("密钥派生失败", "错误", err)
			a.tray.SetStatus("Key Error")
			return
		}
	}

	// Step 3: Connect WebSocket STOMP
	a.stomp = transport.NewStompClient(a.cfg.ServerURL, cookies)
	a.stomp.OnMessage(a.onReceive)

	if err := a.stomp.Connect(); err != nil {
		slog.Error("WebSocket 连接失败", "错误", err)
		a.tray.SetStatus("WS Failed")
		ui.Notify("ClipCascade", "WebSocket connection failed")
		go a.reconnectLoop()
		return
	}

	// Step 4: Connect P2P if enabled
	if a.cfg.P2PEnabled {
		stunURL := constants.DefaultStunURL
		if a.cfg.StunURL != "" {
			stunURL = a.cfg.StunURL
		}
		a.p2p = transport.NewP2PClient(a.cfg.ServerURL, cookies, stunURL)
		a.p2p.OnReceive(a.onReceive)
		if err := a.p2p.Connect(); err != nil {
			slog.Warn("应用：P2P 连接失败", "错误", err)
		}
	}

	// Step 5: Initialize clipboard monitoring
	if err := a.clip.Init(); err != nil {
		slog.Error("clipboard init failed", "error", err)
		a.tray.SetStatus("Clipboard Error")
		return
	}

	a.clip.OnCopy(a.onCopy)
	a.clip.Watch(a.ctx)

	a.tray.SetStatus("Connected ✓")
	ui.Notify("ClipCascade", "Connected to server as "+a.cfg.Username)
	slog.Info("应用：已连接并开始监控剪贴板")

	// Start reconnect monitor
	go a.monitorConnection()
}

// login 执行基于 HTTP 表单的 login 并返回 session cookies。
func (a *Application) login() ([]*http.Cookie, error) {
	loginURL := a.cfg.ServerURL + "/login"

	form := url.Values{
		"username": {a.cfg.Username},
		"password": {a.cfg.Password},
	}

	resp, err := a.httpClient.PostForm(loginURL, form)
	if err != nil {
		return nil, fmt.Errorf("POST /login: %w", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	// Check for redirect to "/" (success) vs "/login?error" (failure)
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		location := resp.Header.Get("Location")
		if strings.Contains(location, "error") {
			return nil, fmt.Errorf("invalid credentials")
		}
	}

	// Extract cookies from jar
	u, _ := url.Parse(a.cfg.ServerURL)
	cookies := a.httpClient.Jar.Cookies(u)

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no session cookie received")
	}

	slog.Info("应用：已登录", "用户名", a.cfg.Username, "Cookie 数量", len(cookies))
	return cookies, nil
}

// deriveEncryptionKey 从 server 获取 user 信息并在本地派生 AES 密钥。
func (a *Application) deriveEncryptionKey() error {
	resp, err := a.httpClient.Get(a.cfg.ServerURL + "/api/user-info")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var info struct {
		Salt       string `json:"salt"`
		HashRounds int    `json:"hash_rounds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return err
	}

	a.encKey = pkgcrypto.DeriveKey(a.cfg.Password, a.cfg.Username, info.Salt, info.HashRounds)
	slog.Info("应用：加密密钥已派生", "循环次数", info.HashRounds)
	return nil
}

// onCopy 在本地剪贴板更改时被调用。发送到 server。
func (a *Application) onCopy(payload string, payloadType string, filename string) {
	if a.stomp == nil || !a.stomp.IsConnected() {
		return
	}

	// Build ClipboardData
	clipData := &protocol.ClipboardData{
		Payload:  payload,
		Type:     payloadType,
		FileName: filename,
	}

	// Encrypt if E2EE enabled
	var body string
	if a.cfg.E2EEEnabled && a.encKey != nil {
		jsonBytes, _ := clipData.Encode()
		encrypted, err := pkgcrypto.Encrypt(a.encKey, jsonBytes)
		if err != nil {
			slog.Error("加密失败", "错误", err)
			return
		}
		body, _ = pkgcrypto.EncodeToJSONString(encrypted)
	} else {
		jsonBytes, _ := clipData.Encode()
		body = string(jsonBytes)
	}

	if err := a.stomp.Send(body); err != nil {
		slog.Error("发送失败", "错误", err)
	}

	// 也通过 P2P 发送（如果可用）
	if a.p2p != nil {
		a.p2p.Send(body)
	}
}

// onReceive 当从 server 接收到剪贴板消息时被调用。
func (a *Application) onReceive(body string) {
	// 双通道并发跨线兜底消重 (针对 P2P 瞬间与 STOMP 服务器并发投递同一套 payload 的场景)
	a.lastRecvMu.Lock()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	now := time.Now()
	// 如果在 5 秒内收到完全相同的加密/明文字符串，安全地认为是通道冗余并抛弃
	if a.lastRecvHash == hash && now.Sub(a.lastRecvTime) < 5*time.Second {
		a.lastRecvMu.Unlock()
		slog.Debug("应用：静默丢弃重复荷载（P2P/STOMP 双通道并发兜底保护）")
		return
	}
	a.lastRecvHash = hash
	a.lastRecvTime = now
	a.lastRecvMu.Unlock()

	var clipData *protocol.ClipboardData

	if a.cfg.E2EEEnabled && a.encKey != nil {
		// Decrypt
		encrypted, err := pkgcrypto.DecodeFromJSONString(body)
		if err != nil {
			slog.Warn("解密解析失败", "错误", err)
			return
		}
		plaintext, err := pkgcrypto.Decrypt(a.encKey, encrypted)
		if err != nil {
			slog.Warn("解密失败", "错误", err)
			return
		}
		clipData, err = protocol.DecodeClipboardData(plaintext)
		if err != nil {
			slog.Warn("剪贴板数据解码失败", "错误", err)
			return
		}
	} else {
		var err error
		clipData, err = protocol.DecodeClipboardData([]byte(body))
		if err != nil {
			slog.Warn("剪贴板数据解码失败", "错误", err)
			return
		}
	}

	// Paste to local clipboard
	a.clip.Paste(clipData.Payload, clipData.Type, clipData.FileName)
	slog.Debug("应用：已接收并粘贴", "类型", clipData.Type, "大小", len(clipData.Payload))
}

// monitorConnection 检查连接健康状况并触发重连。
func (a *Application) monitorConnection() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.stomp != nil && !a.stomp.IsConnected() && !a.reconnecting {
				slog.Warn("应用：连接丢失，正在触发重连")
				go a.reconnectLoop()
			}
		}
	}
}

// reconnectLoop tries to reconnect with exponential backoff and jitter.
func (a *Application) reconnectLoop() {
	if !a.cfg.AutoReconnect || a.reconnecting {
		return
	}
	a.reconnecting = true
	defer func() { a.reconnecting = false }()

	a.tray.SetStatus("Reconnecting...")

	delay := time.Duration(a.cfg.ReconnectDelay) * time.Second
	if delay == 0 {
		delay = time.Duration(constants.DefaultReconnectDelay) * time.Second
	}
	maxDelay := time.Duration(constants.MaxReconnectDelay) * time.Second

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(delay):
			slog.Info("应用：正在尝试重连", "延迟", delay)
			a.connect()
			if a.stomp != nil && a.stomp.IsConnected() {
				return // reconnected
			}
			delay = min(delay*2, maxDelay)
		}
	}
}

// disconnect disconnects from the server.
func (a *Application) disconnect() {
	if a.stomp != nil {
		a.stomp.Close()
		a.stomp = nil
	}
	if a.p2p != nil {
		a.p2p.Close()
		a.p2p = nil
	}
	a.tray.SetStatus("Disconnected")
	ui.Notify("ClipCascade", "Disconnected from server")
	slog.Info("应用：已断开连接")
}

// shutdown cleanly shuts down the application.
func (a *Application) shutdown() {
	a.disconnect()
	a.cancel()
	slog.Info("应用：正在关闭")
}

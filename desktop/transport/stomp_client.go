// Package transport 提供用于与 ClipCascade server 通信的 STOMP-over-WebSocket client。
package transport

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/clipcascade/pkg/protocol"
)

// StompClient 管理到 server 的 STOMP-over-WebSocket 连接。
type StompClient struct {
	serverURL    string
	cookies      []*http.Cookie
	conn         *websocket.Conn
	mu           sync.Mutex
	done         chan struct{}
	onMessage    func(body string) // 接收到的剪贴板数据的回调
	subscribed   bool
	reconnecting bool
}

// NewStompClient 创建一个新的 STOMP client。
func NewStompClient(serverURL string, cookies []*http.Cookie) *StompClient {
	return &StompClient{
		serverURL: serverURL,
		cookies:   cookies,
		done:      make(chan struct{}),
	}
}

// OnMessage 设置传入剪贴板消息的回调。
func (sc *StompClient) OnMessage(fn func(body string)) {
	sc.onMessage = fn
}

// Connect 建立 WebSocket 连接并执行 STOMP 握手。
func (sc *StompClient) Connect() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// 从 server URL 构建 WebSocket URL
	wsURL, err := sc.buildWSURL()
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	// 使用 cookies 创建用于 session auth 的 dialer
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	header := http.Header{}
	for _, c := range sc.cookies {
		header.Add("Cookie", c.String())
	}

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	sc.conn = conn

	// STOMP CONNECT 握手
	connectFrame := protocol.ConnectFrame("1.1", "localhost")
	if err := conn.WriteMessage(websocket.TextMessage, connectFrame.Encode()); err != nil {
		conn.Close()
		return fmt.Errorf("STOMP CONNECT: %w", err)
	}

	// 读取 CONNECTED 响应
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("reading CONNECTED: %w", err)
	}

	frame, err := protocol.ParseFrame(msg)
	if err != nil || frame.Command != "CONNECTED" {
		conn.Close()
		return fmt.Errorf("expected CONNECTED, got: %s", string(msg))
	}

	// SUBSCRIBE 到 user 的剪贴板队列
	subFrame := protocol.SubscribeFrame("sub-0", "/user/queue/cliptext")
	if err := conn.WriteMessage(websocket.TextMessage, subFrame.Encode()); err != nil {
		conn.Close()
		return fmt.Errorf("STOMP SUBSCRIBE: %w", err)
	}

	sc.subscribed = true
	slog.Info("stomp: connected and subscribed")

	// 启动消息读取器 goroutine
	go sc.readLoop()

	return nil
}

// Send 通过 STOMP SEND 帧向 server 发送剪贴板数据。
func (sc *StompClient) Send(body string) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.conn == nil {
		return fmt.Errorf("not connected")
	}

	sendFrame := protocol.SendFrame("/app/cliptext", body)
	return sc.conn.WriteMessage(websocket.TextMessage, sendFrame.Encode())
}

// Close 优雅地断开连接。
func (sc *StompClient) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	select {
	case <-sc.done:
	default:
		close(sc.done)
	}

	if sc.conn != nil {
		// 发送 STOMP DISCONNECT
		disc := protocol.NewFrame("DISCONNECT")
		_ = sc.conn.WriteMessage(websocket.TextMessage, disc.Encode())
		sc.conn.Close()
		sc.conn = nil
	}
	sc.subscribed = false
}

// IsConnected 如果已连接并订阅，则返回 true。
func (sc *StompClient) IsConnected() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn != nil && sc.subscribed
}

// readLoop 读取传入的 STOMP MESSAGE 帧并调用 onMessage。
func (sc *StompClient) readLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	defer func() {
		sc.mu.Lock()
		sc.subscribed = false
		sc.mu.Unlock()
		// 触发外部可能的重新连接逻辑 (通过关闭 conn 让其他地方检测到)
		if sc.conn != nil {
			sc.conn.Close()
		}
	}()

	// 启动心跳写入器
	go func() {
		for {
			select {
			case <-sc.done:
				return
			case <-ticker.C:
				sc.mu.Lock()
				if sc.conn != nil {
					// 发送 WebSocket Ping 帧作为应用层心跳
					err := sc.conn.WriteMessage(websocket.PingMessage, nil)
					if err != nil {
						slog.Warn("stomp: heartbeat ping error", "error", err)
					}
				}
				sc.mu.Unlock()
			}
		}
	}()

	for {
		select {
		case <-sc.done:
			return
		default:
		}

		// 设置读取截止时间以防止死连接一直阻塞
		sc.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, msg, err := sc.conn.ReadMessage()
		if err != nil {
			slog.Warn("stomp: read error (connection might be lost)", "error", err)
			return
		}

		frame, err := protocol.ParseFrame(msg)
		if err != nil {
			slog.Warn("stomp: invalid frame", "error", err)
			continue
		}

		if frame.Command == "MESSAGE" && sc.onMessage != nil {
			sc.onMessage(frame.Body)
		}
	}
}

// buildWSURL 将 http(s)://host:port 转换为 ws(s)://host:port/clipsocket
func (sc *StompClient) buildWSURL() (string, error) {
	u, err := url.Parse(sc.serverURL)
	if err != nil {
		return "", err
	}

	scheme := "ws"
	if strings.HasPrefix(u.Scheme, "https") {
		scheme = "wss"
	}

	return fmt.Sprintf("%s://%s/clipsocket", scheme, u.Host), nil
}

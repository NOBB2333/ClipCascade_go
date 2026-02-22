// Package transport 提供专为 mobile 使用而适配的 STOMP WebSocket client。
// 本 package 包装了共享的 protocol package，并具有 mobile 友好的错误处理
// 和连接生命周期管理。
package transport

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/clipcascade/pkg/protocol"
)

// Client 是针对 mobile 优化的 STOMP-over-WebSocket client。
type Client struct {
	mu           sync.Mutex
	conn         *websocket.Conn
	done         chan struct{}
	connected    bool
	onMessage    func(body string)
	onDisconnect func()
}

// NewClient 创建一个新的 mobile transport client。
func NewClient() *Client {
	return &Client{
		done: make(chan struct{}),
	}
}

// OnMessage 为传入的 STOMP MESSAGE 帧设置处理程序。
func (c *Client) OnMessage(fn func(body string)) {
	c.onMessage = fn
}

// OnDisconnect 为连接丢失事件设置处理程序。
func (c *Client) OnDisconnect(fn func()) {
	c.onDisconnect = fn
}

// Connect 建立 WebSocket 并执行 STOMP 握手。
func (c *Client) Connect(serverURL string, cookies []*http.Cookie) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	wsURL, err := toWSURL(serverURL)
	if err != nil {
		return err
	}

	header := http.Header{}
	for _, cookie := range cookies {
		header.Add("Cookie", cookie.String())
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.conn = conn

	// STOMP CONNECT
	if err := conn.WriteMessage(websocket.TextMessage, protocol.ConnectFrame("1.1", "localhost").Encode()); err != nil {
		conn.Close()
		return fmt.Errorf("STOMP CONNECT: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("reading CONNECTED: %w", err)
	}
	frame, _ := protocol.ParseFrame(msg)
	if frame == nil || frame.Command != "CONNECTED" {
		conn.Close()
		return fmt.Errorf("unexpected response: %s", string(msg))
	}

	// SUBSCRIBE
	if err := conn.WriteMessage(websocket.TextMessage,
		protocol.SubscribeFrame("sub-0", "/user/queue/cliptext").Encode()); err != nil {
		conn.Close()
		return fmt.Errorf("SUBSCRIBE: %w", err)
	}

	c.connected = true
	go c.readLoop()
	return nil
}

// Send 发送包含给定 body 的 STOMP SEND 帧。
func (c *Client) Send(body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.WriteMessage(websocket.TextMessage,
		protocol.SendFrame("/app/cliptext", body).Encode())
}

// Close 优雅地断开连接。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.done:
	default:
		close(c.done)
	}

	if c.conn != nil {
		c.conn.WriteMessage(websocket.TextMessage, protocol.NewFrame("DISCONNECT").Encode())
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
}

// IsConnected 返回连接状态。
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *Client) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			log.Println("mobile/transport: read error:", err)
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			if c.onDisconnect != nil {
				c.onDisconnect()
			}
			return
		}

		frame, err := protocol.ParseFrame(msg)
		if err != nil {
			continue
		}
		if frame.Command == "MESSAGE" && c.onMessage != nil {
			c.onMessage(frame.Body)
		}
	}
}

func toWSURL(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	scheme := "ws"
	if strings.HasPrefix(u.Scheme, "https") {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s/clipsocket", scheme, u.Host), nil
}

package handler

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gofiber/contrib/websocket"

	"github.com/clipcascade/pkg/protocol"
	"github.com/clipcascade/pkg/sizefmt"
)

// WSHub 管理所有活动的 WebSocket 连接并在 users 之间路由 STOMP 消息。
// 每个 user 可以有多个连接（即多个 devices）；当一个 device 发送剪贴板数据时，
// 同一个 user 的所有其他 devices 都会收到该数据。
type WSHub struct {
	// mu 保护 connections map。
	mu sync.RWMutex
	// connections 映射 username → *websocket.Conn 集合
	connections map[string]map[*websocket.Conn]bool
	// writeLocks 避免同一连接被并发写导致 gorilla/websocket panic。
	writeLocks map[*websocket.Conn]*sync.Mutex

	// Stats
	totalConnections  atomic.Int64 // 累计总量
	totalInboundMsgs  atomic.Int64
	totalOutboundMsgs atomic.Int64
}

// NewWSHub 创建一个新的 WebSocket 连接 hub。
func NewWSHub() *WSHub {
	return &WSHub{
		connections: make(map[string]map[*websocket.Conn]bool),
		writeLocks:  make(map[*websocket.Conn]*sync.Mutex),
	}
}

// Register 为给定的 username 添加一个连接。
func (h *WSHub) Register(username string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.connections[username] == nil {
		h.connections[username] = make(map[*websocket.Conn]bool)
	}
	h.connections[username][conn] = true
	h.writeLocks[conn] = &sync.Mutex{}
	h.totalConnections.Add(1)
	slog.Info("WS：客户端已连接", "用户名", username, "IP", conn.RemoteAddr().String(), "当前活动连接数", len(h.connections[username]))
}

// Unregister 为给定的 username 移除一个连接。
func (h *WSHub) Unregister(username string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conns, ok := h.connections[username]; ok {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.connections, username)
		}
	}
	delete(h.writeLocks, conn)
	slog.Info("WS：客户端已断开", "用户名", username, "IP", conn.RemoteAddr().String())
}

// Broadcast 向给定 user 的所有连接发送消息，但 sender 连接除外。
// 这实现了“同步到我的所有其他 devices”的逻辑。
func (h *WSHub) Broadcast(username string, sender *websocket.Conn, data []byte) {
	h.mu.RLock()
	conns, ok := h.connections[username]
	if !ok {
		h.mu.RUnlock()
		return
	}
	type target struct {
		conn *websocket.Conn
		mu   *sync.Mutex
	}
	targets := make([]target, 0, len(conns))
	for conn := range conns {
		if conn == sender {
			continue // 不要回显给 sender
		}
		targets = append(targets, target{
			conn: conn,
			mu:   h.writeLocks[conn],
		})
	}
	h.mu.RUnlock()

	for _, t := range targets {
		h.totalOutboundMsgs.Add(1)
		if t.mu == nil {
			continue
		}
		t.mu.Lock()
		err := t.conn.WriteMessage(websocket.TextMessage, data)
		t.mu.Unlock()
		if err != nil {
			slog.Warn("WS：写入错误", "用户名", username, "错误", err)
		}
	}
}

// HandleWebSocket 是 Fiber WebSocket 升级 handler。
// 它处理类似 STOMP 的协议流程：
//  1. Client 发送 CONNECT → server 回复 CONNECTED
//  2. Client 发送 SUBSCRIBE → server 确认
//  3. Client 发送带有剪贴板数据的 SEND → server 广播给 user 的其他 devices
func (h *WSHub) HandleWebSocket(c *websocket.Conn) {
	// 从 locals 中提取 username (在升级前的 auth 中间件设置)
	username, _ := c.Locals("username").(string)
	if username == "" {
		slog.Warn("WS：连接中缺少用户名，正在关闭")
		c.Close()
		return
	}

	h.Register(username, c)
	defer h.Unregister(username, c)
	defer c.Close()

	subscriptionID := ""

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			// 正常关闭或错误
			break
		}

		h.totalInboundMsgs.Add(1)

		frame, err := protocol.ParseFrame(msg)
		if err != nil {
			slog.Warn("WS：无效的 STOMP 帧", "错误", err)
			continue
		}

		switch frame.Command {
		case "CONNECT":
			// 回复 CONNECTED
			resp := protocol.ConnectedFrame("1.1")
			c.WriteMessage(websocket.TextMessage, resp.Encode())

		case "SUBSCRIBE":
			subscriptionID = frame.Get("id")
			slog.Debug("WS：已订阅", "用户名", username, "订阅ID", subscriptionID)

		case "SEND":
			// 从 STOMP body 中解析剪贴板数据
			var clipData protocol.ClipboardData
			if err := json.Unmarshal([]byte(frame.Body), &clipData); err != nil {
				slog.Warn("WS：无效的剪贴板数据", "错误", err)
				continue
			}
			slog.Info(
				"WS：收到剪贴板发送请求",
				"用户名", username,
				"类型", clipData.Type,
				"体积", sizefmt.HumanSizeFromPayload(clipData.Type, clipData.Payload),
			)

			// 包装在 MESSAGE 帧中进行交付
			msgFrame := protocol.MessageFrame(
				frame.Get("destination"),
				subscriptionID,
				"", // message-id (自动)
				frame.Body,
			)
			h.Broadcast(username, c, msgFrame.Encode())

		case "DISCONNECT":
			return

		default:
			slog.Debug("WS：未知命令", "命令", frame.Command)
		}
	}
}

// Stats 返回当前 hub 统计信息。
func (h *WSHub) Stats() map[string]int64 {
	h.mu.RLock()
	activeConns := int64(0)
	for _, conns := range h.connections {
		activeConns += int64(len(conns))
	}
	h.mu.RUnlock()

	return map[string]int64{
		"active_connections":  activeConns,
		"total_connections":   h.totalConnections.Load(),
		"total_inbound_msgs":  h.totalInboundMsgs.Load(),
		"total_outbound_msgs": h.totalOutboundMsgs.Load(),
	}
}

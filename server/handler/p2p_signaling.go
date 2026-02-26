package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/gofiber/contrib/websocket"
)

// P2PSignaling 处理用于 peer-to-peer 剪贴板同步的 WebRTC signaling。
// 它在同一个 user 的 peers 之间路由 offer/answer/ICE candidate 消息。
type P2PSignaling struct {
	mu       sync.RWMutex
	sessions map[string]map[*websocket.Conn]string // username → {conn → sessionID}
	// writeLocks 避免同一连接并发写。
	writeLocks map[*websocket.Conn]*sync.Mutex
}

// NewP2PSignaling 创建一个新的 P2P signaling handler。
func NewP2PSignaling() *P2PSignaling {
	return &P2PSignaling{
		sessions:   make(map[string]map[*websocket.Conn]string),
		writeLocks: make(map[*websocket.Conn]*sync.Mutex),
	}
}

// SignalMessage 代表一个 WebRTC signaling 消息。
type SignalMessage struct {
	Type      string          `json:"type"`       // "offer", "answer", "ice-candidate", "peer-list"
	From      string          `json:"from"`       // sender session ID
	To        string          `json:"to"`         // 目标 session ID (广播则为空)
	SessionID string          `json:"session_id"` // 此连接的 session ID
	Data      json.RawMessage `json:"data"`       // SDP 或 ICE candidate 数据
}

// HandleP2P 是 P2P signaling 连接的 WebSocket handler。
func (p *P2PSignaling) HandleP2P(c *websocket.Conn) {
	username, _ := c.Locals("username").(string)
	if username == "" {
		c.Close()
		return
	}

	sessionID := generateSessionID()

	// 注册此连接
	p.mu.Lock()
	if p.sessions[username] == nil {
		p.sessions[username] = make(map[*websocket.Conn]string)
	}
	p.sessions[username][c] = sessionID
	p.writeLocks[c] = &sync.Mutex{}
	p.mu.Unlock()

	slog.Info("P2P：节点已连接", "用户名", username, "会话ID", sessionID, "IP", c.RemoteAddr().String())

	// 将 session ID 发送给新的 peer
	initMsg, _ := json.Marshal(SignalMessage{
		Type:      "session-id",
		SessionID: sessionID,
	})
	c.WriteMessage(websocket.TextMessage, initMsg)

	// 广播更新后的 peer 列表
	p.broadcastPeerList(username)

	defer func() {
		p.mu.Lock()
		if conns, ok := p.sessions[username]; ok {
			delete(conns, c)
			if len(conns) == 0 {
				delete(p.sessions, username)
			}
		}
		delete(p.writeLocks, c)
		p.mu.Unlock()
		c.Close()
		p.broadcastPeerList(username)
		slog.Info("P2P：节点已断开", "用户名", username, "会话ID", sessionID, "IP", c.RemoteAddr().String())
	}()

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}

		var signal SignalMessage
		if err := json.Unmarshal(msg, &signal); err != nil {
			slog.Warn("P2P：无效的信令消息", "错误", err)
			continue
		}
		signal.From = sessionID

		// 将消息路由到目标 peer
		p.routeMessage(username, c, &signal)
	}
}

// routeMessage 将 signal 发送到目标 peer 或广播到所有 peers。
func (p *P2PSignaling) routeMessage(username string, sender *websocket.Conn, signal *SignalMessage) {
	p.mu.RLock()
	conns, ok := p.sessions[username]
	if !ok {
		p.mu.RUnlock()
		return
	}

	type target struct {
		conn *websocket.Conn
		mu   *sync.Mutex
	}
	targets := make([]target, 0, len(conns))
	data, _ := json.Marshal(signal)

	if signal.To != "" {
		// 定向消息：查找特定的 peer
		for conn, sid := range conns {
			if sid == signal.To && conn != sender {
				targets = append(targets, target{
					conn: conn,
					mu:   p.writeLocks[conn],
				})
				break
			}
		}
	} else {
		// 广播给除 sender 外的所有 peers
		for conn := range conns {
			if conn != sender {
				targets = append(targets, target{
					conn: conn,
					mu:   p.writeLocks[conn],
				})
			}
		}
	}
	p.mu.RUnlock()

	for _, t := range targets {
		if t.mu == nil {
			continue
		}
		t.mu.Lock()
		err := t.conn.WriteMessage(websocket.TextMessage, data)
		t.mu.Unlock()
		if err != nil {
			slog.Warn("P2P：写入信令失败", "错误", err)
		}
	}
}

// broadcastPeerList 将当前 peer session ID 列表发送给一个 user 的所有 peers。
func (p *P2PSignaling) broadcastPeerList(username string) {
	p.mu.RLock()
	conns, ok := p.sessions[username]
	if !ok {
		p.mu.RUnlock()
		return
	}

	// 收集 session IDs
	peers := make([]string, 0, len(conns))
	for _, sid := range conns {
		peers = append(peers, sid)
	}

	peersJSON, _ := json.Marshal(peers)
	msg, _ := json.Marshal(SignalMessage{
		Type: "peer-list",
		Data: peersJSON,
	})

	type target struct {
		conn *websocket.Conn
		mu   *sync.Mutex
	}
	targets := make([]target, 0, len(conns))
	for conn := range conns {
		targets = append(targets, target{
			conn: conn,
			mu:   p.writeLocks[conn],
		})
	}
	p.mu.RUnlock()

	for _, t := range targets {
		if t.mu == nil {
			continue
		}
		t.mu.Lock()
		err := t.conn.WriteMessage(websocket.TextMessage, msg)
		t.mu.Unlock()
		if err != nil {
			slog.Warn("P2P：广播 peer-list 失败", "错误", err)
		}
	}
}

// Stats 返回 P2P 连接统计信息。
func (p *P2PSignaling) Stats() map[string]int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	totalPeers := 0
	for _, conns := range p.sessions {
		totalPeers += len(conns)
	}
	return map[string]int{
		"active_users": len(p.sessions),
		"active_peers": totalPeers,
	}
}

func generateSessionID() string {
	// 12 bytes => 24 hex chars，足够用于会话标识且不可预测。
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "peer-fallback"
	}
	return "peer-" + hex.EncodeToString(b)
}

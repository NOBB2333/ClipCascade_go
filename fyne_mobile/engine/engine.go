// Package engine 为 mobile 平台提供 STOMP 客户端引擎。
//
// 此 package 中的函数和类型必须遵循 gomobile 规则：
//   - 仅包含带有导出方法的导出类型
//   - 支持的参数类型：int, float, string, bool, []byte, error
//   - 不得使用 channels、maps 或复杂的 generics
package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	pkgcrypto "github.com/clipcascade/pkg/crypto"
	"github.com/clipcascade/pkg/protocol"
)

// MessageCallback 是 mobile 平台必须实现的接口，
// 用于从 Go engine 接收剪贴板数据。
type MessageCallback interface {
	// 当从其他 device 接收到剪贴板数据时调用 OnMessage。
	// payloadType 为 "text"、"image" 或 "files"。
	OnMessage(payload string, payloadType string)

	// 当连接状态改变时调用 OnStatusChange。
	// status: "connected"、"disconnected"、"reconnecting"、"error"
	OnStatusChange(status string)
}

// Engine 是用于 mobile 剪贴板同步的主要 Go engine。
type Engine struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	serverURL  string
	username   string
	password   string
	e2ee       bool
	encKey     []byte
	httpClient *http.Client
	wsConn     *websocket.Conn
	callback   MessageCallback
	done       chan struct{}
	connected  bool
}

// NewEngine 创建一个新的 ClipCascade mobile engine。
func NewEngine(serverURL, username, password string, e2eeEnabled bool) *Engine {
	jar, _ := cookiejar.New(nil)
	return &Engine{
		serverURL: serverURL,
		username:  username,
		password:  password,
		e2ee:      e2eeEnabled,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		done: make(chan struct{}),
	}
}

// SetCallback 注册用于接收消息的 mobile 回调。
func (e *Engine) SetCallback(cb MessageCallback) {
	e.callback = cb
}

// Start 连接到 server 并开始监听剪贴板数据。
func (e *Engine) Start() error {
	// Step 1: Login
	if err := e.login(); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Step 2: Derive encryption key if E2EE
	if e.e2ee {
		if err := e.deriveKey(); err != nil {
			return fmt.Errorf("key derivation failed: %w", err)
		}
	}

	// Step 3: Connect WebSocket
	if err := e.connectWS(); err != nil {
		return fmt.Errorf("websocket failed: %w", err)
	}

	// Step 4: Disabling P2P on mobile to bypass Android NDK 1.22+ zoneCache compilation bugs.
	// STOMP relay will handle all syncing perfectly for mobile.

	e.setConnected(true)
	e.notifyStatus("connected")
	go e.readLoop()
	go e.heartbeatLoop()
	return nil
}

// Stop 从 server 断开连接。
func (e *Engine) Stop() {
	var conn *websocket.Conn
	e.mu.Lock()
	select {
	case <-e.done:
	default:
		close(e.done)
	}

	conn = e.wsConn
	e.wsConn = nil
	e.connected = false
	e.mu.Unlock()

	if conn != nil {
		e.writeMu.Lock()
		_ = conn.WriteMessage(websocket.TextMessage, protocol.NewFrame("DISCONNECT").Encode())
		e.writeMu.Unlock()
		_ = conn.Close()
	}
	e.notifyStatus("disconnected")
}

// SendClipboard 向 server 发送剪贴板数据。
func (e *Engine) SendClipboard(payload string, payloadType string) error {
	e.mu.Lock()
	conn := e.wsConn
	encKey := e.encKey
	e2ee := e.e2ee
	e.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	clipData := &protocol.ClipboardData{Payload: payload, Type: payloadType}
	var body string

	if e2ee && encKey != nil {
		jsonBytes, _ := clipData.Encode()
		encrypted, err := pkgcrypto.Encrypt(encKey, jsonBytes)
		if err != nil {
			return err
		}
		body, _ = pkgcrypto.EncodeToJSONString(encrypted)
	} else {
		jsonBytes, _ := clipData.Encode()
		body = string(jsonBytes)
	}

	sendFrame := protocol.SendFrame("/app/cliptext", body)
	e.writeMu.Lock()
	err := conn.WriteMessage(websocket.TextMessage, sendFrame.Encode())
	e.writeMu.Unlock()

	return err
}

// IsConnected 返回 engine 是否已连接。
func (e *Engine) IsConnected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connected
}

// --- Internal methods ---

func (e *Engine) login() error {
	form := url.Values{
		"username": {e.username},
		"password": {e.password},
	}
	resp, err := e.httpClient.PostForm(e.serverURL+"/login", form)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "error") {
			return fmt.Errorf("invalid credentials")
		}
	}
	return nil
}

func (e *Engine) deriveKey() error {
	resp, err := e.httpClient.Get(e.serverURL + "/api/user-info")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var info struct {
		Salt       string `json:"salt"`
		HashRounds int    `json:"hash_rounds"`
	}
	json.NewDecoder(resp.Body).Decode(&info)
	e.encKey = pkgcrypto.DeriveKey(e.password, e.username, info.Salt, info.HashRounds)
	return nil
}

func (e *Engine) connectWS() error {
	u, _ := url.Parse(e.serverURL)
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/clipsocket", scheme, u.Host)

	header := http.Header{}
	for _, c := range e.httpClient.Jar.Cookies(u) {
		header.Add("Cookie", c.String())
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return err
	}

	// STOMP handshake
	conn.WriteMessage(websocket.TextMessage, protocol.ConnectFrame("1.1", "localhost").Encode())
	_, msg, _ := conn.ReadMessage()
	frame, _ := protocol.ParseFrame(msg)
	if frame == nil || frame.Command != "CONNECTED" {
		conn.Close()
		return fmt.Errorf("STOMP handshake failed")
	}

	conn.WriteMessage(websocket.TextMessage, protocol.SubscribeFrame("sub-0", "/user/queue/cliptext").Encode())

	e.mu.Lock()
	old := e.wsConn
	e.wsConn = conn
	e.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (e *Engine) readLoop() {
	for {
		if e.isStopped() {
			return
		}

		e.mu.Lock()
		conn := e.wsConn
		e.mu.Unlock()
		if conn == nil {
			if err := e.reconnectLoop(); err != nil {
				e.notifyStatus("disconnected")
				return
			}
			continue
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("bridge: ws read error:", err)
			if e.isStopped() {
				return
			}
			e.setConnected(false)
			e.notifyStatus("reconnecting")
			if rErr := e.reconnectLoop(); rErr != nil {
				e.notifyStatus("disconnected")
				return
			}
			continue
		}

		frame, err := protocol.ParseFrame(msg)
		if err != nil || frame.Command != "MESSAGE" {
			continue
		}

		e.handleIncomingData(frame.Body)
	}
}

func (e *Engine) reconnectLoop() error {
	backoff := 2 * time.Second
	for {
		if e.isStopped() {
			return fmt.Errorf("stopped")
		}

		if err := e.login(); err == nil {
			if !e.e2ee || e.deriveKey() == nil {
				if err := e.connectWS(); err == nil {
					e.setConnected(true)
					e.notifyStatus("connected")
					return nil
				}
			}
		}

		select {
		case <-e.done:
			return fmt.Errorf("stopped")
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (e *Engine) heartbeatLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			e.mu.Lock()
			conn := e.wsConn
			e.mu.Unlock()
			if conn == nil {
				continue
			}

			// STOMP heartbeat: single LF
			e.writeMu.Lock()
			err := conn.WriteMessage(websocket.TextMessage, []byte("\n"))
			e.writeMu.Unlock()
			if err != nil {
				log.Println("bridge: heartbeat failed:", err)
				_ = conn.Close() // readLoop will detect and reconnect
			}
		}
	}
}

func (e *Engine) setConnected(v bool) {
	e.mu.Lock()
	e.connected = v
	e.mu.Unlock()
}

func (e *Engine) notifyStatus(status string) {
	if e.callback != nil {
		e.callback.OnStatusChange(status)
	}
}

func (e *Engine) isStopped() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

func (e *Engine) handleIncomingData(body string) {
	var clipData *protocol.ClipboardData

	if e.e2ee && e.encKey != nil {
		encrypted, err := pkgcrypto.DecodeFromJSONString(body)
		if err != nil {
			return
		}
		plaintext, err := pkgcrypto.Decrypt(e.encKey, encrypted)
		if err != nil {
			return
		}
		clipData, _ = protocol.DecodeClipboardData(plaintext)
	} else {
		clipData, _ = protocol.DecodeClipboardData([]byte(body))
	}

	if clipData != nil && e.callback != nil {
		e.callback.OnMessage(clipData.Payload, clipData.Type)
	}
}

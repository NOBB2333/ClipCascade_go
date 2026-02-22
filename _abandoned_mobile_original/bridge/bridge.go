// Package bridge 为 mobile 平台提供 gomobile 可导出的接口。
// 这是被编译为 .aar (Android) 和 .xcframework (iOS) 的 Go “engine”。
//
// 此 package 中的函数和类型必须遵循 gomobile 规则：
//   - 仅包含带有导出方法的导出类型
//   - 支持的参数类型：int, float, string, bool, []byte, error
//   - 不得使用 channels、maps 或复杂的 generics
package bridge

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
	"github.com/clipcascade/mobile/transport"
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
	p2p        *transport.P2PClient
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

	// Step 4: Connect P2P if possible
	stunURL := "stun:stun.l.google.com:19302"
	e.p2p = transport.NewP2PClient(stunURL)
	e.p2p.OnReceive(e.handleIncomingData)
	
	// 这里我们需要从 httpClient 的 Jar 中提取 cookies
	u, _ := url.Parse(e.serverURL)
	cookies := e.httpClient.Jar.Cookies(u)
	if err := e.p2p.Connect(e.serverURL, cookies); err != nil {
		log.Println("bridge: p2p connect failed:", err)
	}

	e.connected = true
	if e.callback != nil {
		e.callback.OnStatusChange("connected")
	}
	return nil
}

// Stop 从 server 断开连接。
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	select {
	case <-e.done:
	default:
		close(e.done)
	}

	if e.wsConn != nil {
		disc := protocol.NewFrame("DISCONNECT")
		e.wsConn.WriteMessage(websocket.TextMessage, disc.Encode())
		e.wsConn.Close()
		e.wsConn = nil
	}
	if e.p2p != nil {
		e.p2p.Close()
		e.p2p = nil
	}
	e.connected = false
	if e.callback != nil {
		e.callback.OnStatusChange("disconnected")
	}
}

// SendClipboard 向 server 发送剪贴板数据。
func (e *Engine) SendClipboard(payload string, payloadType string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.wsConn == nil {
		return fmt.Errorf("not connected")
	}

	clipData := &protocol.ClipboardData{Payload: payload, Type: payloadType}
	var body string

	if e.e2ee && e.encKey != nil {
		jsonBytes, _ := clipData.Encode()
		encrypted, err := pkgcrypto.Encrypt(e.encKey, jsonBytes)
		if err != nil {
			return err
		}
		body, _ = pkgcrypto.EncodeToJSONString(encrypted)
	} else {
		jsonBytes, _ := clipData.Encode()
		body = string(jsonBytes)
	}

	sendFrame := protocol.SendFrame("/app/cliptext", body)
	err := e.wsConn.WriteMessage(websocket.TextMessage, sendFrame.Encode())

	// 也尝试通过 P2P 发送
	if e.p2p != nil {
		e.p2p.Send(body)
	}

	return err
}

// IsConnected 返回 engine 是否已连接。
func (e *Engine) IsConnected() bool {
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
	e.wsConn = conn

	// STOMP handshake
	conn.WriteMessage(websocket.TextMessage, protocol.ConnectFrame("1.1", "localhost").Encode())
	_, msg, _ := conn.ReadMessage()
	frame, _ := protocol.ParseFrame(msg)
	if frame == nil || frame.Command != "CONNECTED" {
		conn.Close()
		return fmt.Errorf("STOMP handshake failed")
	}

	conn.WriteMessage(websocket.TextMessage, protocol.SubscribeFrame("sub-0", "/user/queue/cliptext").Encode())

	go e.readLoop()
	return nil
}

func (e *Engine) readLoop() {
	for {
		select {
		case <-e.done:
			return
		default:
		}

		_, msg, err := e.wsConn.ReadMessage()
		if err != nil {
			log.Println("bridge: ws read error:", err)
			e.connected = false
			if e.callback != nil {
				e.callback.OnStatusChange("disconnected")
			}
			return
		}

		frame, err := protocol.ParseFrame(msg)
		if err != nil || frame.Command != "MESSAGE" {
			continue
		}

		e.handleIncomingData(frame.Body)
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

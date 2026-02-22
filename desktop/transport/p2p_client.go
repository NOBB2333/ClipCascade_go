// Package transport 提供 P2P WebRTC DataChannel client，用于
// 设备到设备之间的直接剪贴板同步，绕过 server 进行数据传输。
package transport

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/clipcascade/pkg/constants"
	"github.com/clipcascade/pkg/protocol"
)

// P2PClient 管理用于 P2P 剪贴板同步的 WebRTC peer 连接。
type P2PClient struct {
	serverURL   string
	cookies     []*http.Cookie
	wsConn      *websocket.Conn
	mu          sync.Mutex
	peers       map[string]*webrtc.PeerConnection // sessionID → PeerConnection
	dataChans   map[string]*webrtc.DataChannel     // sessionID → DataChannel
	sessionID   string
	stunURL            string
	onReceive          func(data string)
	receivingFragments map[string][]string // ID -> fragments
	done               chan struct{}
}

// NewP2PClient 创建一个连接到 signaling server 的 P2P client。
func NewP2PClient(serverURL string, cookies []*http.Cookie, stunURL string) *P2PClient {
	return &P2PClient{
		serverURL: serverURL,
		cookies:   cookies,
		stunURL:   stunURL,
		peers:              make(map[string]*webrtc.PeerConnection),
		dataChans:          make(map[string]*webrtc.DataChannel),
		receivingFragments: make(map[string][]string),
		done:               make(chan struct{}),
	}
}

// OnReceive 设置通过 DataChannel 接收到数据时的回调。
func (p *P2PClient) OnReceive(fn func(data string)) {
	p.onReceive = fn
}

// Connect 建立 signaling WebSocket 连接。
func (p *P2PClient) Connect() error {
	wsURL, err := p.buildWSURL()
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{}
	header := http.Header{}
	for _, c := range p.cookies {
		header.Add("Cookie", c.String())
	}

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("p2p signaling dial: %w", err)
	}
	p.wsConn = conn

	go p.signalLoop()

	slog.Info("p2p: signaling connected")
	return nil
}

// Send 通过 DataChannel 向所有已连接的 peers 广播数据。
// 支持自动分片以处理大数据包。
func (p *P2PClient) Send(data string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	fragmentID := uuid.New().String()
	dataBytes := []byte(data)
	totalSize := len(dataBytes)

	// 计算分片
	numFragments := (totalSize + constants.FragmentSize - 1) / constants.FragmentSize
	if numFragments == 0 {
		return
	}

	for i := 0; i < numFragments; i++ {
		start := i * constants.FragmentSize
		end := start + constants.FragmentSize
		if end > totalSize {
			end = totalSize
		}

		clipData := &protocol.ClipboardData{
			Payload: string(dataBytes[start:end]),
			Type:    "text", // 默认类型，实际由调用者决定或在 body 中编码
			Metadata: &protocol.FragmentMetadata{
				ID:             fragmentID,
				Index:          i,
				TotalFragments: numFragments,
				IsFragmented:   numFragments > 1,
			},
		}

		encoded, _ := json.Marshal(clipData)

		for sid, dc := range p.dataChans {
			if dc.ReadyState() == webrtc.DataChannelStateOpen {
				if err := dc.SendText(string(encoded)); err != nil {
					slog.Warn("p2p: send error", "peer", sid, "error", err)
				}
			}
		}
	}
}

// Close 关闭所有 peer 连接和 signaling。
func (p *P2PClient) Close() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.peers {
		pc.Close()
	}
	p.peers = make(map[string]*webrtc.PeerConnection)
	p.dataChans = make(map[string]*webrtc.DataChannel)

	if p.wsConn != nil {
		p.wsConn.Close()
	}
}

// signalLoop 从 WebSocket 读取 signaling 消息。
func (p *P2PClient) signalLoop() {
	for {
		select {
		case <-p.done:
			return
		default:
		}

		_, msg, err := p.wsConn.ReadMessage()
		if err != nil {
			slog.Warn("p2p: signaling read error", "error", err)
			return
		}

		var signal struct {
			Type      string          `json:"type"`
			From      string          `json:"from"`
			To        string          `json:"to"`
			SessionID string          `json:"session_id"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(msg, &signal); err != nil {
			continue
		}

		switch signal.Type {
		case "session-id":
			p.sessionID = signal.SessionID
			slog.Info("p2p: assigned session", "id", p.sessionID)

		case "peer-list":
			var peers []string
			json.Unmarshal(signal.Data, &peers)
			p.handlePeerList(peers)

		case "offer":
			p.handleOffer(signal.From, signal.Data)

		case "answer":
			p.handleAnswer(signal.From, signal.Data)

		case "ice-candidate":
			p.handleICECandidate(signal.From, signal.Data)
		}
	}
}

// handlePeerList 为新 peers 创建 offers。
func (p *P2PClient) handlePeerList(peers []string) {
	for _, peerID := range peers {
		if peerID == p.sessionID {
			continue
		}
		p.mu.Lock()
		_, exists := p.peers[peerID]
		p.mu.Unlock()
		if !exists {
			go p.createOffer(peerID)
		}
	}
}

// createOffer 发起 peer 连接并发送 SDP offer。
func (p *P2PClient) createOffer(peerID string) {
	pc, err := p.newPeerConnection(peerID)
	if err != nil {
		slog.Warn("p2p: create peer connection failed", "error", err)
		return
	}

	// 创建 DataChannel
	dc, err := pc.CreateDataChannel("clipboard", nil)
	if err != nil {
		slog.Warn("p2p: create data channel failed", "error", err)
		return
	}
	p.setupDataChannel(peerID, dc)

	// 创建并设置本地 offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return
	}
	pc.SetLocalDescription(offer)

	// 通过 signaling 发送 offer
	offerJSON, _ := json.Marshal(offer)
	p.sendSignal("offer", peerID, offerJSON)
}

// handleOffer 处理传入的 SDP offer。
func (p *P2PClient) handleOffer(from string, data json.RawMessage) {
	pc, err := p.newPeerConnection(from)
	if err != nil {
		return
	}

	// 处理传入的 DataChannel
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		p.setupDataChannel(from, dc)
	})

	var offer webrtc.SessionDescription
	json.Unmarshal(data, &offer)
	pc.SetRemoteDescription(offer)

	answer, _ := pc.CreateAnswer(nil)
	pc.SetLocalDescription(answer)

	answerJSON, _ := json.Marshal(answer)
	p.sendSignal("answer", from, answerJSON)
}

// handleAnswer 处理传入的 SDP answer。
func (p *P2PClient) handleAnswer(from string, data json.RawMessage) {
	p.mu.Lock()
	pc, ok := p.peers[from]
	p.mu.Unlock()
	if !ok {
		return
	}

	var answer webrtc.SessionDescription
	json.Unmarshal(data, &answer)
	pc.SetRemoteDescription(answer)
}

// handleICECandidate 添加来自 peer 的 ICE candidate。
func (p *P2PClient) handleICECandidate(from string, data json.RawMessage) {
	p.mu.Lock()
	pc, ok := p.peers[from]
	p.mu.Unlock()
	if !ok {
		return
	}

	var candidate webrtc.ICECandidateInit
	json.Unmarshal(data, &candidate)
	pc.AddICECandidate(candidate)
}

// newPeerConnection 使用 STUN 配置创建一个新的 WebRTC PeerConnection。
func (p *P2PClient) newPeerConnection(peerID string) (*webrtc.PeerConnection, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{p.stunURL}},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}

	// 处理 ICE candidates
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidateJSON, _ := json.Marshal(c.ToJSON())
		p.sendSignal("ice-candidate", peerID, candidateJSON)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Debug("p2p: connection state", "peer", peerID, "state", state)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			p.mu.Lock()
			delete(p.peers, peerID)
			delete(p.dataChans, peerID)
			p.mu.Unlock()
		}
	})

	p.mu.Lock()
	p.peers[peerID] = pc
	p.mu.Unlock()

	return pc, nil
}

// setupDataChannel 配置 DataChannel 回调。
func (p *P2PClient) setupDataChannel(peerID string, dc *webrtc.DataChannel) {
	dc.OnOpen(func() {
		slog.Info("p2p: data channel open", "peer", peerID)
		p.mu.Lock()
		p.dataChans[peerID] = dc
		p.mu.Unlock()
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		p.mu.Lock()
		defer p.mu.Unlock()

		var clipData protocol.ClipboardData
		if err := json.Unmarshal(msg.Data, &clipData); err != nil {
			// 如果不是 JSON，尝试作为原始数据处理
			if p.onReceive != nil {
				p.onReceive(string(msg.Data))
			}
			return
		}

		if clipData.Metadata == nil || !clipData.Metadata.IsFragmented {
			if p.onReceive != nil {
				// 重新序列化为一致的格式发送给应用层
				encoded, _ := json.Marshal(clipData)
				p.onReceive(string(encoded))
			}
			return
		}

		// 处理分片
		meta := clipData.Metadata
		if _, ok := p.receivingFragments[meta.ID]; !ok {
			p.receivingFragments[meta.ID] = make([]string, meta.TotalFragments)
		}
		p.receivingFragments[meta.ID][meta.Index] = clipData.Payload

		// 检查是否所有分片都已到达
		complete := true
		for _, f := range p.receivingFragments[meta.ID] {
			if f == "" {
				complete = false
				break
			}
		}

		if complete {
			fullPayload := strings.Join(p.receivingFragments[meta.ID], "")
			delete(p.receivingFragments, meta.ID)

			if p.onReceive != nil {
				// 组装完整的 ClipboardData 并发送
				recovered := &protocol.ClipboardData{
					Payload: fullPayload,
					Type:    clipData.Type,
				}
				encoded, _ := json.Marshal(recovered)
				p.onReceive(string(encoded))
			}
		}
	})

	dc.OnClose(func() {
		slog.Info("p2p: data channel closed", "peer", peerID)
		p.mu.Lock()
		delete(p.dataChans, peerID)
		p.mu.Unlock()
	})
}

// sendSignal 通过 signaling server 向特定 peer 发送 signaling 消息。
func (p *P2PClient) sendSignal(msgType, to string, data json.RawMessage) {
	msg, _ := json.Marshal(map[string]interface{}{
		"type": msgType,
		"to":   to,
		"data": data,
	})
	p.wsConn.WriteMessage(websocket.TextMessage, msg)
}

func (p *P2PClient) buildWSURL() (string, error) {
	u, err := url.Parse(p.serverURL)
	if err != nil {
		return "", err
	}
	scheme := "ws"
	if strings.HasPrefix(u.Scheme, "https") {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s/p2p", scheme, u.Host), nil
}

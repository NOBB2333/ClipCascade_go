// Package transport 为 mobile 提供 P2P DataChannel 支持。
package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/google/uuid"
	"github.com/clipcascade/pkg/constants"
	"github.com/clipcascade/pkg/protocol"
)


// P2PClient 在 mobile 端管理 WebRTC peer 连接。
type P2PClient struct {
	mu        sync.Mutex
	wsConn    *websocket.Conn
	peers     map[string]*webrtc.PeerConnection
	channels  map[string]*webrtc.DataChannel
	sessionID string
	stunURL   string
	onReceive          func(data string)
	receivingFragments map[string][]string // ID -> fragments
	done               chan struct{}
}

// NewP2PClient 创建一个 mobile P2P client。
func NewP2PClient(stunURL string) *P2PClient {
	return &P2PClient{
		stunURL:            stunURL,
		peers:              make(map[string]*webrtc.PeerConnection),
		channels:           make(map[string]*webrtc.DataChannel),
		receivingFragments: make(map[string][]string),
		done:               make(chan struct{}),
	}
}

// OnReceive 设置从 peers 接收到数据时的处理程序。
func (p *P2PClient) OnReceive(fn func(data string)) {
	p.onReceive = fn
}

// Connect 建立 signaling WebSocket。
func (p *P2PClient) Connect(serverURL string, cookies []*http.Cookie) error {
	u, _ := url.Parse(serverURL)
	scheme := "ws"
	if strings.HasPrefix(u.Scheme, "https") {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/p2p", scheme, u.Host)

	header := http.Header{}
	for _, c := range cookies {
		header.Add("Cookie", c.String())
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return err
	}
	p.wsConn = conn
	go p.signalLoop()
	return nil
}

// Send 向所有开启的 DataChannels 广播数据。
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
			Type:    "text",
			Metadata: &protocol.FragmentMetadata{
				ID:             fragmentID,
				Index:          i,
				TotalFragments: numFragments,
				IsFragmented:   numFragments > 1,
			},
		}

		encoded, _ := json.Marshal(clipData)

		for _, dc := range p.channels {
			if dc.ReadyState() == webrtc.DataChannelStateOpen {
				dc.SendText(string(encoded))
			}
		}
	}
}

// Close 关闭所有连接。
func (p *P2PClient) Close() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	p.mu.Lock()
	for _, pc := range p.peers {
		pc.Close()
	}
	p.peers = make(map[string]*webrtc.PeerConnection)
	p.channels = make(map[string]*webrtc.DataChannel)
	p.mu.Unlock()
	if p.wsConn != nil {
		p.wsConn.Close()
	}
}

func (p *P2PClient) signalLoop() {
	for {
		select {
		case <-p.done:
			return
		default:
		}
		_, msg, err := p.wsConn.ReadMessage()
		if err != nil {
			log.Println("mobile/p2p: signal read error:", err)
			return
		}

		var signal struct {
			Type      string          `json:"type"`
			From      string          `json:"from"`
			SessionID string          `json:"session_id"`
			Data      json.RawMessage `json:"data"`
		}
		if json.Unmarshal(msg, &signal) != nil {
			continue
		}

		switch signal.Type {
		case "session-id":
			p.sessionID = signal.SessionID
		case "peer-list":
			var peers []string
			json.Unmarshal(signal.Data, &peers)
			for _, pid := range peers {
				if pid != p.sessionID {
					p.mu.Lock()
					_, exists := p.peers[pid]
					p.mu.Unlock()
					if !exists {
						go p.createOffer(pid)
					}
				}
			}
		case "offer":
			p.handleOffer(signal.From, signal.Data)
		case "answer":
			p.handleAnswer(signal.From, signal.Data)
		case "ice-candidate":
			p.handleICE(signal.From, signal.Data)
		}
	}
}

func (p *P2PClient) newPC(peerID string) (*webrtc.PeerConnection, error) {
	cfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{p.stunURL}}},
	}
	pc, err := webrtc.NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		data, _ := json.Marshal(c.ToJSON())
		p.sendSignal("ice-candidate", peerID, data)
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			p.mu.Lock()
			delete(p.peers, peerID)
			delete(p.channels, peerID)
			p.mu.Unlock()
		}
	})
	p.mu.Lock()
	p.peers[peerID] = pc
	p.mu.Unlock()
	return pc, nil
}

func (p *P2PClient) createOffer(peerID string) {
	pc, err := p.newPC(peerID)
	if err != nil {
		return
	}
	dc, err := pc.CreateDataChannel("clipboard", nil)
	if err != nil {
		return
	}
	p.setupDC(peerID, dc)
	offer, _ := pc.CreateOffer(nil)
	pc.SetLocalDescription(offer)
	data, _ := json.Marshal(offer)
	p.sendSignal("offer", peerID, data)
}

func (p *P2PClient) handleOffer(from string, data json.RawMessage) {
	pc, err := p.newPC(from)
	if err != nil {
		return
	}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) { p.setupDC(from, dc) })
	var offer webrtc.SessionDescription
	json.Unmarshal(data, &offer)
	pc.SetRemoteDescription(offer)
	answer, _ := pc.CreateAnswer(nil)
	pc.SetLocalDescription(answer)
	d, _ := json.Marshal(answer)
	p.sendSignal("answer", from, d)
}

func (p *P2PClient) handleAnswer(from string, data json.RawMessage) {
	p.mu.Lock()
	pc := p.peers[from]
	p.mu.Unlock()
	if pc == nil {
		return
	}
	var answer webrtc.SessionDescription
	json.Unmarshal(data, &answer)
	pc.SetRemoteDescription(answer)
}

func (p *P2PClient) handleICE(from string, data json.RawMessage) {
	p.mu.Lock()
	pc := p.peers[from]
	p.mu.Unlock()
	if pc == nil {
		return
	}
	var candidate webrtc.ICECandidateInit
	json.Unmarshal(data, &candidate)
	pc.AddICECandidate(candidate)
}

func (p *P2PClient) setupDC(peerID string, dc *webrtc.DataChannel) {
	dc.OnOpen(func() {
		p.mu.Lock()
		p.channels[peerID] = dc
		p.mu.Unlock()
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		p.mu.Lock()
		defer p.mu.Unlock()

		var clipData protocol.ClipboardData
		if err := json.Unmarshal(msg.Data, &clipData); err != nil {
			if p.onReceive != nil {
				p.onReceive(string(msg.Data))
			}
			return
		}

		if clipData.Metadata == nil || !clipData.Metadata.IsFragmented {
			if p.onReceive != nil {
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
		p.mu.Lock()
		delete(p.channels, peerID)
		p.mu.Unlock()
	})
}

func (p *P2PClient) sendSignal(typ, to string, data json.RawMessage) {
	msg, _ := json.Marshal(map[string]interface{}{"type": typ, "to": to, "data": data})
	p.wsConn.WriteMessage(websocket.TextMessage, msg)
}

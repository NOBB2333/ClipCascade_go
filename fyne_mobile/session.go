package main

import (
	"log"
	"sync"

	"fyne.io/fyne/v2"
	"github.com/clipcascade/fynemobile/engine"
)

// Session wraps the STOMP/P2P Engine and handles Fyne UI callbacks.
type Session struct {
	app            fyne.App
	window         fyne.Window
	engine         *engine.Engine
	lastCopied     string
	mu             sync.RWMutex
	status         string
	statusListener func(string)
	textListener   func(string, string)
}

func NewSession(app fyne.App, w fyne.Window) *Session {
	return &Session{
		app:    app,
		window: w,
		status: "disconnected",
	}
}

func (s *Session) SetStatusListener(listener func(string)) {
	s.mu.Lock()
	s.statusListener = listener
	current := s.status
	s.mu.Unlock()

	if listener != nil {
		listener(current)
	}
}

func (s *Session) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Session) SetTextListener(listener func(string, string)) {
	s.mu.Lock()
	s.textListener = listener
	s.mu.Unlock()
}

func (s *Session) LastCopied() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastCopied
}

func (s *Session) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	listener := s.statusListener
	s.mu.Unlock()

	if listener != nil {
		listener(status)
	}
}

// Connect initializes the Engine and starts the connection.
func (s *Session) Connect(serverURL, username, password string, e2ee bool) error {
	s.setStatus("connecting")

	newEngine := engine.NewEngine(serverURL, username, password, e2ee)
	newEngine.SetCallback(s)

	s.mu.Lock()
	s.engine = newEngine
	s.mu.Unlock()

	if err := newEngine.Start(); err != nil {
		s.setStatus("disconnected")
		return err
	}

	s.setStatus("connected")
	return nil
}

// Disconnect stops the engine.
func (s *Session) Disconnect() {
	s.setStatus("disconnecting")

	s.mu.Lock()
	currentEngine := s.engine
	s.engine = nil
	s.mu.Unlock()

	if currentEngine != nil {
		currentEngine.Stop()
	}

	s.setStatus("disconnected")
}

func (s *Session) IsConnected() bool {
	s.mu.RLock()
	currentEngine := s.engine
	state := s.status
	s.mu.RUnlock()

	if state != "connected" && state != "reconnecting" {
		return false
	}
	return currentEngine != nil && currentEngine.IsConnected()
}

func (s *Session) SendText(text string) {
	s.mu.Lock()
	currentEngine := s.engine
	listener := s.textListener
	s.lastCopied = text
	s.mu.Unlock()

	if currentEngine != nil {
		err := currentEngine.SendClipboard(text, "text")
		if err != nil {
			log.Println("Send failed:", err)
		} else if listener != nil {
			listener(text, "sent")
		}
	}
}

func (s *Session) OnMessage(payload string, payloadType string) {
	if payloadType == "text" {
		fyne.Do(func() {
			s.mu.Lock()
			last := s.lastCopied
			listener := s.textListener
			s.mu.Unlock()

			// 防止死循环：如果本地剪贴板已经是这个内容，则跳过
			if s.window.Clipboard().Content() == payload || last == payload {
				return
			}
			log.Println("Received new text payload from server. Writing to Fyne clipboard.")
			s.mu.Lock()
			s.lastCopied = payload
			s.mu.Unlock()
			s.window.Clipboard().SetContent(payload)
			if listener != nil {
				listener(payload, "received")
			}

			// Optional: Send a local system notification (requires Fyne's notification API)
			s.app.SendNotification(fyne.NewNotification("ClipCascade Sync", "New text copied to clipboard!"))
		})
	} else {
		log.Println("Received untested payload type:", payloadType)
	}
}

// OnStatusChange is called by the Engine on disconnects/errors.
func (s *Session) OnStatusChange(status string) {
	log.Println("Engine status changed to:", status)
	s.setStatus(status)

	fyne.Do(func() {
		if status == "disconnected" || status == "error" {
			s.app.SendNotification(fyne.NewNotification("ClipCascade", "Connection lost constraint"))
		}
	})
}

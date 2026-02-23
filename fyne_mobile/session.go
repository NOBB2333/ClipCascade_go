package main

import (
	"log"

	"fyne.io/fyne/v2"
	"github.com/clipcascade/fynemobile/engine"
)

// Session wraps the STOMP/P2P Engine and handles Fyne UI callbacks.
type Session struct {
	app        fyne.App
	window     fyne.Window
	engine     *engine.Engine
	lastCopied string
}

func NewSession(app fyne.App, w fyne.Window) *Session {
	return &Session{
		app:    app,
		window: w,
	}
}

// Connect initializes the Engine and starts the connection.
func (s *Session) Connect(serverURL, username, password string, e2ee bool) error {
	s.engine = engine.NewEngine(serverURL, username, password, e2ee)
	s.engine.SetCallback(s)
	
	return s.engine.Start()
}

// Disconnect stops the engine.
func (s *Session) Disconnect() {
	if s.engine != nil {
		s.engine.Stop()
	}
}

func (s *Session) IsConnected() bool {
	return s.engine != nil && s.engine.IsConnected()
}

func (s *Session) SendText(text string) {
	if s.engine != nil {
		s.lastCopied = text
		err := s.engine.SendClipboard(text, "text")
		if err != nil {
			log.Println("Send failed:", err)
		}
	}
}

func (s *Session) OnMessage(payload string, payloadType string) {
	if payloadType == "text" {
		fyne.Do(func() {
			// 防止死循环：如果本地剪贴板已经是这个内容，则跳过
			if s.window.Clipboard().Content() == payload || s.lastCopied == payload {
				return
			}
			log.Println("Received new text payload from server. Writing to Fyne clipboard.")
			s.lastCopied = payload
			s.window.Clipboard().SetContent(payload)
			
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
	fyne.Do(func() {
		if status == "disconnected" || status == "error" {
			s.app.SendNotification(fyne.NewNotification("ClipCascade", "Connection lost constraint"))
		}
	})
}

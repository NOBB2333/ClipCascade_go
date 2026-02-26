package bridge

import (
	"github.com/clipcascade/fynemobile/engine"
)

// MessageCallback is the mobile callback interface exposed to gomobile bindings.
type MessageCallback interface {
	OnMessage(payload string, payloadType string)
	OnStatusChange(status string)
}

type callbackAdapter struct {
	cb MessageCallback
}

func (a *callbackAdapter) OnMessage(payload string, payloadType string) {
	if a.cb != nil {
		a.cb.OnMessage(payload, payloadType)
	}
}

func (a *callbackAdapter) OnStatusChange(status string) {
	if a.cb != nil {
		a.cb.OnStatusChange(status)
	}
}

// Engine wraps fyne_mobile/engine.Engine for gomobile consumption.
type Engine struct {
	inner   *engine.Engine
	adapter *callbackAdapter
}

// NewEngine creates a bridge engine instance.
func NewEngine(serverURL, username, password string, e2eeEnabled bool) *Engine {
	return &Engine{
		inner: engine.NewEngine(serverURL, username, password, e2eeEnabled),
	}
}

// SetCallback registers callbacks from mobile shell.
func (e *Engine) SetCallback(cb MessageCallback) {
	e.adapter = &callbackAdapter{cb: cb}
	e.inner.SetCallback(e.adapter)
}

// Start starts sync engine.
func (e *Engine) Start() error {
	return e.inner.Start()
}

// Stop stops sync engine.
func (e *Engine) Stop() {
	e.inner.Stop()
}

// SendClipboard sends clipboard payload.
func (e *Engine) SendClipboard(payload string, payloadType string) error {
	return e.inner.SendClipboard(payload, payloadType)
}

// IsConnected returns connection state.
func (e *Engine) IsConnected() bool {
	return e.inner.IsConnected()
}

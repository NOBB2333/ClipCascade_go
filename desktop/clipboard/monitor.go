// Package clipboard 提供跨平台的剪贴板监控和管理功能。
package clipboard

import (
	"context"
	"encoding/base64"
	"log/slog"
	"sync"

	"golang.design/x/clipboard"

	pkgcrypto "github.com/clipcascade/pkg/crypto"
)

// Manager 处理剪贴板监听、更改检测和内容编码。
type Manager struct {
	mu           sync.Mutex
	lastHash     uint64
	ignoreNext   bool // 用于跳过自身触发的更改的标记
	onCopy       func(payload string, payloadType string)
}

// NewManager 创建一个新的剪贴板 Manager。
func NewManager() *Manager {
	return &Manager{}
}

// Init 初始化剪贴板子系统。在某些平台上必须从 main goroutine 调用。
func (m *Manager) Init() error {
	return clipboard.Init()
}

// OnCopy 设置剪贴板内容更改时的回调。
func (m *Manager) OnCopy(fn func(payload string, payloadType string)) {
	m.onCopy = fn
}

// Watch 开始监控剪贴板的文本和图像更改。
// 它一直运行，直到 context 被取消。
func (m *Manager) Watch(ctx context.Context) {
	// 监控文本更改
	textCh := clipboard.Watch(ctx, clipboard.FmtText)
	// 监控图像更改
	imgCh := clipboard.Watch(ctx, clipboard.FmtImage)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-textCh:
				m.handleChange(string(data), "text")
			case data := <-imgCh:
				m.handleChange(base64.StdEncoding.EncodeToString(data), "image")
			}
		}
	}()
}

// handleChange 处理剪贴板更改事件。
func (m *Manager) handleChange(payload string, payloadType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ignoreNext {
		m.ignoreNext = false
		return
	}

	// 使用 xxHash 检查内容是否确实发生了更改
	hash := pkgcrypto.XXHash64(payload)
	if hash == m.lastHash {
		return
	}
	m.lastHash = hash

	slog.Debug("clipboard: change detected", "type", payloadType, "size", len(payload))

	if m.onCopy != nil {
		m.onCopy(payload, payloadType)
	}
}

// Paste sets the clipboard content. Sets ignoreNext flag to avoid self-triggering.
func (m *Manager) Paste(payload string, payloadType string) {
	m.mu.Lock()
	m.ignoreNext = true
	m.lastHash = pkgcrypto.XXHash64(payload)
	m.mu.Unlock()

	switch payloadType {
	case "text":
		clipboard.Write(clipboard.FmtText, []byte(payload))
		slog.Debug("clipboard: pasted text", "size", len(payload))
	case "image":
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			slog.Warn("clipboard: failed to decode image", "error", err)
			return
		}
		clipboard.Write(clipboard.FmtImage, data)
		slog.Debug("clipboard: pasted image", "size", len(data))
	default:
		slog.Warn("clipboard: unsupported type", "type", payloadType)
	}
}

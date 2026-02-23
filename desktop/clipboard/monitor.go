// Package clipboard 提供跨平台的剪贴板监控和管理功能。
package clipboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.design/x/clipboard"

	pkgcrypto "github.com/clipcascade/pkg/crypto"
)

// Manager 处理剪贴板监听、更改检测和内容编码。
type Manager struct {
	mu           sync.Mutex
	lastHash     uint64
	onCopy       func(payload string, payloadType string, filename string)
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
func (m *Manager) OnCopy(fn func(payload string, payloadType string, filename string)) {
	m.onCopy = fn
}

// Watch 开始监控剪贴板的文本和图像更改。
// 它一直运行，直到 context 被取消。
func (m *Manager) Watch(ctx context.Context) {
	// 启动零 CGO 的大文件系统路径监控轮询
	m.startPlatformFileWatcher()

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
				m.handleChange(string(data), "text", "")
			case data := <-imgCh:
				m.handleChange(base64.StdEncoding.EncodeToString(data), "image", "")
			}
		}
	}()
}

// handleChange 处理剪贴板更改事件。
func (m *Manager) handleChange(payload string, payloadType string, filename string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 统一使用 xxHash 检查内容是否确实发生实质性更改（防止自身 Paste 死循环）
	hash := pkgcrypto.XXHash64(payload)
	if hash == m.lastHash {
		return
	}
	m.lastHash = hash

	if payloadType == "file_stub" {
		paths := strings.Split(payload, "\n")
		var totalSize int64
		for _, p := range paths {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				totalSize += info.Size()
			}
		}
		sizeStr := fmt.Sprintf("%.2f MB", float64(totalSize)/(1024*1024))
		slog.Debug("剪贴板：检测到本地大文件复制", "文件数", len(paths), "总大小", sizeStr, "首文件", paths[0])
	} else if payloadType == "file_eager" {
		slog.Debug("剪贴板：检测到本地小文件复制", "文件名", filename, "编码大小", fmt.Sprintf("%.2f MB", float64(len(payload)*3/4)/(1024*1024)))
	} else {
		slog.Debug("剪贴板：检测到更改", "类型", payloadType, "大小", len(payload))
	}

	if m.onCopy != nil {
		m.onCopy(payload, payloadType, filename)
	}
}

// Paste sets the clipboard content. Updates lastHash to securely prevent self-trigger loop echoing.
func (m *Manager) Paste(payload string, payloadType string, filename string) {
	m.mu.Lock()
	m.lastHash = pkgcrypto.XXHash64(payload)
	m.mu.Unlock()

	switch payloadType {
	case "text":
		clipboard.Write(clipboard.FmtText, []byte(payload))
		slog.Debug("剪贴板：已粘贴文本", "大小", len(payload))
	case "image":
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			slog.Warn("剪贴板：无法解码图像", "错误", err)
			return
		}
		clipboard.Write(clipboard.FmtImage, data)
		slog.Debug("剪贴板：已粘贴图像", "大小", len(data))
	case "file_stub":
		paths := strings.Split(payload, "\n")
		firstPath := ""
		if len(paths) > 0 {
			firstPath = paths[0]
		}
		slog.Info("剪贴板：大文件拦截，仅接收路径占位符，跳过直传", "路径数", len(paths), "远程路径预览", firstPath)
	case "file_eager":
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			slog.Warn("剪贴板：无法解码小文件闪电直传数据", "错误", err)
			return
		}
		
		tempDir := filepath.Join(os.TempDir(), "ClipCascade")
		os.MkdirAll(tempDir, 0755)
		
		safeName := filename
		if safeName == "" {
			safeName = "已下载文件"
		}
		
		destPath := filepath.Join(tempDir, safeName)
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			slog.Warn("剪贴板：无法将闪电直传文件保存到磁盘", "错误", err)
			return
		}
		
		slog.Info("剪贴板：已将闪电直传文件保存到本地磁盘", "路径", destPath)
		setPlatformFilePaths([]string{destPath})
	default:	
		slog.Warn("剪贴板：不支持的数据类型", "类型", payloadType)
	}
}

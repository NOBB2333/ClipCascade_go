// Package clipboard 提供跨平台的剪贴板监控和管理功能。
package clipboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.design/x/clipboard"

	"github.com/clipcascade/pkg/constants"
	pkgcrypto "github.com/clipcascade/pkg/crypto"
	"github.com/clipcascade/pkg/sizefmt"
)

// Manager 处理剪贴板监听、更改检测和内容编码。
type Manager struct {
	mu       sync.Mutex
	lastHash uint64
	onCopy   func(payload string, payloadType string, filename string)
	notifier func(title, message string)
}

// fileStubPayload 是文件懒加载模式下的轻量元信息，不包含文件内容。
type fileStubPayload struct {
	Count      int      `json:"count"`
	TotalBytes int64    `json:"total_bytes"`
	Names      []string `json:"names,omitempty"`
	Lazy       bool     `json:"lazy"`
}

const tempFileRetention = 24 * time.Hour

// NewManager 创建一个新的剪贴板 Manager。
func NewManager() *Manager {
	return &Manager{}
}

// Init 初始化剪贴板子系统。在某些平台上必须从 main goroutine 调用。
func (m *Manager) Init() error {
	return clipboard.Init()
}

// CleanupExpiredTempFiles 在启动阶段清理超过保留时长的临时文件。
func (m *Manager) CleanupExpiredTempFiles() {
	tempDir := filepath.Join(os.TempDir(), "ClipCascade")
	cleanupOldTempFiles(tempDir, tempFileRetention)
}

// OnCopy 设置剪贴板内容更改时的回调。
func (m *Manager) OnCopy(fn func(payload string, payloadType string, filename string)) {
	m.onCopy = fn
}

// SetNotifier 设置可选通知回调，避免 clipboard 包直接依赖 UI 层。
func (m *Manager) SetNotifier(fn func(title, message string)) {
	m.notifier = fn
}

// Watch 开始监控剪贴板变更。
// 它通过轮询系统底层的变更计数器（SequenceNumber/ChangeCount）来实现零 CGO 的事件驱动模拟。
func (m *Manager) Watch(ctx context.Context) {
	// macOS / Linux: 文本和图片采用事件监听；文件保持 1s 轮询兜底。
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		go m.watchEventDriven(ctx)
		return
	}

	// 其他平台（Windows）保持原有变更计数轮询策略。
	go m.watchLegacyByChangeCount(ctx)
}

func (m *Manager) watchLegacyByChangeCount(ctx context.Context) {
	var lastCount int64 = -1

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count := getPlatformChangeCount()
			if count == lastCount {
				continue
			}

			// 只有在初始启动后的第一次变化才处理，或者计数器确实发生了位移
			if lastCount != -1 {
				m.handleSystemChange()
			}
			lastCount = count
		}
	}
}

func (m *Manager) watchEventDriven(ctx context.Context) {
	textCh := clipboard.Watch(ctx, clipboard.FmtText)
	imageCh := clipboard.Watch(ctx, clipboard.FmtImage)
	fileTicker := time.NewTicker(1 * time.Second) // 文件一秒循环检测一次
	defer fileTicker.Stop()

	// 与旧轮询逻辑保持一致：忽略启动后的首次事件，避免刚连接时回放当前剪贴板。
	skipFirstTextEvent := true
	skipFirstImageEvent := true
	skipFirstFileTick := true

	for {
		select {
		case <-ctx.Done():
			return
		case <-fileTicker.C:
			if skipFirstFileTick {
				skipFirstFileTick = false
				continue
			}
			m.handleFileChange()
		case data, ok := <-textCh:
			if !ok {
				textCh = nil
				continue
			}
			if skipFirstTextEvent {
				skipFirstTextEvent = false
				continue
			}
			// 保持文件优先级：如果当前是文件剪贴板，优先走文件链路。
			if m.handleFileChange() {
				continue
			}
			if len(data) > 0 {
				m.handleChange(string(data), constants.TypeText, "")
			}
		case data, ok := <-imageCh:
			if !ok {
				imageCh = nil
				continue
			}
			if skipFirstImageEvent {
				skipFirstImageEvent = false
				continue
			}
			if m.handleFileChange() {
				continue
			}
			if len(data) > 0 {
				m.handleChange(base64.StdEncoding.EncodeToString(data), constants.TypeImage, "")
			}
		}
	}
}

// handleSystemChange 当检测到系统剪贴板变动时，按严格的物理优先级尝试读取。
func (m *Manager) handleSystemChange() {
	if m.handleFileChange() {
		return
	}

	// 优先级 2: 图像
	if data := clipboard.Read(clipboard.FmtImage); len(data) > 0 {
		m.handleChange(base64.StdEncoding.EncodeToString(data), constants.TypeImage, "")
		return
	}

	// 优先级 3: 文本 (兜底)
	if data := clipboard.Read(clipboard.FmtText); len(data) > 0 {
		m.handleChange(string(data), constants.TypeText, "")
		return
	}
}

func (m *Manager) handleFileChange() bool {
	// 优先级 1: 文件 (CF_HDROP / Mac Class furl / Linux uri-list)
	paths, _ := getPlatformFilePaths()
	if len(paths) > 0 {
		// 单文件且大小可控时，使用 eager 直传保证跨端可粘贴。
		if len(paths) == 1 {
			if payload, name, ok := buildFileEagerPayload(paths[0]); ok {
				m.handleChange(payload, constants.TypeFileEager, name)
				return true
			}
		}
		// 其余情况走懒加载占位符（多文件/超大文件）。
		payload := buildFileStubPayload(paths)
		meta := buildFileStubMeta(paths)
		m.handleChange(payload, constants.TypeFileStub, meta)
		return true
	}
	return false
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

	if payloadType == constants.TypeFileStub {
		meta := parseFileStubPayloadWithMeta(payload, filename)
		first := ""
		if len(meta.Names) > 0 {
			first = meta.Names[0]
		}
		slog.Debug("剪贴板：检测到文件懒加载占位符", "文件数", meta.Count, "总大小", sizefmt.FormatBytes(meta.TotalBytes), "首文件", first)
	} else if payloadType == constants.TypeFileEager {
		// 兼容旧版本: 仍可收到 file_eager，但本端不再主动发送此类型。
		slog.Debug("剪贴板：收到旧版 file_eager 数据", "文件名", filename, "大小", sizefmt.FormatBytes(int64(sizefmt.EstimatedBase64DecodedSize(payload))))
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
	case constants.TypeText:
		clipboard.Write(clipboard.FmtText, []byte(payload))
		slog.Debug("剪贴板：已粘贴文本", "大小", len(payload))
		if m.notifier != nil {
			m.notifier("ClipCascade", fmt.Sprintf("收到文本剪贴板更新 (%s)", sizefmt.FormatBytes(int64(len(payload)))))
		}
	case constants.TypeImage:
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			slog.Warn("剪贴板：无法解码图像", "错误", err)
			return
		}
		clipboard.Write(clipboard.FmtImage, data)
		slog.Debug("剪贴板：已粘贴图像", "大小", len(data))
		if m.notifier != nil {
			m.notifier("ClipCascade", fmt.Sprintf("收到图片剪贴板更新 (%s)", sizefmt.FormatBytes(int64(len(data)))))
		}
	case constants.TypeFileStub:
		meta := parseFileStubPayloadWithMeta(payload, filename)
		slog.Info("剪贴板：收到文件懒加载占位符", "文件数", meta.Count, "总大小", sizefmt.FormatBytes(meta.TotalBytes))
		if m.notifier != nil {
			m.notifier("ClipCascade", fmt.Sprintf("收到文件剪贴板更新 (%d 个, %s)", meta.Count, sizefmt.FormatBytes(meta.TotalBytes)))
		}
	case constants.TypeFileEager:
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			slog.Warn("剪贴板：无法解码文件直传数据", "错误", err)
			return
		}
		tempDir := filepath.Join(os.TempDir(), "ClipCascade")
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			slog.Warn("剪贴板：无法创建临时目录", "错误", err)
			return
		}
		cleanupOldTempFiles(tempDir, tempFileRetention)
		safeName := sanitizeFilename(filename)
		if safeName == "" {
			safeName = "clipcascade-file"
		}
		destPath := filepath.Join(tempDir, safeName)
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			slog.Warn("剪贴板：无法保存接收文件", "错误", err)
			return
		}
		if err := setPlatformFilePaths([]string{destPath}); err != nil {
			slog.Warn("剪贴板：无法设置文件路径到系统剪贴板", "错误", err)
		}
		size := sizefmt.FormatBytes(int64(len(data)))
		slog.Info("剪贴板：已接收并写入文件到临时目录", "文件名", safeName, "大小", size, "路径", destPath)
		if m.notifier != nil {
			m.notifier("ClipCascade", fmt.Sprintf("收到文件剪贴板更新 (%s)", size))
		}
	default:
		slog.Warn("剪贴板：不支持的数据类型", "类型", payloadType)
	}
}

func buildFileStubPayload(paths []string) string {
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		normalized = append(normalized, p)
	}
	// 发送端优先使用旧版兼容格式（换行分隔路径），保证与历史客户端互通。
	// 新版接收端仍支持解析 JSON file_stub（用于未来协议升级）。
	return strings.Join(normalized, "\n")
}

func buildFileStubMeta(paths []string) string {
	meta := fileStubPayload{
		Names: make([]string, 0, len(paths)),
		Lazy:  true,
	}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		meta.Count++
		name := filepath.Base(p)
		if name == "" || name == "." || name == string(os.PathSeparator) {
			name = "unknown"
		}
		meta.Names = append(meta.Names, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			meta.TotalBytes += info.Size()
		}
	}
	if meta.Count == 0 {
		return ""
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(data)
}

func buildFileEagerPayload(path string) (payload string, filename string, ok bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", "", false
	}
	const maxEagerBytes = constants.DefaultMaxMessageSizeMiB * 1024 * 1024
	if info.Size() <= 0 || info.Size() > maxEagerBytes {
		return "", "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	return base64.StdEncoding.EncodeToString(data), filepath.Base(path), true
}

func parseFileStubPayload(payload string) fileStubPayload {
	return parseFileStubPayloadWithMeta(payload, "")
}

func parseFileStubPayloadWithMeta(payload string, metaRaw string) fileStubPayload {
	var meta fileStubPayload
	if err := json.Unmarshal([]byte(metaRaw), &meta); err == nil && meta.Count > 0 {
		return meta
	}
	if err := json.Unmarshal([]byte(payload), &meta); err == nil && meta.Count > 0 {
		return meta
	}
	// 兼容旧格式: 换行分隔路径
	lines := strings.Split(payload, "\n")
	meta.Lazy = true
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		meta.Count++
		meta.Names = append(meta.Names, filepath.Base(line))
		if info, err := os.Stat(line); err == nil && !info.IsDir() {
			meta.TotalBytes += info.Size()
		}
	}
	return meta
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "." || name == string(os.PathSeparator) {
		return ""
	}
	return name
}

func cleanupOldTempFiles(dir string, olderThan time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if entry.IsDir() {
			_ = os.RemoveAll(path)
		} else {
			_ = os.Remove(path)
		}
	}
}

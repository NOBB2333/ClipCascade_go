//go:build darwin

package clipboard

import (
	"encoding/base64"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/clipcascade/pkg/constants"
)

// getPlatformFilePaths 尝试使用 AppleScript 从 macOS 剪贴板获取底层物理文件的真实路径。
func getPlatformFilePaths() ([]string, error) {
	cmd := exec.Command("osascript", "-e", "return POSIX path of (the clipboard as «class furl»)")
	out, err := cmd.Output()
	if err != nil {
		// 如果剪贴板空或者不是文件的话会走到这，属于正常情况
		return nil, nil
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	// AppleScript 会用逗号分割多个独立的文件路径
	paths := strings.Split(raw, ", ")
	var validPaths []string
	for _, p := range paths {
		if p != "" {
			validPaths = append(validPaths, p)
		}
	}

	return validPaths, nil
}

func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	slog.Info("剪贴板：通过 osascript 将文件路径写入 macOS 剪贴板...")
	// 为演示零CGO架构的懒加载机制，这里包装一层 POSIX file 调用
	script := "set the clipboard to POSIX file \"" + paths[0] + "\""
	cmd := exec.Command("osascript", "-e", script)
	
	// 异步执行，防止阻塞剪贴板 P2P 数据流和 UI假死
	err := cmd.Start()
	if err == nil {
		go cmd.Wait()
	} else {
		slog.Warn("剪贴板：无法设置 macOS 剪贴板文件路径", "错误", err)
	}
	return err
}

// startPlatformFileWatcher 开启一个低频轮询，用于解决 macOS 绕过 CGO 的原生文件变化监控。
func (m *Manager) startPlatformFileWatcher() {
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		var lastPaths string
		for range ticker.C {
			paths, err := getPlatformFilePaths()
			if err != nil || len(paths) == 0 {
				if lastPaths != "" {
					lastPaths = ""
				}
				continue
			}

			payload := strings.Join(paths, "\n")
			if payload == lastPaths {
				continue
			}
			lastPaths = payload

			if len(paths) == 1 {
				info, err := os.Stat(paths[0])
				if err == nil && info.Size() < constants.DefaultMaxMessageSizeMiB*1024*1024 && !info.IsDir() {
					data, err := os.ReadFile(paths[0])
					if err == nil {
						b64 := base64.StdEncoding.EncodeToString(data)
						m.handleChange(b64, "file_eager", filepath.Base(paths[0]))
						continue
					}
				}
			}

			m.handleChange(payload, "file_stub", "")
		}
	}()
}

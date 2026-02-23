//go:build linux

package clipboard

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/clipcascade/pkg/constants"
)

// getPlatformFilePaths 尝试使用 xclip 从剪贴板获取文件 URI (例如 GNOME 的 text/uri-list)
func getPlatformFilePaths() ([]string, error) {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "text/uri-list")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // xclip 失败或格式不匹配时静默返回
	}

	raw := string(bytes.TrimSpace(out))
	if raw == "" {
		return nil, nil
	}

	var validPaths []string
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "file://") {
			// 解析 URL 编码的路径
			if parsed, err := url.Parse(line); err == nil {
				validPaths = append(validPaths, parsed.Path)
			}
		}
	}

	return validPaths, nil
}

// setPlatformFilePaths 使用 xclip 把本地文件路径包装成 text/uri-list 塞回剪贴板
func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	slog.Info("剪贴板：通过 xclip 将文件路径写入 Linux 剪贴板...")

	var uriList []string
	for _, p := range paths {
		// xclip 需要标准的 file:// URI
		uri := "file://" + p
		uriList = append(uriList, uri)
	}
	payload := strings.Join(uriList, "\n")

	// 使用 xclip 写入
	cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "text/uri-list")
	cmd.Stdin = strings.NewReader(payload)
	// 异步执行 xclip，防止网络数据流阻塞
	err := cmd.Start()
	if err == nil {
		// 异步收割，防止产生僵尸进程
		go cmd.Wait()
	} else {
		slog.Warn("剪贴板：通过 xclip 异步写入失败", "错误", err)
	}
	return err
}

// startPlatformFileWatcher 开启一个低频轮询，用于无 CGO 环境下的 Linux 原生文件变化监控。
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

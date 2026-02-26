//go:build darwin

package clipboard

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	pkgcrypto "github.com/clipcascade/pkg/crypto"
)

// getPlatformChangeCount 使用 AppleScript 获取 macOS 剪贴板的 change count。
func getPlatformChangeCount() int64 {
	cmd := exec.Command("osascript", "-e", "return (get the clipboard info)")
	out, err := cmd.Output()
	if err != nil {
		return time.Now().UnixNano() // 降级方案
	}
	// 利用输出的哈希值作为计数器，或者从详细 info 中解析（此处简单处理）
	return int64(pkgcrypto.XXHash64(string(out)))
}

// getPlatformFilePaths 尝试获取 macOS 剪贴板中的物理文件路径。
func getPlatformFilePaths() ([]string, error) {
	cmd := exec.Command("osascript", "-e", "return POSIX path of (the clipboard as «class furl»)")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	paths := strings.Split(raw, ", ")
	var validPaths []string
	for _, p := range paths {
		if p != "" {
			// 严格磁盘校验
			if _, err := os.Stat(p); err == nil {
				validPaths = append(validPaths, p)
			}
		}
	}

	return validPaths, nil
}

func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	slog.Info("剪贴板：通过 osascript 将文件路径写入 macOS 剪贴板...")
	script := "set the clipboard to POSIX file \"" + paths[0] + "\""
	cmd := exec.Command("osascript", "-e", script)
	
	err := cmd.Start()
	if err == nil {
		go cmd.Wait()
	} else {
		slog.Warn("剪贴板：无法设置 macOS 剪贴板文件路径", "错误", err)
	}
	return err
}

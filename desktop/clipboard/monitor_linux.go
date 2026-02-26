//go:build linux

package clipboard

import (
	"bytes"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"

	pkgcrypto "github.com/clipcascade/pkg/crypto"
)

// getPlatformChangeCount 对于 Linux 零 CGO 方案，较难直接获取 SequenceNumber，
// 我们通过读取 xclip 的元数据或直接由 Watch 内部状态管理。
func getPlatformChangeCount() int64 {
	// 简单实现：由于 Watch 已经是 500ms 轮询，这里可以配合 clipboard.Watch 的原生信号
	// 或者针对 Linux 做一个内容哈希（虽然低效但仅限 Linux 兜底）
	cmd := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "TARGETS")
	out, _ := cmd.Output()
	return int64(pkgcrypto.XXHash64(string(out)))
}

// getPlatformFilePaths 尝试使用 xclip 从剪贴板获取文件 URI。
func getPlatformFilePaths() ([]string, error) {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-o", "-t", "text/uri-list")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
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
			if parsed, err := url.Parse(line); err == nil {
				// 严格磁盘校验
				if _, err := os.Stat(parsed.Path); err == nil {
					validPaths = append(validPaths, parsed.Path)
				}
			}
		}
	}

	return validPaths, nil
}

// setPlatformFilePaths 使用 xclip 写入文件路径。
func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	slog.Info("剪贴板：通过 xclip 将文件路径写入 Linux 剪贴板...")

	var uriList []string
	for _, p := range paths {
		uriList = append(uriList, "file://"+p)
	}
	payload := strings.Join(uriList, "\n")

	cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "text/uri-list")
	cmd.Stdin = strings.NewReader(payload)
	err := cmd.Start()
	if err == nil {
		go cmd.Wait()
	} else {
		slog.Warn("剪贴板：通过 xclip 异步写入失败", "错误", err)
	}
	return err
}

//go:build windows

package clipboard

import (
	"encoding/base64"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"github.com/clipcascade/pkg/constants"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procDragQueryFileW   = shell32.NewProc("DragQueryFileW")
)

const CF_HDROP = 15

// getPlatformFilePaths queries the Windows CF_HDROP clipboard purely via Go syscalls.
func getPlatformFilePaths() ([]string, error) {
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return nil, nil
	}
	defer procCloseClipboard.Call()

	hDrop, _, _ := procGetClipboardData.Call(CF_HDROP)
	if hDrop == 0 {
		return nil, nil // No files in clipboard
	}

	count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
	if count == 0 {
		return nil, nil
	}

	var paths []string
	for i := uintptr(0); i < count; i++ {
		size, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0)
		if size == 0 {
			continue
		}

		buf := make([]uint16, size+1)
		procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), size+1)

		path := windows.UTF16ToString(buf)
		if path != "" {
			paths = append(paths, path)
		}
	}

	return paths, nil
}

func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	slog.Info("剪贴板：通过 powershell 将文件路径写入 Windows 剪贴板...")
	
	// 格式化专门用于 PowerShell 的数组格式：'C:\a.txt','D:\b.png'
	var psPaths []string
	for _, p := range paths {
		psPaths = append(psPaths, `'`+p+`'`)
	}
	pathList := strings.Join(psPaths, ",")
	
	cmd := exec.Command("powershell", "-WindowStyle", "Hidden", "-NoProfile", "-Command", "Set-Clipboard", "-Path", pathList)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	
	// 异步执行并隐身，防止阻塞剪贴板 P2P 数据流和 UI 假死
	err := cmd.Start()
	if err == nil {
		// 必须要 Wait，否则后台会产生大量的 zombie 僵尸子进程
		go cmd.Wait()
	} else {
		slog.Warn("剪贴板：通过 powershell 异步写入失败", "错误", err)
	}
	return err
}

// startPlatformFileWatcher starts a low-frequency polling loop for CF_HDROP.
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

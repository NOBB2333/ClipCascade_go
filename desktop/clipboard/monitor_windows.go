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
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
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

	// 再次确认格式，防止在大文件复制过程中剪贴板被释放
	avail, _, _ := procIsClipboardFormatAvailable.Call(CF_HDROP)
	if avail == 0 {
		return nil, nil
	}

	hDrop, _, _ := procGetClipboardData.Call(CF_HDROP)
	if hDrop == 0 {
		return nil, nil // No files in clipboard
	}

	// 锁定内存，确保 hDrop 句柄在读取期间有效
	ptr, _, _ := procGlobalLock.Call(hDrop)
	if ptr == 0 {
		return nil, nil
	}
	defer procGlobalUnlock.Call(hDrop)

	count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
	if count == 0 || count > 1000 { // 增加一个合理的上限，防止恶意内存错误
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
				// 严格检查：必须真实存在且不是目录。某些终端文本可能包含看起来像路径的内容，
				// 但如果没有 os.Stat 确认，会被误判为 file_stub 导致对端触发大文件拦截提示。
				if err == nil && !info.IsDir() {
					if info.Size() < constants.DefaultMaxMessageSizeMiB*1024*1024 {
						data, err := os.ReadFile(paths[0])
						if err == nil {
							b64 := base64.StdEncoding.EncodeToString(data)
							m.handleChange(b64, "file_eager", filepath.Base(paths[0]))
							continue
						}
					}
					// 是真实存在的文件但太大，走占位符模式
					m.handleChange(payload, "file_stub", "")
					continue
				}
			} else if len(paths) > 1 {
				// 多文件情况，至少检查第一个文件是否存在
				if info, err := os.Stat(paths[0]); err == nil && !info.IsDir() {
					m.handleChange(payload, "file_stub", "")
					continue
				}
			}
			
			// 如果路径不存在，说明这可能只是普通的终端文本或误报，不作为文件处理
		}
	}()
}

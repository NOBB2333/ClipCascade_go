// Package ui 提供系统 tray 和通知功能。
package ui

import (
	"log/slog"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
)

// Tray 管理系统 tray 图标和菜单。
type Tray struct {
	onConnect    func()
	onDisconnect func()
	onQuit       func()
	statusItem   *systray.MenuItem
}

// NewTray 创建一个新的系统 tray 管理器。
func NewTray() *Tray {
	return &Tray{}
}

// OnConnect 设置“Connect”菜单点击的回调。
func (t *Tray) OnConnect(fn func()) { t.onConnect = fn }

// OnDisconnect 设置“Disconnect”菜单点击的回调。
func (t *Tray) OnDisconnect(fn func()) { t.onDisconnect = fn }

// OnQuit 设置“Quit”菜单点击的回调。
func (t *Tray) OnQuit(fn func()) { t.onQuit = fn }

// Run 启动系统 tray。这在 tray 退出前保持阻塞。
// 在 macOS 上必须从 main goroutine 调用。
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

// Quit 退出系统 tray。
func (t *Tray) Quit() {
	systray.Quit()
}

func (t *Tray) onReady() {
	if len(iconData) > 0 {
		systray.SetIcon(iconData) // 显示嵌入的图标图片
	}
	// systray.SetTitle("ClipCascade") 
	// 在 macOS 上，如果同时设置了 Title 和 Icon，那么 Title 的纯文本会强制覆盖掉精美的图标！所以留空以显示 Logo 
	systray.SetTooltip("ClipCascade - Clipboard Sync")

	t.statusItem = systray.AddMenuItem("Status: Disconnected", "")
	t.statusItem.Disable()

	systray.AddSeparator()

	connectItem := systray.AddMenuItem("Connect", "Connect to server")
	disconnectItem := systray.AddMenuItem("Disconnect", "Disconnect from server")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "Exit ClipCascade")

	go func() {
		for {
			select {
			case <-connectItem.ClickedCh:
				if t.onConnect != nil {
					t.onConnect()
				}
			case <-disconnectItem.ClickedCh:
				if t.onDisconnect != nil {
					t.onDisconnect()
				}
			case <-quitItem.ClickedCh:
				if t.onQuit != nil {
					t.onQuit()
				}
				systray.Quit()
			}
		}
	}()
}

func (t *Tray) onExit() {
	slog.Info("tray: exiting")
}

// SetStatus 更新 tray 菜单中的状态显示。
func (t *Tray) SetStatus(status string) {
	if t.statusItem != nil {
		t.statusItem.SetTitle("Status: " + status)
	}
}

// Notify 发送桌面通知。
func Notify(title, message string) {
	if err := beeep.Notify(title, message, ""); err != nil {
		slog.Warn("notification failed", "error", err)
	}
}

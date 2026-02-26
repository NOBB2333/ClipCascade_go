package main

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/grandcat/zeroconf"
)

// Main application entry point for the pure Go Fyne mobile client.
func main() {
	a := app.NewWithID("com.clipcascade.mobile")
	w := a.NewWindow("ClipCascade")
	// On mobile, Window.Resize is ignored since apps are forcibly fullscreen,
	// but we set a logical starting size for desktop debugging.
	w.Resize(fyne.NewSize(380, 600))

	// The session manages the connection to the backend Engine
	sess := NewSession(a, w)

	// Create UI elements
	title := widget.NewLabelWithStyle("ClipCascade", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	serverEntry := widget.NewEntry()
	serverEntry.SetPlaceHolder("Scanning network... or enter http://ip:8080")
	serverURL := a.Preferences().StringWithFallback("ServerURL", "")
	serverEntry.Text = serverURL

	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("Username")
	userEntry.Text = a.Preferences().StringWithFallback("Username", "")

	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("Password")
	passEntry.Text = a.Preferences().StringWithFallback("Password", "")

	e2eCheck := widget.NewCheck("Enable E2EE Encryption", nil)
	e2eCheck.Checked = a.Preferences().BoolWithFallback("E2EE", true)

	statusLabel := widget.NewLabel("Status: Disconnected")

	// 自动通过 mDNS 发现局域网服务器
	if serverURL == "" || serverURL == "http://localhost:8080" {
		go func() {
			resolver, err := zeroconf.NewResolver(nil)
			if err != nil {
				return
			}
			entries := make(chan *zeroconf.ServiceEntry)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := resolver.Browse(ctx, "_clipcascade._tcp", "local.", entries); err == nil {
				select {
				case <-ctx.Done():
				case entry := <-entries:
					if entry != nil && len(entry.AddrIPv4) > 0 {
						addr := fmt.Sprintf("http://%s:%d", entry.AddrIPv4[0], entry.Port)
						// 确保在 UI 线程更新组件
						serverEntry.SetText(addr)
					}
				}
			}
		}()
	}

	var connectBtn, disconnectBtn *widget.Button

	applyConnectionState := func(state string) {
		switch state {
		case "connecting":
			statusLabel.SetText("Status: Connecting...")
			connectBtn.Disable()
			disconnectBtn.Disable()
		case "connected":
			statusLabel.SetText("Status: Connected")
			connectBtn.Disable()
			disconnectBtn.Enable()
		case "disconnecting":
			statusLabel.SetText("Status: Disconnecting...")
			connectBtn.Disable()
			disconnectBtn.Disable()
		case "reconnecting":
			statusLabel.SetText("Status: Reconnecting...")
			connectBtn.Disable()
			disconnectBtn.Disable()
		case "error":
			statusLabel.SetText("Status: Error")
			connectBtn.Enable()
			disconnectBtn.Disable()
		default:
			statusLabel.SetText("Status: Disconnected")
			connectBtn.Enable()
			disconnectBtn.Disable()
		}
	}

	connectBtn = widget.NewButtonWithIcon("Connect", theme.LoginIcon(), func() {
		currentState := sess.Status()
		if currentState == "connecting" || currentState == "connected" || currentState == "reconnecting" {
			return
		}
		if serverEntry.Text == "" || userEntry.Text == "" {
			dialog.ShowInformation("Error", "Please enter server URL and credentials", w)
			return
		}

		// Save preferences
		a.Preferences().SetString("ServerURL", serverEntry.Text)
		a.Preferences().SetString("Username", userEntry.Text)
		a.Preferences().SetString("Password", passEntry.Text)
		a.Preferences().SetBool("E2EE", e2eCheck.Checked)

		go func() {
			err := sess.Connect(serverEntry.Text, userEntry.Text, passEntry.Text, e2eCheck.Checked)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w)
				}
			})
		}()
	})

	disconnectBtn = widget.NewButtonWithIcon("Disconnect", theme.LogoutIcon(), func() {
		currentState := sess.Status()
		if currentState != "connected" && currentState != "reconnecting" {
			return
		}
		go sess.Disconnect()
	})

	// Sync clipboard button directly fetches from the OS and sends it
	syncBtn := widget.NewButtonWithIcon("Send OS Clipboard", theme.ContentCopyIcon(), func() {
		if !sess.IsConnected() {
			dialog.ShowInformation("Notice", "Please connect first", w)
			return
		}
		// Read from OS clipboard using Fyne
		content := w.Clipboard().Content()
		if content != "" {
			sess.SendText(content)
			dialog.ShowInformation("Sent", "Clipboard text sent to server.", w)
		} else {
			dialog.ShowInformation("Notice", "Nothing in clipboard to send.", w)
		}
	})

	titleContainer := container.NewCenter(title)

	// 使用 Grid 确保输入框可以随屏幕宽度自动伸缩拉伸 (特别是在高分辨率及异形屏手机上)
	configCard := widget.NewCard("配置", "", container.NewGridWithColumns(1,
		serverEntry,
		userEntry,
		passEntry,
		e2eCheck,
	))

	connCard := widget.NewCard("连接", "", container.NewGridWithColumns(1,
		statusLabel,
		connectBtn,
		disconnectBtn,
	))

	actionHint := widget.NewLabel("Android 10+ 手动同步时，请将应用保持在前台等待剪贴板读取。")
	actionHint.Wrapping = fyne.TextWrapWord
	actionCard := widget.NewCard("操作", "", container.NewGridWithColumns(1,
		actionHint,
		syncBtn,
	))

	// 使用 VBox 组合所有的 Card 组件，并用 Padded 增加呼吸感和边距留白
	formContent := container.NewPadded(container.NewVBox(
		titleContainer,
		configCard,
		connCard,
		actionCard,
	))

	w.SetContent(container.NewVScroll(formContent))

	sess.SetStatusListener(func(state string) {
		fyne.Do(func() {
			applyConnectionState(state)
		})
	})

	// Fyne mobile lifecycle triggers
	a.Lifecycle().SetOnEnteredForeground(func() {
		// When app comes to foreground, we could auto-sync if connected
		if sess.IsConnected() {
			content := w.Clipboard().Content()
			if content != "" && content != sess.lastCopied {
				sess.SendText(content)
			}
		}
	})
	a.Lifecycle().SetOnExitedForeground(func() {
		state := sess.Status()
		if sess.IsConnected() || state == "connecting" || state == "reconnecting" {
			go sess.Disconnect()
		}
	})

	w.ShowAndRun()
}

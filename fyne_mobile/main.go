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
	w.Resize(fyne.NewSize(400, 600))

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

	connectBtn = widget.NewButtonWithIcon("Connect", theme.LoginIcon(), func() {
		if serverEntry.Text == "" || userEntry.Text == "" {
			dialog.ShowInformation("Error", "Please enter server URL and credentials", w)
			return
		}

		// Save preferences
		a.Preferences().SetString("ServerURL", serverEntry.Text)
		a.Preferences().SetString("Username", userEntry.Text)
		a.Preferences().SetString("Password", passEntry.Text)
		a.Preferences().SetBool("E2EE", e2eCheck.Checked)

		statusLabel.SetText("Status: Connecting...")
		connectBtn.Disable()
		
		go func() {
			err := sess.Connect(serverEntry.Text, userEntry.Text, passEntry.Text, e2eCheck.Checked)
			fyne.Do(func() {
				if err != nil {
					statusLabel.SetText("Status: Disconnected")
					connectBtn.Enable()
					dialog.ShowError(err, w)
				} else {
					statusLabel.SetText("Status: Connected")
					disconnectBtn.Enable()
				}
			})
		}()
	})

	disconnectBtn = widget.NewButtonWithIcon("Disconnect", theme.LogoutIcon(), func() {
		sess.Disconnect()
		fyne.Do(func() {
			statusLabel.SetText("Status: Disconnected")
			disconnectBtn.Disable()
			connectBtn.Enable()
		})
	})
	disconnectBtn.Disable()

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

	formContent := container.NewVBox(
		title,
		widget.NewCard("Configuration", "", container.NewVBox(
			serverEntry,
			userEntry,
			passEntry,
			e2eCheck,
		)),
		widget.NewCard("Connection", "", container.NewVBox(
			statusLabel,
			connectBtn,
			disconnectBtn,
		)),
		widget.NewCard("Actions", "Manual Sync (Android 10+ requires App in foreground)", container.NewVBox(
			syncBtn,
		)),
	)

	w.SetContent(container.NewScroll(formContent))
	
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

	w.ShowAndRun()
}

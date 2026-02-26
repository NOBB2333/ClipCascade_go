package main

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2/layout"
	"runtime"
	"sort"
	"strings"
	"sync"
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

	serverEntry := widget.NewSelectEntry(nil)
	serverEntry.SetPlaceHolder("Enter server URL or choose discovered host")
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

	// 自动通过 mDNS 发现局域网服务器（收集全部候选，不只第一个）。
	var discoveredMu sync.Mutex
	discoveredSet := make(map[string]struct{})
	discoveredHosts := make([]string, 0, 8)
	addDiscoveredHost := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		discoveredMu.Lock()
		if _, exists := discoveredSet[addr]; exists {
			discoveredMu.Unlock()
			return
		}
		discoveredSet[addr] = struct{}{}
		discoveredHosts = append(discoveredHosts, addr)
		sort.Strings(discoveredHosts)
		options := append([]string(nil), discoveredHosts...)
		discoveredMu.Unlock()

		fyne.Do(func() {
			serverEntry.SetOptions(options)
			current := strings.TrimSpace(serverEntry.Text)
			if current == "" || current == "http://localhost:8080" {
				serverEntry.SetText(options[0])
			}
		})
	}
	// 把已保存的地址也放进下拉候选里，便于快速切换。
	addDiscoveredHost(serverURL)

	go func() {
		resolver, err := zeroconf.NewResolver(nil)
		if err != nil {
			return
		}
		entries := make(chan *zeroconf.ServiceEntry)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := resolver.Browse(ctx, "_clipcascade._tcp", "local.", entries); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case entry := <-entries:
				if entry == nil {
					continue
				}
				for _, ip := range entry.AddrIPv4 {
					addDiscoveredHost(fmt.Sprintf("http://%s:%d", ip, entry.Port))
				}
				for _, ip := range entry.AddrIPv6 {
					addDiscoveredHost(fmt.Sprintf("http://[%s]:%d", ip, entry.Port))
				}
				if len(entry.AddrIPv4) == 0 && len(entry.AddrIPv6) == 0 && entry.HostName != "" {
					host := strings.TrimSuffix(entry.HostName, ".")
					addDiscoveredHost(fmt.Sprintf("http://%s:%d", host, entry.Port))
				}
			}
		}
	}()

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

	type historyItem struct {
		Text      string
		Direction string
		At        time.Time
	}

	historyLimit := 10
	var historyMu sync.Mutex
	historyItems := make([]historyItem, 0, 20)

	historyList := widget.NewList(
		func() int {
			historyMu.Lock()
			defer historyMu.Unlock()
			if len(historyItems) == 0 {
				return 1
			}
			return len(historyItems)
		},
		func() fyne.CanvasObject {
			dir := widget.NewLabel("·")
			txt := widget.NewLabel("暂无历史记录")
			txt.Wrapping = fyne.TextWrapOff
			return container.NewHBox(dir, txt)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*fyne.Container)
			dir := row.Objects[0].(*widget.Label)
			txt := row.Objects[1].(*widget.Label)

			historyMu.Lock()
			defer historyMu.Unlock()

			if len(historyItems) == 0 {
				dir.SetText("·")
				txt.SetText("暂无历史记录")
				return
			}
			item := historyItems[id]
			arrow := "↓"
			if item.Direction == "sent" {
				arrow = "↑"
			}
			dir.SetText(arrow)
			txt.SetText(fmt.Sprintf("%s  %s", item.At.Format("15:04:05"), item.Text))
		},
	)
	historyScroll := container.NewVScroll(historyList)
	historyScroll.SetMinSize(fyne.NewSize(0, 160))
	historyList.OnSelected = func(id widget.ListItemID) {
		historyMu.Lock()
		if len(historyItems) == 0 || id < 0 || id >= len(historyItems) {
			historyMu.Unlock()
			historyList.Unselect(id)
			return
		}
		text := historyItems[id].Text
		historyMu.Unlock()

		w.Clipboard().SetContent(text)
		a.SendNotification(fyne.NewNotification("ClipCascade", "历史内容已写入本机剪贴板"))
		historyList.Unselect(id)
	}

	appendHistory := func(text, direction string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if len(text) > 160 {
			text = text[:160] + "..."
		}
		line := strings.ReplaceAll(text, "\n", " ")

		historyMu.Lock()
		if len(historyItems) > 0 && historyItems[0].Text == line && historyItems[0].Direction == direction {
			historyMu.Unlock()
			return
		}
		historyItems = append([]historyItem{{
			Text:      line,
			Direction: direction,
			At:        time.Now(),
		}}, historyItems...)
		if len(historyItems) > historyLimit {
			historyItems = historyItems[:historyLimit]
		}
		historyMu.Unlock()
		fyne.Do(func() {
			historyList.Refresh()
		})
	}

	applyLimit := func(n int) {
		historyMu.Lock()
		historyLimit = n
		if len(historyItems) > historyLimit {
			historyItems = historyItems[:historyLimit]
		}
		historyMu.Unlock()
		fyne.Do(func() {
			historyList.Refresh()
		})
	}
	limit10Btn := widget.NewButton("10", func() { applyLimit(10) })
	limit20Btn := widget.NewButton("20", func() { applyLimit(20) })
	clearBtn := widget.NewButton("清空", func() {
		historyMu.Lock()
		historyItems = historyItems[:0]
		historyMu.Unlock()
		fyne.Do(func() {
			historyList.Refresh()
		})
	})

	sess.SetTextListener(func(text, direction string) {
		appendHistory(text, direction)
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
	actionCard := widget.NewCard("操作", "", container.NewVBox(
		actionHint,
		syncBtn,
	))

	historyCard := widget.NewCard("历史记录", "点击某条可回填到本机剪贴板", container.NewBorder(
		container.NewHBox(
			widget.NewLabel("显示"),
			limit10Btn,
			limit20Btn,
			layout.NewSpacer(),
			clearBtn,
		),
		nil, nil, nil,
		historyScroll,
	))

	// 使用 VBox 组合所有的 Card 组件，并用 Padded 增加呼吸感和边距留白
	formContent := container.NewPadded(container.NewVBox(
		titleContainer,
		configCard,
		connCard,
		actionCard,
		historyCard,
	))

	w.SetContent(container.NewVScroll(formContent))

	sess.SetStatusListener(func(state string) {
		fyne.Do(func() {
			applyConnectionState(state)
		})
	})

	// 本机剪贴板监听：连接中自动发送，并作为“sent”进入历史记录。
	lastObservedClipboard := w.Clipboard().Content()
	go func() {
		ticker := time.NewTicker(700 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			content := strings.TrimSpace(w.Clipboard().Content())
			if content == "" || content == lastObservedClipboard {
				continue
			}
			lastObservedClipboard = content
			if !sess.IsConnected() {
				continue
			}
			if content == sess.LastCopied() {
				continue
			}
			sess.SendText(content)
		}
	}()

	// 仅在移动端启用前台回到应用时的补偿同步，不在后台主动断开连接。
	// 目标是尽量保持长连；是否被系统回收由平台策略决定。
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		a.Lifecycle().SetOnEnteredForeground(func() {
			// When app comes to foreground, we could auto-sync if connected
			if sess.IsConnected() {
				content := w.Clipboard().Content()
				if content != "" && content != sess.LastCopied() {
					sess.SendText(content)
				}
			}
		})
	}

	w.ShowAndRun()
}

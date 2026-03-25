# ClipCascade v1.6 - Desktop 重连与剪贴板生命周期修复

## 发布说明

本次更新聚焦于桌面端在休眠唤醒、网络短暂中断、服务端瞬断后的自动重连体验。

此前桌面端存在两个体验问题：

1. 每次断线重连都会重复启动 clipboard watcher / monitor goroutine，长时间运行后可能累积后台监听器
2. 自动重连的首轮尝试会先等待默认退避时间，导致休眠唤醒后的恢复不够直接

## 修复内容

### [fix] `client-desktop/app/application.go` - clipboard 监听只启动一次

- `clip.Init()`、`clip.OnCopy()`、`clip.Watch()` 挪到 `Run()` 中执行
- `monitorConnection()` 也改为在应用生命周期内只启动一次
- 避免每次 `connect()` / 自动重连时不断叠加新的后台 watcher

### [fix] `client-desktop/app/application.go` - 自动重连首轮立即执行

- `monitorConnection()` 的健康检查周期从 `5s` 调整为 `1s`
- `reconnectLoop()` 的第一次重连尝试不再先等待退避时间，而是立即执行
- 后续失败仍保留指数退避

### [fix] `client-desktop/app/application.go` - 重连前统一关闭旧传输对象

- 新增 `closeTransports()`
- 在手动断开和自动重连前先关闭旧的 `stomp/p2p`
- 重连仍沿用原来的 `connect()` 流程，不引入额外的复杂分支

### [fix] `client-desktop/clipboard/monitor.go` - 图片内容按像素规范化去重

- 修复 macOS 与 Windows 间同步图片时可能出现的循环回传
- 原因是同一张图片在不同平台写回系统剪贴板后，会被系统重新编码为不同字节流
- 旧逻辑按原始 base64 字节做哈希，导致“像素相同但编码不同”的图片被误判为新内容
- 新逻辑对图片先解码，再按像素内容计算规范化哈希，避免微信截图等场景触发回环

### [fix] `client-desktop/clipboard/monitor_windows.go` - Windows 原生读取截图剪贴板

- 新增对 `CF_UNICODETEXT` 的原生读取
- 新增对 `CF_DIB` / `CF_DIBV5` 的原生读取，并转换为 PNG 后进入现有 `image` 同步链路
- `handleSystemChange()` 在 Windows 上优先使用原生图片/文本读取，避免仅依赖通用库时漏掉截图工具放入剪贴板的位图内容

## 影响范围

- **macOS 桌面端**：✅ 修复 watcher 累积，并缩短休眠唤醒后的首轮重连等待
- **Windows 桌面端**：✅ 同样受益于 watcher 生命周期修复与首轮立即重连
- **Linux 桌面端**：✅ 同样受益于 watcher 生命周期修复与首轮立即重连
- **iOS / Android 移动端**：无变动

## 编译验证

```bash
cd client-desktop
go test ./...
```

---

*记录人：Codex*

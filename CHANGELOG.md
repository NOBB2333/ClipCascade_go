# Changelog

## v1.1 (2026-02-27)

### Changed

- Desktop 剪贴板监控策略调整：
  - macOS / Linux 的文本与图片改为事件监听（`clipboard.Watch`）。
  - 文件保留轮询兜底，轮询间隔从 `500ms` 调整为 `1s`，降低高频调用带来的 CPU 开销与终端闪烁。
  - Windows 保持原有变更计数方案不变。

### Fixed

- Server WebSocket 日志修复：
  - 修复 E2EE 场景下日志显示 `类型=""`、`体积="0 B"` 的问题。
  - 现在加密包会显示为 `e2ee_envelope`，并输出真实包体大小。

### Mobile / Android

- Android 原生端新增局域网服务发现下拉选择（可手动输入 + 自动发现列表）。
- Android 原生端新增历史剪贴板能力（发送/接收入历史，支持点击回填）。
- Android 原生构建脚本改进：
  - 同时产出可直接安装的 Debug 包和 Release Unsigned 包。
  - 构建链路兼容 Gradle Wrapper，减少环境差异问题。


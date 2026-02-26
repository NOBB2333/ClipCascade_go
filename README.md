# ClipCascade Go

跨设备剪贴板同步工具（Go 版），支持文本、图片、文件同步，支持 STOMP + P2P 双通道。

## 构建

### 环境准备（必看）

基础环境：
- Go `1.22+`（建议与 CI 一致，当前 CI 使用 `1.25`）
- Git
- `PATH` 包含 `$(go env GOPATH)/bin`

按构建目标补充：
- Desktop/Linux 交叉编译：需要 Docker，并确保 Docker daemon 已启动。
- macOS Desktop 构建：需要 Xcode Command Line Tools（`xcode-select --install`）。
- Android（Fyne）构建：需要 Android SDK / NDK。
- Android 原生保活版（`mobile-android-native`）：
  - JDK `17`
  - Android SDK（`platform-tools`、`platforms;android-34`、`build-tools;34.0.0`）
  - Android NDK（建议 `26.1.10909125`）
  - `gomobile`（`go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init`）

建议设置的环境变量（Android 原生保活版）：

```bash
export ANDROID_HOME="$ANDROID_SDK_ROOT"
export ANDROID_NDK_HOME="$ANDROID_SDK_ROOT/ndk/26.1.10909125"
export CC_ANDROID_API=26
```

说明：
- 项目已内置 `mobile/android/gradlew`，不要求全局安装 Gradle。
- 若遇到 Gradle 缓存污染，可删除根目录 `.gradle-user-home/` 后重试。

### 一键构建

```bash
./scripts/build.sh all
```

`all` 会尝试构建：
- server 全平台
- desktop 全平台
- desktop-ui（当前平台）
- mobile-android
- mobile-ios
- desktop-ui-cross（依赖 Docker）

说明：
- Linux desktop 交叉编译依赖 Docker daemon（未启动会被跳过并告警）。
- iOS 打包需要 Apple 开发者证书（无证书会告警）。

### 常用构建命令

```bash
./scripts/build.sh server
./scripts/build.sh server-cross
./scripts/build.sh desktop
./scripts/build.sh cross
./scripts/build.sh desktop-ui
./scripts/build.sh mobile-android
./scripts/build.sh mobile-android-native
```

`mobile-android-native` 产物说明：
- 可直接安装（推荐）：`build/ClipCascade-Android-Debug.apk`
- 便捷别名：`build/ClipCascade-Android-Installable.apk`
- 发布包（未签名）：`build/ClipCascade-Android-Release-Unsigned.apk`

### Windows 黑框模式

- 默认：Windows 桌面端保留控制台黑框（便于看日志）。
- 无黑框托盘模式：

```bash
CLIPCASCADE_WINDOWS_GUI=1 ./scripts/build.sh cross
```

## 运行

### 启动服务端

```bash
./build/clipcascade-server-darwin-arm64
```

默认账号：`admin / admin123`

### 启动桌面端

首次保存配置：

```bash
./build/clipcascade-desktop-darwin-arm64 --server http://127.0.0.1:8080 --username admin --password admin123 --save
```

调试日志：

```bash
./build/clipcascade-desktop-darwin-arm64 --debug
```

## 用户管理

### 现状

- 默认 `signup` 关闭。
- `/signup` 在关闭时会返回 `Signup is disabled`。
- 登录页在 `signup` 关闭时不会再显示“Create an account”，会提示联系管理员。

### 新增用户（无需手调接口）

1. 管理员登录后打开 `/advance`。
2. 点击 `+ Add User`。
3. 输入用户名和密码即可创建。

## 同步行为（当前设计）

### 文本/图片

- 复制后实时同步。

### 文件

- 单文件且大小 `<= 20 MiB`：`file_eager` 直传。
- 多文件或超大文件：`file_stub` 占位（元信息通知）。

接收端 `file_eager` 行为：
- 落盘到临时目录：
  - macOS/Linux: `${TMPDIR}/ClipCascade` 或 `/tmp/ClipCascade`
  - Windows: `%LOCALAPPDATA%\\Temp\\ClipCascade`
- 同时写入系统剪贴板文件路径，用户可直接粘贴。

临时文件清理：
- 接收文件时会自动清理 `24 小时`前的旧临时文件。

## 关键环境变量（Server）

```bash
CC_PORT=8080
CC_SIGNUP_ENABLED=false
CC_P2P_ENABLED=false
CC_P2P_STUN_URL=stun:stun.l.google.com:19302
CC_MAX_MESSAGE_SIZE_IN_MiB=20
CC_ALLOWED_ORIGINS=*
CC_DATABASE_PATH=./database/clipcascade.db
```

## 常见问题

### 1) Docker 相关构建失败

报错：`Cannot connect to the Docker daemon ...`

处理：
```bash
open -a Docker
# 或
open -a OrbStack
```
确认 `docker info` 正常后重试。

### 2) 登录页点 Create 显示 Signup is disabled

这是配置行为（默认关闭注册）。

- 开启公开注册：`CC_SIGNUP_ENABLED=true`
- 推荐方式：保持关闭，由管理员在 `/advance` 新增用户。

### 3) 文件能收到提示但粘贴不可用

请确认两端都使用最新 desktop 二进制，并检查日志是否出现：
- `应用：准备发送剪贴板更新 类型=file_eager`
- `剪贴板：已接收并写入文件到临时目录`

### 4) Android 安装时报“解析软件包出现问题 / packageInfo is null”

通常是安装了 `Release-Unsigned` 包。该包未签名，系统会拒绝安装。

请改为安装：
- `build/ClipCascade-Android-Debug.apk`
- 或 `build/ClipCascade-Android-Installable.apk`

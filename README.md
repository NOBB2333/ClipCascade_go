# ClipCascade Go

跨设备剪贴板同步工具，Go 重写版。

## � 快速开始

### 方式一：脚本构建 (推荐)

```bash
# 查看帮助
./scripts/build.sh help

# 构建服务端 + 桌面客户端
./scripts/build.sh all

# 交叉编译所有平台
./scripts/build.sh cross

# 整理依赖
./scripts/build.sh tidy

# 单独构建
./scripts/build.sh server          # 服务端
./scripts/build.sh desktop         # 桌面客户端
./scripts/build.sh mobile-android  # Android .aar
./scripts/build.sh mobile-ios      # iOS .xcframework
./scripts/build.sh docker          # Docker 镜像
```

### 方式二：手动编译

```bash
# 前置条件: Go 1.22+
go version

# 构建服务端
cd server && go build -ldflags="-s -w" -o clipcascade-server .

# 构建桌面客户端
cd desktop && go build -ldflags="-s -w" -o clipcascade-desktop .

# 构建移动端 (需要 gomobile)
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android -o mobile.aar ./mobile/bridge/     # Android
gomobile bind -target=ios -o Mobile.xcframework ./mobile/bridge/  # iOS
```

### 方式三：Docker 部署

```bash
# 直接运行
docker compose up -d

# 或手动构建
docker build -t clipcascade -f server/Dockerfile .
docker run -d -p 8080:8080 -v clipcascade-data:/data clipcascade
```

### 方式四：Makefile

```bash
make all              # 当前平台 server + desktop
make server-linux     # Linux amd64 + arm64
make desktop-all      # 全平台桌面客户端
make mobile-android   # Android .aar
make docker           # Docker 镜像
make tidy             # 整理依赖
make clean            # 清理构建产物
```

## ⚡ 使用方法
```

---

## 🛠 各平台开发指南 (高级)

### 1. 服务端 (Server)
纯 Go 编写，无 CGO 依赖，支持完美交叉编译。
- **全平台打包**: `./scripts/build.sh server-cross`

### 2. 桌面客户端 (Desktop)
涉及 **CGO 调用系统图形库**。
- **原则**: **必须在目标系统中进行原生构建**。
- **Linux 依赖**: `sudo apt install libgtk-3-dev libx11-dev libayatana-appindicator3-dev pkg-config`
- **注意**: 桌面端建议在对应的物理机中构建，以确保托盘等 GUI 功能完整。

### 3. 移动端 (Mobile)
- **Android**: `./scripts/build.sh mobile-android`
- **iOS**: `./scripts/build.sh mobile-ios` (仅限 macOS)

---

## ⚡ 快速开始

### 1. 启动服务端

```bash
# 默认端口 8080, 默认账号 admin/admin123
./clipcascade-server

# 自定义配置
CC_PORT=9090 CC_SIGNUP_ENABLED=true ./clipcascade-server
```

### 2. 启动桌面客户端

```bash
# 首次运行（保存配置）
./clipcascade-desktop --server http://your-server:8080 --username admin --password admin123 --save

# 之后直接运行（从配置文件读取）
./clipcascade-desktop

# 调试模式
./clipcascade-desktop --debug
```

配置文件位置：
- macOS: `~/Library/Application Support/ClipCascade/config.json`
- Linux: `~/.config/ClipCascade/config.json`
- Windows: `%APPDATA%/ClipCascade/config.json`

### 3. 移动端集成

**Android:**
```
1. ./scripts/build.sh mobile-android
2. 复制 build/mobile.aar → Android 项目 app/libs/
3. build.gradle: implementation files("libs/mobile.aar")
4. 参考 mobile/android/ClipboardService.kt.reference
```

**iOS:**
```
1. ./scripts/build.sh mobile-ios  (需要 macOS)
2. 拖拽 build/Mobile.xcframework → Xcode 项目
3. import Mobile
4. 参考 mobile/ios/ClipCascadeEngine.swift.reference
```

## 🏗 项目结构 & 开发入口

### 核心模块
- **[共享逻辑 (pkg)](file:///Users/wong/Code/PythonLang/CheckDiff/ClipCascade_go/pkg)**: 所有的协议定义、加密算法、STOMP 帧解析都在这里。如果你需要修改同步协议或加密方式，请先修改这里。
- **[服务端 (server)](file:///Users/wong/Code/PythonLang/CheckDiff/ClipCascade_go/server)**: 基于 Fiber 的 Web 服务。
    - 修改 API 或界面：进入 `handler/` 或 `web/templates/`。
    - 修改数据库逻辑：进入 `model/`。
- **[桌面客户端 (desktop)](file:///Users/wong/Code/PythonLang/CheckDiff/ClipCascade_go/desktop)**: 核心逻辑在 `app/`，传输逻辑在 `transport/`。
    - 修改剪贴板监听：进入 `clipboard/`。
    - 修改系统托盘：进入 `ui/`。
- **[移动端桥接 (mobile)](file:///Users/wong/Code/PythonLang/CheckDiff/ClipCascade_go/mobile)**: Go 逻辑入口在 `bridge/bridge.go`。

### 目录总览
```
ClipCascade_go/
├── pkg/                    共享库
│   ├── constants/          协议常量
│   ├── protocol/           STOMP 帧 + ClipboardData 模型
│   └── crypto/             AES-256-GCM + PBKDF2 + xxHash
├── server/                 Fiber 服务端 (13MB)
│   ├── config/             环境变量配置
│   ├── model/              GORM 数据模型
│   ├── handler/            路由处理器 (Auth/WebSocket/P2P/Admin)
│   ├── middleware/         暴力破解防护
│   ├── web/                HTML 模板 + 静态资源
│   └── Dockerfile          多阶段构建
├── desktop/                桌面客户端 (6.5MB)
│   ├── config/             JSON 配置持久化
│   ├── transport/          STOMP + P2P WebRTC 客户端
│   ├── clipboard/          事件驱动剪贴板监控
│   ├── ui/                 系统托盘 + 通知
│   └── app/                应用生命周期管理
├── mobile/                 移动端引擎
│   ├── bridge/             gomobile 导出接口
│   ├── clipboard/          剪贴板处理服务
│   ├── transport/          STOMP + P2P 客户端
│   ├── android/            Kotlin 参考壳
│   └── ios/                Swift 参考壳
├── .github/workflows/      CI/CD
├── scripts/                构建脚本
├── docker-compose.yml      一键部署
├── Makefile                构建命令
└── go.work                 Go Workspace
```

## 🔧 环境变量 (服务端)

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CC_PORT` | 8080 | 监听端口 |
| `CC_SIGNUP_ENABLED` | false | 开放注册 |
| `CC_P2P_ENABLED` | false | P2P 模式 |
| `CC_MAX_MESSAGE_SIZE_IN_MiB` | 1 | 最大消息体积 |
| `CC_MAX_USER_ACCOUNTS` | 0 (无限) | 最大用户数 |
| `CC_ALLOWED_ORIGINS` | * | CORS 白名单 |
| `CC_STUN_URL` | stun:stun.l.google.com:19302 | STUN 服务器 |
| `CC_DATABASE_PATH` | ./database/clipcascade.db | SQLite 路径 |
| `CC_SESSION_TIMEOUT_MINUTES` | 1440 | Session 超时(分) |

## 🔄 CI/CD

推送到 `main` 或创建 `v*` 标签时自动触发：

1. **Test** — 运行测试 + vet
2. **Build Server** — 5 个平台 (linux/darwin/windows × amd64/arm64)
3. **Build Desktop** — 4 个平台 (原生 Runner 编译)
4. **Docker** — 多架构镜像 (amd64 + arm64)
5. **Release** — 标签推送时自动创建 GitHub Release

```bash
# 发布新版本
git tag v1.0.0
git push origin v1.0.0
# → GitHub Actions 自动构建 + 发布所有平台二进制 + Docker 镜像
```

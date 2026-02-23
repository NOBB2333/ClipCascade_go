#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# ClipCascade Go - 构建脚本 (简化版 - 原生优先)
# 用法: ./scripts/build.sh [target...]
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$ROOT_DIR/build"
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo "dev")}"
LDFLAGS="-s -w -X main.Version=$VERSION"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[信息]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[警告]${NC}  $*"; }
error() { echo -e "${RED}[错误]${NC} $*" >&2; }

mkdir -p "$BUILD_DIR"

# ─── 检查环境 ───
check_go() {
    if ! command -v go &>/dev/null; then
        error "未找到 Go 环境。"
        exit 1
    fi
}

check_docker() {
    if ! command -v docker &>/dev/null; then
        error "未找到 Docker 环境。"
        exit 1
    fi
}

check_gomobile() {
    if ! command -v gomobile &>/dev/null; then
        local gopath_bin="$(go env GOPATH)/bin/gomobile"
        if [ -x "$gopath_bin" ]; then
            export PATH="$(go env GOPATH)/bin:$PATH"
        else
            warn "未找到 gomobile，正在尝试安装..."
            go install golang.org/x/mobile/cmd/gomobile@latest || { error "gomobile 安装失败。"; exit 1; }
            export PATH="$(go env GOPATH)/bin:$PATH"
            gomobile init || { error "gomobile init 失败。"; exit 1; }
        fi
    fi
}

# ─── 构建函数 ───
build_server() {
    info "正在构建服务端 (本地平台)..."
    cd "$ROOT_DIR/server"
    go build -ldflags="$LDFLAGS" -o "$BUILD_DIR/clipcascade-server" .
    info "✅ 服务端构建成功 → $BUILD_DIR/clipcascade-server"
}

build_server_cross() {
    info "正在为所有平台交叉编译服务端 (纯 Go)..."
    cd "$ROOT_DIR/server"
    local platforms=("linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64" "windows/amd64")

    for p in "${platforms[@]}"; do
        local os="${p%/*}" arch="${p#*/}"
        local ext=""
        [[ "$os" == "windows" ]] && ext=".exe"
        info "  → 编译中: $os/$arch"
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -ldflags="$LDFLAGS" -o "$BUILD_DIR/clipcascade-server-${os}-${arch}${ext}" .
    done
    info "✅ 服务端交叉编译完成"
}

build_desktop() {
    info "正在构建桌面客户端 (本地平台)..."
    cd "$ROOT_DIR/desktop"
    # 本地编译使用默认 CGO 设置
    go build -ldflags="$LDFLAGS" -o "$BUILD_DIR/clipcascade-desktop" .
    info "✅ 桌面端构建成功 → $BUILD_DIR/clipcascade-desktop"
}

build_desktop_cross() {
    local host_os=$(go env GOOS)
    info "正在为所有平台交叉编译桌面端..."
    
    cd "$ROOT_DIR/desktop"
    mkdir -p "$BUILD_DIR"
    
    # 1. macOS (Darwin) - 原生编译
    info "  → 编译中: darwin/amd64"
    if [[ "$host_os" == "darwin" ]]; then
        GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o "$BUILD_DIR/clipcascade-desktop-darwin-amd64" . || warn "    ⚠ darwin/amd64 编译失败"
        info "  → 编译中: darwin/arm64"
        GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o "$BUILD_DIR/clipcascade-desktop-darwin-arm64" . || warn "    ⚠ darwin/arm64 编译失败"
    else
        warn "    ⚠ 跳过 macOS 构建 (CGO 需要在 macOS 主机编译)"
    fi
    
    # 2. Windows - 因为我们全程实现了纯 Go 的剪贴板钩子，Windows 端现己支持 100% 无 CGO 跨平台编译！再也不需要 MinGW 了！
    info "  → 编译中: windows/amd64 (100% Pure Go)"
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$LDFLAGS -H=windowsgui" -o "$BUILD_DIR/clipcascade-desktop-windows-amd64.exe" . || warn "    ⚠ windows/amd64 编译失败"


    # 3. Linux - 使用 Docker 交叉编译 (由于依赖 GTK3)
    info "  → 编译中: linux/amd64 (通过 Docker)"
    if command -v docker &>/dev/null; then
        info "    正在启动 Docker 容器进行 Linux CGO 编译..."
        docker run --rm -v "$ROOT_DIR:/app" -w /app/desktop golang:1.22-bookworm bash -c "
            apt-get update && apt-get install -y libgtk-3-dev libx11-dev libayatana-appindicator3-dev pkg-config && 
            GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -ldflags=\"$LDFLAGS\" -o /app/build/clipcascade-desktop-linux-amd64 .
        " || warn "    ⚠ linux/amd64 编译失败"
    else
        warn "    ⚠ 未找到 Docker，跳过 Linux 交叉编译"
    fi

    info "✅ 桌面端交叉编译流程结束"
}

export PATH="$PATH:$(go env GOPATH)/bin"

check_fyne() {
    if ! command -v fyne &> /dev/null; then
        warn "未找到 fyne 命令，正在尝试安装..."
        go install fyne.io/tools/cmd/fyne@latest
        if ! command -v fyne &> /dev/null; then
            error "安装 fyne 失败。请确保 $(go env GOPATH)/bin 在您的 PATH 中。"
            exit 1
        fi
        info "✅ fyne 安装成功。"
    fi
}

build_desktop_ui() {
    info "正在构建桌面端 UI 版 (利用 Fyne 可跨所有桌面系统运行)..."
    mkdir -p "$BUILD_DIR"
    local os=$(go env GOOS)
    local arch=$(go env GOARCH)
    
    cd "$ROOT_DIR/fyne_mobile"
    go build -ldflags="-s -w" -o "$BUILD_DIR/clipcascade-ui-desktop-$os-$arch" . || { error "桌面 UI 构建失败"; exit 1; }
    info "✅ 桌面 UI 构建成功 → $BUILD_DIR/clipcascade-ui-desktop-$os-$arch"
    cd "$ROOT_DIR"
}

check_fyne_cross() {
    if ! command -v fyne-cross &> /dev/null; then
        warn "未找到 fyne-cross 命令，正在尝试安装..."
        go install github.com/fyne-io/fyne-cross@latest
        if ! command -v fyne-cross &> /dev/null; then
            error "安装 fyne-cross 失败。请确保 $(go env GOPATH)/bin 在您的 PATH 中。"
            exit 1
        fi
        info "✅ fyne-cross 安装成功。"
    fi
}

build_desktop_ui_cross() {
    info "正在为所有平台交叉编译桌面端 UI 版 (依靠 Docker 和 fyne-cross)..."
    check_fyne_cross
    check_docker
    mkdir -p "$BUILD_DIR"
    
    cd "$ROOT_DIR/fyne_mobile"
    
    info "    → 编译中: windows/amd64 (UI 版)"
    fyne-cross windows -arch=amd64 -app-id com.clipcascade.desktopui || warn "⚠ Windows UI 构建失败 (请检查 Docker)"
    mv fyne-cross/bin/windows-amd64/fynemobile.exe "$BUILD_DIR/clipcascade-ui-desktop-windows-amd64.exe" 2>/dev/null || true
    
    info "    → 编译中: linux/amd64 (UI 版)"
    fyne-cross linux -arch=amd64 -app-id com.clipcascade.desktopui || warn "⚠ Linux UI 构建失败"
    mv fyne-cross/bin/linux-amd64/fynemobile "$BUILD_DIR/clipcascade-ui-desktop-linux-amd64" 2>/dev/null || true
    
    # 清理 Fyne Cross 临时目录
    rm -rf fyne-cross

    info "✅ 所有平台的 UI 桌面版本交叉编译结束"
    cd "$ROOT_DIR"
}

build_mobile_android() {
    info "正在构建 Android 端 (.apk)..."
    check_fyne
    mkdir -p "$BUILD_DIR"
    cd "$ROOT_DIR/fyne_mobile"
    fyne package -os android/arm64 -app-id com.clipcascade.mobile -tags netgo -release
    mv fynemobile.apk "$BUILD_DIR/clipcascade-mobile.apk" 2>/dev/null || warn "APK 文件生成但未能移动到 build 目录"
    info "✅ Android 构建成功 → $BUILD_DIR/clipcascade-mobile.apk"
    cd "$ROOT_DIR"
}

build_mobile_ios() {
    info "正在构建 iOS 端 (.app)..."
    if [[ "$(uname)" != "Darwin" ]]; then
        error "iOS 构建必须在 macOS 上执行。"
        exit 1
    fi
    check_fyne
    mkdir -p "$BUILD_DIR"
    cd "$ROOT_DIR/fyne_mobile"
    fyne package -os ios -app-id com.clipcascade.mobile -release || warn "⚠ iOS 构建失败: 缺少 Apple 开发者证书 (如需免签，请参阅 README 手动通过 Xcode 编译)"
    mv ClipCascade.app "$BUILD_DIR/clipcascade-mobile.app" 2>/dev/null || true
    if [ -d "$BUILD_DIR/clipcascade-mobile.app" ]; then
        info "✅ iOS 构建成功 → $BUILD_DIR/clipcascade-mobile.app"
    fi
    cd "$ROOT_DIR"
}

build_docker() {
    info "正在构建 Docker 镜像..."
    check_docker
    cd "$ROOT_DIR"
    docker build -t clipcascade -f server/Dockerfile . || { error "Docker 镜像构建失败"; exit 1; }
    info "✅ Docker 镜像构建成功 → clipcascade:latest"
}

tidy() {
    info "清理模块依赖..."
    for d in pkg server desktop; do
        if [ -d "$ROOT_DIR/$d" ]; then
            cd "$ROOT_DIR/$d" && go mod tidy
        fi
    done
    info "✅ 整理完成"
}

clean() {
    info "移除构建产物..."
    rm -rf "$BUILD_DIR"
}

show_help() {
    echo "ClipCascade 全能构建脚本 (无 CGO 依赖重制版 & Fyne UI 合并版)"
    echo "用法: $0 {server|server-cross|desktop|desktop-ui|desktop-ui-cross|cross|mobile-android|mobile-ios|docker|all|tidy|clean}"
    echo
    echo "命令:"
    echo "  desktop          构建当前系统原生隐形式 Desktop 托盘端 (无界面纯后台)"
    echo "  desktop-ui       构建当前系统的 Fyne 图形化 Desktop 桌面端面板 (可视化操作)"
    echo "  desktop-ui-cross 跨平台交叉编译 Mac, Windows, Linux 的图形化桌面端面板 (依赖 Docker)"
    echo "  cross            交叉编译 Mac, Windows, Linux 桌面(无界面)端和全平台 Server"
    echo "  mobile-android   使用 Fyne 构建 Android (.apk) 安装包"
    echo "  mobile-ios       使用 Fyne 构建 iOS (.app) (仅限 Mac)"
    echo "  server           构建本地 Server 二进制文件"
    echo "  server-cross     交叉编译所有平台 Server 架构"
    echo "  docker           将 Server 构建为 Docker 镜像"
    echo "  all              一键满配全平台编译: 隐形式桌面端 + UI式桌面端 + Android + iOS + Server"
    echo "  tidy             对所有模块运行 'go mod tidy'"
    echo "  clean            删除生成的 build 目录和所有二进制文件"
}

# ─── 主流程 ───
if [[ -d "/opt/homebrew/share/android-ndk" && -z "${ANDROID_NDK_HOME:-}" ]]; then
    export ANDROID_NDK_HOME="/opt/homebrew/share/android-ndk"
fi

if [[ -d "/usr/local/share/android-ndk" && -z "${ANDROID_NDK_HOME:-}" ]]; then
    export ANDROID_NDK_HOME="/usr/local/share/android-ndk"
fi

check_go
[[ $# -eq 0 ]] && show_help && exit 0

for target in "$@"; do
    case "$target" in
        server)           build_server ;;
        server-cross)     build_server_cross ;;
        desktop)          build_desktop ;;
        desktop-ui)       build_desktop_ui ;;
        desktop-ui-cross) build_desktop_ui_cross ;;
        cross)            build_server_cross; build_desktop_cross; build_desktop_ui_cross ;;
        mobile-android)   build_mobile_android ;;
        mobile-ios)       build_mobile_ios ;;
        docker)           build_docker ;;
        all)              build_server_cross; build_desktop_cross; build_desktop_ui; build_mobile_android; build_mobile_ios; build_desktop_ui_cross ;;
        tidy)           tidy ;;
        clean)          clean ;;
        *)              show_help; exit 1 ;;
    esac
done

info "操作完成!"

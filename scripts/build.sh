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
    
    # 2. Windows - 使用 MinGW 交叉编译
    info "  → 编译中: windows/amd64"
    if command -v x86_64-w64-mingw32-gcc &>/dev/null; then
        GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -ldflags="$LDFLAGS -H=windowsgui" -o "$BUILD_DIR/clipcascade-desktop-windows-amd64.exe" . || warn "    ⚠ windows/amd64 编译失败"
    else
        warn "    ⚠ 未找到 x86_64-w64-mingw32-gcc，跳过 Windows 交叉编译 (请运行: brew install mingw-w64)"
    fi

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

build_mobile_android() {
    info "正在构建 Android 端 (.aar)..."
    check_gomobile
    mkdir -p "$BUILD_DIR"
    cd "$ROOT_DIR"
    gomobile bind -target=android -o "$BUILD_DIR/mobile.aar" ./mobile/bridge/ || { error "Android 构建失败"; exit 1; }
    info "✅ Android 构建成功 → $BUILD_DIR/mobile.aar"
}

build_mobile_ios() {
    local host_os=$(go env GOOS)
    if [[ "$host_os" != "darwin" ]]; then
        error "iOS 构建必须在 macOS 上进行。"
        exit 1
    fi
    info "正在构建 iOS 端 (.xcframework)..."
    check_gomobile
    mkdir -p "$BUILD_DIR"
    cd "$ROOT_DIR"
    gomobile bind -target=ios -o "$BUILD_DIR/Mobile.xcframework" ./mobile/bridge/ || { error "iOS 构建失败"; exit 1; }
    info "✅ iOS 构建成功 → $BUILD_DIR/Mobile.xcframework"
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
    for d in pkg server desktop mobile; do
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
    cat <<EOF
用法: $0 [目标...]

构建目标:
  server            构建本地平台服务端
  server-cross      交叉编译所有平台服务端 (Linux/Mac/Win)
  desktop           构建本地平台桌面客户端
  mobile-android    构建 Android 端 (.aar)
  mobile-ios        构建 iOS 端 (.xcframework) (仅限 macOS)
  docker            构建 Docker 镜像
  all               构建本地服务端 + 本地桌面端
  cross             服务端全平台 + 桌面端跨平台
  tidy              整理依赖
  clean             清理 build 目录
EOF
}

# ─── 主流程 ───
check_go
[[ $# -eq 0 ]] && show_help && exit 0

for target in "$@"; do
    case "$target" in
        server)         build_server ;;
        server-cross)   build_server_cross ;;
        desktop)        build_desktop ;;
        mobile-android) build_mobile_android ;;
        mobile-ios)     build_mobile_ios ;;
        docker)         build_docker ;;
        all)            build_server; build_desktop ;;
        cross)          build_server_cross; build_desktop_cross ;;
        tidy)           tidy ;;
        clean)          clean ;;
        *)              show_help; exit 1 ;;
    esac
done

info "操作完成!"

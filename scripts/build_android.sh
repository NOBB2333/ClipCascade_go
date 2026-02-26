#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# ClipCascade Go - Android 混合架构构建脚本
# 此脚本负责：
# 1. 使用 gomobile bind 将纯 Go 引擎编译为 engine.aar
# 2. 调用 Gradle 将 engine.aar 与 Kotlin 写的保活壳组合打包为 APK
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$ROOT_DIR/build"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[信息]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[警告]${NC}  $*"; }
error() { echo -e "${RED}[错误]${NC} $*" >&2; }

mkdir -p "$BUILD_DIR"
cd "$ROOT_DIR"

# ─── 环境检查 ───
if ! command -v gomobile &>/dev/null; then
    warn "未找到 gomobile，尝试将其添加到 PATH..."
    export PATH="$(go env GOPATH)/bin:$PATH"
    if ! command -v gomobile &>/dev/null; then
        error "无法找到 gomobile，请先运行: go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init"
        exit 1
    fi
fi

if [[ -z "${ANDROID_HOME:-}" && -z "${ANDROID_SDK_ROOT:-}" ]]; then
    warn "未设置 ANDROID_HOME 环境变量。如果编译失败，请先设置 Android SDK 路径。"
fi

# ─── 第一步：编译 Go 核心引擎 (AAR) ───
info "第一步：使用 gomobile 编译 Go 核心逻辑为 engine.aar..."
mkdir -p mobile/android/app/libs
gomobile bind -target=android -o mobile/android/app/libs/engine.aar ./fyne_mobile/engine
info "✅ Go 引擎编译成功: mobile/android/app/libs/engine.aar"

# ─── 第二步：组合打包 Kotlin 原生壳 (APK) ───
info "第二步：使用 Gradle 组装 Android Kotlin 壳应用..."
cd mobile/android

# 赋予 gradlew 执行权限
chmod +x ./gradlew

# 清理并构建 Release 版本
./gradlew clean assembleRelease

# 返回根目录
cd "$ROOT_DIR"

APK_SRC="mobile/android/app/build/outputs/apk/release/app-release-unsigned.apk"
APK_DEST="$BUILD_DIR/ClipCascade-Android-Release-Unsigned.apk"

if [[ -f "$APK_SRC" ]]; then
    cp "$APK_SRC" "$APK_DEST"
    info ""
    info "🎉 恭喜！Android 安装包构建完毕！"
    info "📦 输出路径: $APK_DEST"
    info "💡 提示：该包尚未签名，可使用 jarsigner 签名后安装，或直接在 Android Studio 中点击 Run 调试。"
else
    error "找不到构建产物 APK 文件！请检查 Gradle 构建日志。"
    exit 1
fi

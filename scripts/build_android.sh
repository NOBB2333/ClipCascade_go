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

# 使用项目内 Gradle HOME，避免全局 ~/.gradle 的权限或缓存污染导致构建异常。
if [[ -z "${GRADLE_USER_HOME:-}" ]]; then
    export GRADLE_USER_HOME="$ROOT_DIR/.gradle-user-home"
fi
mkdir -p "$GRADLE_USER_HOME"

# 自动探测 Homebrew Android SDK 路径，减少手工环境配置。
if [[ -z "${ANDROID_HOME:-}" && -d "/opt/homebrew/share/android-commandlinetools" ]]; then
    export ANDROID_HOME="/opt/homebrew/share/android-commandlinetools"
fi
if [[ -z "${ANDROID_HOME:-}" && -d "/usr/local/share/android-commandlinetools" ]]; then
    export ANDROID_HOME="/usr/local/share/android-commandlinetools"
fi
if [[ -n "${ANDROID_HOME:-}" && -z "${ANDROID_SDK_ROOT:-}" ]]; then
    export ANDROID_SDK_ROOT="$ANDROID_HOME"
fi

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
ANDROID_API="${CC_ANDROID_API:-26}"
gomobile bind -target=android -androidapi "$ANDROID_API" -javapkg bridge -o mobile/android/app/libs/engine.aar ./fyne_mobile/bridge
info "✅ Go 引擎编译成功: mobile/android/app/libs/engine.aar"

# ─── 第二步：组合打包 Kotlin 原生壳 (APK) ───
info "第二步：使用 Gradle 组装 Android Kotlin 壳应用..."
cd mobile/android

if [[ -x "./gradlew" ]]; then
    chmod +x ./gradlew
    ./gradlew --no-daemon clean assembleDebug assembleRelease
elif command -v gradle &>/dev/null; then
    gradle --no-daemon clean assembleDebug assembleRelease
else
    error "未找到 gradlew 或 gradle，无法构建 Android 原生壳。"
    exit 1
fi

# 返回根目录
cd "$ROOT_DIR"

APK_DEBUG_SRC="mobile/android/app/build/outputs/apk/debug/app-debug.apk"
APK_RELEASE_UNSIGNED_SRC="mobile/android/app/build/outputs/apk/release/app-release-unsigned.apk"
APK_DEBUG_DEST="$BUILD_DIR/ClipCascade-Android-Debug.apk"
APK_RELEASE_UNSIGNED_DEST="$BUILD_DIR/ClipCascade-Android-Release-Unsigned.apk"
APK_INSTALLABLE_DEST="$BUILD_DIR/ClipCascade-Android-Installable.apk"

if [[ -f "$APK_DEBUG_SRC" ]]; then
    cp "$APK_DEBUG_SRC" "$APK_DEBUG_DEST"
    cp "$APK_DEBUG_SRC" "$APK_INSTALLABLE_DEST"
fi

if [[ -f "$APK_RELEASE_UNSIGNED_SRC" ]]; then
    cp "$APK_RELEASE_UNSIGNED_SRC" "$APK_RELEASE_UNSIGNED_DEST"
fi

if [[ ! -f "$APK_DEBUG_DEST" && ! -f "$APK_RELEASE_UNSIGNED_DEST" ]]; then
    error "找不到构建产物 APK 文件！请检查 Gradle 构建日志。"
    exit 1
fi

info ""
info "🎉 恭喜！Android 安装包构建完毕！"
if [[ -f "$APK_DEBUG_DEST" ]]; then
    info "📦 可直接安装包（已签名 Debug）: $APK_DEBUG_DEST"
    info "📦 便捷安装别名: $APK_INSTALLABLE_DEST"
fi
if [[ -f "$APK_RELEASE_UNSIGNED_DEST" ]]; then
    info "📦 发布包（Unsigned，需要签名后安装）: $APK_RELEASE_UNSIGNED_DEST"
fi

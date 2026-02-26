# ClipCascade Go - Build & Cross-Compile
# Usage: make server | make desktop | make mobile-android | make all

.PHONY: all server desktop mobile-android mobile-ios clean tidy docker

# Versioning
VERSION ?= 0.1.0
LDFLAGS = -ldflags="-s -w -X main.Version=$(VERSION)"

# Output directory
OUT = build

all: server-cross desktop

# --- Server ---
server:
	@echo "🔧 Building server..."
	@mkdir -p $(OUT)
	cd server && go build $(LDFLAGS) -o ../$(OUT)/clipcascade-server .
	@echo "✅ Server: $(OUT)/clipcascade-server"

server-cross:
	@echo "🔧 Cross-compiling server for all platforms..."
	@mkdir -p $(OUT)
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-server-linux-amd64 .
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-server-linux-arm64 .
	cd server && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-server-darwin-arm64 .
	cd server && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-server-darwin-amd64 .
	cd server && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-server-windows-amd64.exe .
	@echo "✅ Server cross-compile complete"

# --- Desktop Client ---
desktop:
	@echo "🔧 Building desktop client..."
	@mkdir -p $(OUT)
	cd desktop && go build $(LDFLAGS) -o ../$(OUT)/clipcascade-desktop .
	@echo "✅ Desktop: $(OUT)/clipcascade-desktop"

desktop-all:
	@mkdir -p $(OUT)
	cd desktop && GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-desktop-darwin-arm64 .
	cd desktop && GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-desktop-darwin-amd64 .
	cd desktop && GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-desktop-linux-amd64 .
	cd desktop && GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o ../$(OUT)/clipcascade-desktop-windows-amd64.exe .
	@echo "✅ Desktop cross-compile complete"

# --- Mobile (Hybrid Architecture) ---
mobile-android:
	@echo "🔧 Building Go Engine AAR..."
	@mkdir -p mobile/android/app/libs $(OUT)
	gomobile bind -target=android -o mobile/android/app/libs/engine.aar ./fyne_mobile/engine
	@echo "🔧 Building Android APK via Gradle..."
	cd mobile/android && ./gradlew assembleRelease
	cp mobile/android/app/build/outputs/apk/release/app-release-unsigned.apk $(OUT)/ClipCascade-Android-Release-Unsigned.apk
	@echo "✅ Android APK: $(OUT)/ClipCascade-Android-Release-Unsigned.apk"

mobile-ios:
	@echo "🔧 Building iOS .xcframework..."
	@mkdir -p $(OUT)
	gomobile bind -target=ios -o $(OUT)/Mobile.xcframework ./fyne_mobile/engine
	@echo "✅ iOS: $(OUT)/Mobile.xcframework"

# --- Docker ---
docker:
	@echo "🐳 Building Docker image..."
	docker build -t clipcascade-server:$(VERSION) -f server/Dockerfile .
	@echo "✅ Docker: clipcascade-server:$(VERSION)"

docker-run:
	docker run -d --name clipcascade \
		-p 8080:8080 \
		-v clipcascade-data:/data \
		-e CC_SIGNUP_ENABLED=true \
		clipcascade-server:$(VERSION)

# --- Utilities ---
tidy:
	cd pkg && go mod tidy
	cd server && go mod tidy
	cd desktop && go mod tidy
	cd fyne_mobile && go mod tidy

test:
	go test ./pkg/... ./server/... ./desktop/...

clean:
	rm -rf $(OUT) server/database/
	cd mobile/android && ./gradlew clean || true

fmt:
	gofmt -w pkg/ server/ desktop/ fyne_mobile/

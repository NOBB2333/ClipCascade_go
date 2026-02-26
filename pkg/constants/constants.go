// Package constants 为 ClipCascade 协议定义全项目共享的常量。
package constants

const (
	// --- STOMP 目标端点 ---

	// SubscriptionDestination 是接收剪贴板数据的订阅地址。
	SubscriptionDestination = "/user/queue/cliptext"
	// SendDestination 是将数据发送到服务器的地址。
	SendDestination = "/app/cliptext"

	// --- WebSocket 与网络端点 ---

	// StompWSEndpoint 是核心剪贴板同步的 WebSocket 路径。
	StompWSEndpoint = "/clipsocket"
	// P2PWSEndpoint 是 P2P WebRTC 信令的 WebSocket 路径。
	P2PWSEndpoint = "/p2p"
	// DefaultStunURL 是建立 WebRTC 连接时默认使用的 Google STUN 服务器。
	DefaultStunURL = "stun:stun.l.google.com:19302"

	// --- STOMP 帧命令 ---

	StompConnect    = "CONNECT"
	StompConnected  = "CONNECTED"
	StompSubscribe  = "SUBSCRIBE"
	StompSend       = "SEND"
	StompMessage    = "MESSAGE"
	StompError      = "ERROR"
	StompDisconnect = "DISCONNECT"

	// --- 心跳与超时 (毫秒) ---

	// HeartbeatSendInterval 是发送心跳的间隔时间。
	HeartbeatSendInterval = 10000
	// HeartbeatReceiveInterval 是期望接收心跳的最大间隔时间。
	HeartbeatReceiveInterval = 10000
	// WebSocketTimeout 是建立连接时的超时限制。
	WebSocketTimeout = 10000

	// --- 重连策略 (秒) ---

	// DefaultReconnectDelay 是首次重连尝试前的等待时间。
	DefaultReconnectDelay = 5
	// MaxReconnectDelay 是重连指数退避的最大等待时间。
	MaxReconnectDelay = 60

	// --- 应用信息 ---

	AppName = "ClipCascade"
	// AppVersion 是当前应用的版本号。
	AppVersion = "0.1.0"

	// --- 剪贴板内容类型 ---

	TypeText  = "text"
	TypeImage = "image"
	TypeFiles = "files"

	// --- P2P 分片配置 ---

	// FragmentSize 是 P2P 数据传输时的固定分片大小 (16KB)。
	FragmentSize = 16384

	// --- 默认服务器安全配置 ---

	// DefaultPort 是服务器默认监听的端口。
	DefaultPort = 8080
	// DefaultMaxMessageSizeMiB 是服务器处理的默认最大消息大小 (单位: MiB)。
	DefaultMaxMessageSizeMiB = 20
	// DefaultSessionTimeoutMin 是默认用户会话有效期 (约 1 年)。
	DefaultSessionTimeoutMin = 525960
	// DefaultMaxUniqueIPAttempts 是允许的同一 IP 对应的最大唯一 IP 尝试次数。
	DefaultMaxUniqueIPAttempts = 15
	// DefaultMaxAttemptsPerIP 是同一 IP 允许的最大失败登录尝试次数。
	DefaultMaxAttemptsPerIP = 30
	// DefaultLockTimeoutSeconds 是触发锁定后的初始超时时间。
	DefaultLockTimeoutSeconds = 60
	// DefaultLockScalingFactor 是锁定时间增加的缩放倍数。
	DefaultLockScalingFactor = 2
)

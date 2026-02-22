// Package protocol 提供一个轻量级的 STOMP 帧解析器/序列化器，
// 与原始 ClipCascade STOMP-over-WebSocket protocol 兼容。
//
// 仅实现了 ClipCascade 使用的 STOMP 命令子集：
// CONNECT、CONNECTED、SUBSCRIBE、SEND、MESSAGE、ERROR、DISCONNECT。
package protocol

import (
	"fmt"
	"strings"
)

// Frame 表示单个 STOMP protocol 帧。
type Frame struct {
	Command string
	Headers map[string]string
	Body    string
}

// NewFrame 使用给定的 command 创建一个 STOMP 帧。
func NewFrame(command string) *Frame {
	return &Frame{
		Command: command,
		Headers: make(map[string]string),
	}
}

// Set 将 header 键值对添加到帧中。
func (f *Frame) Set(key, value string) *Frame {
	f.Headers[key] = value
	return f
}

// Get 通过 key 返回 header 值。
func (f *Frame) Get(key string) string {
	return f.Headers[key]
}

// Encode 将帧序列化为 STOMP 线路格式 (wire-format) 字节 slice。
// 格式：COMMAND\nheader1:value1\nheader2:value2\n\nbody\x00
func (f *Frame) Encode() []byte {
	var sb strings.Builder
	sb.WriteString(f.Command)
	sb.WriteByte('\n')

	for k, v := range f.Headers {
		sb.WriteString(k)
		sb.WriteByte(':')
		sb.WriteString(v)
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')
	sb.WriteString(f.Body)
	sb.WriteByte(0) // NULL 终止符

	return []byte(sb.String())
}

// ParseFrame 从 bytes 解析原始 STOMP 帧。
func ParseFrame(data []byte) (*Frame, error) {
	raw := string(data)

	// 如果存在，删除尾部的 NULL 字节
	raw = strings.TrimRight(raw, "\x00")

	// 分割为 header 部分和 body
	parts := strings.SplitN(raw, "\n\n", 2)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid STOMP frame: empty data")
	}

	headerSection := parts[0]
	body := ""
	if len(parts) == 2 {
		body = parts[1]
	}

	// 解析 command (第一行) 和 headers
	lines := strings.Split(headerSection, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("invalid STOMP frame: no command line")
	}

	frame := &Frame{
		Command: strings.TrimSpace(lines[0]),
		Headers: make(map[string]string),
		Body:    body,
	}

	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		key := line[:idx]
		value := line[idx+1:]
		frame.Headers[key] = value
	}

	return frame, nil
}

// --- 常见帧的便捷构造函数 ---

// ConnectFrame 创建一个 STOMP CONNECT 帧。
func ConnectFrame(acceptVersion, host string) *Frame {
	return NewFrame("CONNECT").
		Set("accept-version", acceptVersion).
		Set("host", host)
}

// ConnectedFrame 创建一个 STOMP CONNECTED 响应帧。
func ConnectedFrame(version string) *Frame {
	return NewFrame("CONNECTED").
		Set("version", version)
}

// SubscribeFrame 创建一个 STOMP SUBSCRIBE 帧。
func SubscribeFrame(id, destination string) *Frame {
	return NewFrame("SUBSCRIBE").
		Set("id", id).
		Set("destination", destination)
}

// SendFrame 创建一个带 body 的 STOMP SEND 帧。
func SendFrame(destination, body string) *Frame {
	f := NewFrame("SEND").
		Set("destination", destination)
	f.Body = body
	return f
}

// MessageFrame 为交付给订阅者创建一个 STOMP MESSAGE 帧。
func MessageFrame(destination, subscriptionID, messageID, body string) *Frame {
	f := NewFrame("MESSAGE").
		Set("destination", destination).
		Set("subscription", subscriptionID).
		Set("message-id", messageID)
	f.Body = body
	return f
}

// ErrorFrame 创建一个 STOMP ERROR 帧。
func ErrorFrame(message string) *Frame {
	f := NewFrame("ERROR").
		Set("message", message)
	return f
}


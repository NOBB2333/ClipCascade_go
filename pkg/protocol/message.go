// Package protocol 定义了用于 server 和所有 client 之间交换剪贴板内容的 ClipboardData 消息模型。
package protocol

import "encoding/json"

// FragmentMetadata 包含分片消息的调试和重组信息。
type FragmentMetadata struct {
	ID             string `json:"id"`
	Index          int    `json:"index"`
	TotalFragments int    `json:"totalFragments"`
	IsFragmented   bool   `json:"isFragmented"`
}

// ClipboardData 表示通过 WebSocket 或 P2P 传输的剪贴板 payload。
// 它是所有 ClipCascade 组件之间共享的核心消息格式。
type ClipboardData struct {
	// Payload 是剪贴板内容，对于图像/文件，通常是 base64 编码的或者是文件路径列表。
	Payload string `json:"payload"`
	// Type 表示内容类型："text"、"image"、"file_stub"(懒加载占位)、"file_eager"(旧版兼容) 或 "file_request"。
	Type string `json:"type"`
	// FileName 保存用于小文件闪电直传的原始文件名。
	FileName string `json:"filename,omitempty"`
	// Metadata 包含分片信息（可选）。
	Metadata *FragmentMetadata `json:"metadata,omitempty"`
}

// Encode 将 ClipboardData 序列化为 JSON 字节 slice。
func (cd *ClipboardData) Encode() ([]byte, error) {
	return json.Marshal(cd)
}

// DecodeClipboardData 将 JSON 字节 slice 反序列化为 ClipboardData。
func DecodeClipboardData(data []byte) (*ClipboardData, error) {
	var cd ClipboardData
	if err := json.Unmarshal(data, &cd); err != nil {
		return nil, err
	}
	return &cd, nil
}

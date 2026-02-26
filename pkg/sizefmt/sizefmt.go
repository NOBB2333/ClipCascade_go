package sizefmt

import (
	"fmt"

	"github.com/clipcascade/pkg/constants"
)

// FormatBytes 将字节数转换为可读字符串，例如 12.4 KB / 3.10 MB。
func FormatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	kb := float64(size) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	mb := kb / 1024
	if mb < 1024 {
		return fmt.Sprintf("%.2f MB", mb)
	}
	gb := mb / 1024
	return fmt.Sprintf("%.2f GB", gb)
}

// EstimatedBase64DecodedSize 估算 base64 字符串解码后的原始字节大小。
func EstimatedBase64DecodedSize(s string) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	padding := 0
	if s[n-1] == '=' {
		padding++
		if n > 1 && s[n-2] == '=' {
			padding++
		}
	}
	return (n*3)/4 - padding
}

// HumanSizeFromPayload 根据 payload 类型输出可读大小。
func HumanSizeFromPayload(payloadType string, payload string) string {
	size := int64(len(payload))
	if payloadType == constants.TypeImage || payloadType == constants.TypeFileEager {
		size = int64(EstimatedBase64DecodedSize(payload))
	}
	return FormatBytes(size)
}

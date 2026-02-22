// Package clipboard 提供 mobile 端剪贴板服务逻辑。
// 实际的剪贴板读/写由原生层 (Kotlin/Swift) 处理，
// 本 package 提供 Go 端处理流水线。
package clipboard

import (
	"encoding/base64"
	"sync"

	pkgcrypto "github.com/clipcascade/pkg/crypto"
)

// Service 在原生层和网络之间处理剪贴板内容。
type Service struct {
	mu       sync.Mutex
	lastHash uint64
}

// NewService 创建一个新剪贴板 service。
func NewService() *Service {
	return &Service{}
}

// HasChanged 使用 xxHash 检查内容自上次检查以来是否发生了更改。
// 如果已更改，则返回 true (并更新存储的 hash)。
func (s *Service) HasChanged(content string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	hash := pkgcrypto.XXHash64(content)
	if hash == s.lastHash {
		return false
	}
	s.lastHash = hash
	return true
}

// EncodeImage 将原始图像 bytes 转换为用于网络传输的 base64 字符串。
func EncodeImage(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeImage 将 base64 字符串转换回原始图像 bytes。
func DecodeImage(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}

// EncodeFiles converts multiple file byte arrays to a JSON array of base64 strings.
// Each file is represented as: {"name": "filename", "data": "base64..."}
func EncodeFiles(names []string, data [][]byte) string {
	// Simplified: for gomobile compatibility, we just concatenate base64 with separator
	// Native layer handles the actual file→bytes conversion
	result := "["
	for i, d := range data {
		if i > 0 {
			result += ","
		}
		result += `{"name":"` + names[i] + `","data":"` + base64.StdEncoding.EncodeToString(d) + `"}`
	}
	result += "]"
	return result
}

package crypto

import (
	"encoding/binary"
	"hash"

	"github.com/cespare/xxhash/v2"
)

// XXHash64 计算输入字符串的 xxHash64。
// 用于快速剪贴板内容更改检测，
// 匹配 Python 的 xxhash.xxh64(payload).intdigest()。
func XXHash64(data string) uint64 {
	return xxhash.Sum64String(data)
}

// NewXXHash64 返回一个新的实现 hash.Hash64 的 xxHash64 hasher。
func NewXXHash64() hash.Hash64 {
	return xxhash.New()
}

// XXHash64Bytes 计算原始 bytes 的 xxHash64。
func XXHash64Bytes(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// XXHash64ToBytes 将 uint64 hash 转换为 8 字节大端序 (big-endian) slice。
func XXHash64ToBytes(h uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, h)
	return b
}

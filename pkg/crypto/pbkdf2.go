package crypto

import (
	"crypto/sha256"
	"crypto/sha512"

	"golang.org/x/crypto/pbkdf2"
)

// DeriveKey 使用 PBKDF2-HMAC-SHA256 派生 AES-256 密钥，
// 与原始 Python CipherManager.hash_password 实现匹配。
//
// 参数：
//   - password: user 密码
//   - username: 用于构建 salt
//   - salt:     server 提供的 salt 字符串
//   - rounds:   PBKDF2 迭代次数
//
// Salt 构建方式为：username + password + salt (与 Python 匹配)。
func DeriveKey(password, username, salt string, rounds int) []byte {
	saltBytes := []byte(username + password + salt)
	return pbkdf2.Key([]byte(password), saltBytes, rounds, 32, sha256.New)
}

// SHA3_512Hex 计算输入字符串的小写十六进制 SHA3-512 hash，
// 与 Python 的 hashlib.sha3_512(...).hexdigest() 匹配。
func SHA3_512Hex(input string) string {
	h := sha512.New()
	h.Write([]byte(input))
	sum := h.Sum(nil)
	// 转换为小写十六进制
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(sum)*2)
	for i, v := range sum {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
	return string(dst)
}

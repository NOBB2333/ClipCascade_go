// Package crypto 提供 AES-256-GCM 加密/解密和 PBKDF2 密钥派生，
// 与原始 ClipCascade Python/Java 实现完全兼容。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// EncryptedPayload 保存 AES-GCM 密文的三个组成部分。
type EncryptedPayload struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	Tag        []byte `json:"tag"`
}

// 使用指定的 key 利用 AES-256-GCM 加密明文。
// 返回 nonce、ciphertext 和身份验证 tag。
// 与 Python PyCryptodome AES.MODE_GCM 兼容。
func Encrypt(key []byte, plaintext []byte) (*EncryptedPayload, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	aesGCM, err := cipher.NewGCMWithNonceSize(block, 16) // PyCryptodome default nonce = 16 bytes
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce generation: %w", err)
	}

	// Go's GCM Seal appends tag to ciphertext; split them for compatibility.
	sealed := aesGCM.Seal(nil, nonce, plaintext, nil)
	tagStart := len(sealed) - aesGCM.Overhead()

	return &EncryptedPayload{
		Nonce:      nonce,
		Ciphertext: sealed[:tagStart],
		Tag:        sealed[tagStart:],
	}, nil
}

// Decrypt 使用指定的密钥解密 AES-256-GCM 加密的载荷。
// 与 Python PyCryptodome decrypt_and_verify 兼容。
func Decrypt(key []byte, payload *EncryptedPayload) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	aesGCM, err := cipher.NewGCMWithNonceSize(block, len(payload.Nonce))
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	// Reconstruct the sealed message (ciphertext + tag) as Go expects.
	sealed := append(payload.Ciphertext, payload.Tag...)

	plaintext, err := aesGCM.Open(nil, payload.Nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption/verification failed: %w", err)
	}

	return plaintext, nil
}

// EncodeToJSONString converts an EncryptedPayload to a JSON string with
// base64-encoded values, matching the Python CipherManager.encode_to_json_string format.
func EncodeToJSONString(payload *EncryptedPayload) (string, error) {
	m := map[string]string{
		"nonce":      base64.StdEncoding.EncodeToString(payload.Nonce),
		"ciphertext": base64.StdEncoding.EncodeToString(payload.Ciphertext),
		"tag":        base64.StdEncoding.EncodeToString(payload.Tag),
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeFromJSONString parses a JSON string with base64-encoded values back
// into an EncryptedPayload, matching the Python CipherManager.decode_from_json_string format.
func DecodeFromJSONString(jsonStr string) (*EncryptedPayload, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(m["nonce"])
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(m["ciphertext"])
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(m["tag"])
	if err != nil {
		return nil, fmt.Errorf("decode tag: %w", err)
	}

	return &EncryptedPayload{
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Tag:        tag,
	}, nil
}

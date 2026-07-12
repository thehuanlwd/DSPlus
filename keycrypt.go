package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

// apiKeyEncPrefix 用于标记 config.json 中已加密存储的 API Key。
// 没有此前缀的值视为旧版明文，原样保留（向后兼容），下次保存时会被加密。
const apiKeyEncPrefix = "ENC:"

// apiKeySecret 是用于混淆的内置密钥材料。
// 注意：这属于“混淆”而非绝对安全——真实密钥由该常量派生，避免以明文落盘。
// 任何拿到本程序的人理论上都能还原明文，目的是防止直接打开 config.json 看到密钥。
const apiKeySecret = "DSPlus::apiKey::obfuscation::v1"

func apiKeyDeriveKey() []byte {
	h := sha256.Sum256([]byte(apiKeySecret))
	return h[:]
}

// encryptAPIKey 将明文 API Key 加密为带前缀的 base64 字符串。
// 空字符串直接返回空（不加密）。
func encryptAPIKey(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(apiKeyDeriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return apiKeyEncPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// decryptAPIKey 还原 encryptAPIKey 的结果。
// 若值不含 ENC: 前缀（旧版明文），原样返回；解密失败返回错误，由调用方决定如何处理。
func decryptAPIKey(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, apiKeyEncPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, apiKeyEncPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(apiKeyDeriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Package termxkey TermX 同步接入密钥的解析与持久化。
// 自动生成的密钥以 keyring（data/auth.key 那把 AES-256 密钥）加密后落盘
package termxkey

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"os"

	"ky/internal/pkg/keyring"
)

func Peek(configured string, kr *keyring.Keyring, path string) string {
	if configured != "" {
		return configured
	}
	if kr == nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	plain, err := kr.Decrypt(raw)
	if err != nil || len(plain) == 0 {
		return ""
	}
	return string(plain)
}

// Resolve 解析生效密钥。configured 非空直接返回（显式配置优先,调试/过渡用）;
// 否则读取加密文件，不存在则生成新密钥并加密落盘。
func Resolve(configured string, kr *keyring.Keyring, path string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if kr == nil {
		return "", nil // keyring 未初始化（不应发生）：保持禁用，由中间件拒绝
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		plain, err2 := kr.Decrypt(raw)
		if err2 == nil && len(plain) > 0 {
			return string(plain), nil
		}
		log.Printf("TermX 同步密钥文件无法解密（%v），将重新生成（旧客户端需更新密钥）", err2)
		// 落到下方重新生成
	} else if !os.IsNotExist(err) {
		return "", err
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	// 加密 hex 字符串本身：读回解密即得到可直接比对的密钥串
	hexKey := hex.EncodeToString(key)
	enc, err := kr.Encrypt([]byte(hexKey))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, enc, 0o600); err != nil {
		return "", err
	}
	return hexKey, nil
}

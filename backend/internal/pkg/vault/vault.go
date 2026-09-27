// Package vault 零知识保险库原语：主密码不落服务器，磁盘上不存在可解密数据的密钥。
//
//	vaultKey（随机 32B，真正加密业务数据的库密钥，仅存于内存/被包裹的密文）
//	  ├─ VaultCipher    = GCM(vaultKey, PBKDF2(主密码, VaultSalt))   ← 改密只重包这一层
//	  └─ RecoveryCipher = GCM(vaultKey, PBKDF2(恢复密钥, VaultSalt)) ← 忘记密码不丢数据
//	登录验证 = 常数时间比较 PBKDF2(主密码, AuthSalt)（AuthHash，不可逆）
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

// 派生参数：PBKDF2-SHA256，OWASP 2023 建议的最低迭代次数
const (
	KeyLen    = 32
	IterCount = 210000
	SaltLen   = 16
)

// ErrWrongKey 解密失败（密钥不正确或密文损坏）
var ErrWrongKey = errors.New("密钥不正确或数据无法解密")

func RandomSalt() []byte {
	b := make([]byte, SaltLen)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(fmt.Sprintf("生成盐失败: %v", err))
	}
	return b
}

func RandomKey() []byte {
	b := make([]byte, KeyLen)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(fmt.Sprintf("生成库密钥失败: %v", err))
	}
	return b
}

func Derive(secret string, salt []byte) []byte {
	return pbkdf2.Key([]byte(secret), salt, IterCount, KeyLen, sha256.New)
}

// Seal 用 key 做 AES-256-GCM 加密，返回 nonce‖密文。
func Seal(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Open 解开 Seal 产出的 nonce‖密文;密钥不正确返回 ErrWrongKey
func Open(key, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	n := gcm.NonceSize()
	if len(blob) < n {
		return nil, ErrWrongKey
	}
	plain, err := gcm.Open(nil, blob[:n], blob[n:], nil)
	if err != nil {
		return nil, ErrWrongKey
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyLen {
		return nil, errors.New("密钥长度不合法")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

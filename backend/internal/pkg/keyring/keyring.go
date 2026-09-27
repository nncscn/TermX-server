// Package keyring 负责登录密码的静态加密：AES-256-GCM 密钥的持久化与加解密。
// 密钥以 base64 文本存放在独立文件（0600 权限），不进入数据库。
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// KeySize AES-256 密钥长度（字节）。
const KeySize = 32

// Keyring 持有一把 256 位密钥，提供 GCM 加解密。
type Keyring struct {
	gcm cipher.AEAD
}

// Load 只读取密钥文件（不生成，无副作用）；文件不存在或内容非法返回错误。
func Load(path string) (*Keyring, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(string(trimSpace(raw)))
	if err != nil {
		return nil, errors.New("密钥文件内容不是合法的 base64")
	}
	return newKeyring(key)
}

// LoadOrCreate 读取密钥文件;不存在则生成新密钥并写入（0600 权限）。
func LoadOrCreate(path string) (*Keyring, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return generate(path)
	}

	key, err := base64.StdEncoding.DecodeString(string(trimSpace(raw)))
	if err != nil {
		return nil, errors.New("密钥文件内容不是合法的 base64")
	}
	return newKeyring(key)
}

// Encrypt 使用 AES-256-GCM 加密,返回 nonce‖密文。
func (k *Keyring) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return k.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (k *Keyring) Decrypt(blob []byte) ([]byte, error) {
	n := k.gcm.NonceSize()
	if len(blob) < n {
		return nil, errors.New("密文长度不合法")
	}
	return k.gcm.Open(nil, blob[:n], blob[n:], nil)
}

// newKeyring 由 32 字节密钥构造 GCM
func newKeyring(key []byte) (*Keyring, error) {
	if len(key) != KeySize {
		return nil, errors.New("密钥长度必须为 256 位")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keyring{gcm: gcm}, nil
}

func generate(path string) (*Keyring, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(key)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// 0600：仅属主可读写,密钥文件不允许其他用户访问
	if err := os.WriteFile(path, []byte(encoded+"\n"), 0o600); err != nil {
		return nil, err
	}
	return newKeyring(key)
}

// trimSpace 去掉文件内容首尾的空白与换行。
func trimSpace(b []byte) []byte {
	s := string(b)
	for len(s) > 0 && (s[0] == '\n' || s[0] == '\r' || s[0] == ' ') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return []byte(s)
}

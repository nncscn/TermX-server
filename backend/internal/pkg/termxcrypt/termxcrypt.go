//	TermX 线格式 Wrap{v,c,n} ——c=base64(GCM密文‖tag)、n=base64(12B nonce)，
//	密钥 = VaultKey(服务器密钥, 仓库盐)（复刻客户端 PBKDF2→HKDF 派生）；
//	ky 原生格式 —— vault.Seal(库密钥) 的 nonce‖密文 blob（Web 端数据密钥同源）。
//
// 服务端同时持有两把密钥材料（接入密钥在配置、库密钥在 Web 登录后的内存）,
// TermX 端拉取/推送照常——两端都看到完整明文（信任模型：服务端可解密）
package termxcrypt

// ⚠ 信任模型声明（安全审计 H1）：服务器持有本包两把密钥材料，可解密 TermX 副本
// TermX 条目"这一功能的必然代价。彻底零知识需撤销服务端翻译,见
// docs/信任模型与安全边界.md。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

// 派生参数与客户端 authSession.ts 严格一致
const (
	pbkdf2Iters = 250000
	hkdfInfo    = "termx-vault-data-v1"
)

type Wrap struct {
	V int    `json:"v"`
	C string `json:"c"`
	N string `json:"n"`
}

var ErrBadCipher = errors.New("密文不正确或数据无法解密")

// VaultKey 复刻客户端派生：PBKDF2-SHA256(服务器密钥, 仓库盐, 250000, 64B)
// 取后 32 字节经 HKDF-SHA256(info="termx-vault-data-v1") 展开 32 字节数据密钥。
func VaultKey(serverKey, saltHex string) ([]byte, error) {
	salt, err := hex.DecodeString(saltHex)
	if err != nil || len(salt) == 0 {
		return nil, errors.New("仓库盐非法")
	}
	out := pbkdf2.Key([]byte(serverKey), salt, pbkdf2Iters, 64, sha256.New)
	rd := hkdf.New(sha256.New, out[32:64], nil, []byte(hkdfInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(rd, key); err != nil {
		return nil, err
	}
	return key, nil
}

// Seal 加密任意 JSON 可序列化对象为 Wrap（payload {"v":1,"session":…}）。
func Seal(key []byte, payload any) (Wrap, error) {
	plain, err := json.Marshal(payload)
	if err != nil {
		return Wrap{}, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return Wrap{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Wrap{}, err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)
	return Wrap{
		V: 1,
		C: base64.StdEncoding.EncodeToString(ct),
		N: base64.StdEncoding.EncodeToString(nonce),
	}, nil
}

// Open 解开 Wrap，返回载荷的原始 JSON 字节（调用方自行反序列化）
func Open(key []byte, w Wrap) ([]byte, error) {
	if w.V != 1 || w.C == "" || w.N == "" {
		return nil, ErrBadCipher
	}
	ct, err1 := base64.StdEncoding.DecodeString(w.C)
	nonce, err2 := base64.StdEncoding.DecodeString(w.N)
	if err1 != nil || err2 != nil {
		return nil, ErrBadCipher
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrBadCipher
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("密钥长度不合法")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

//	sig = base64url(HMAC-SHA256(secret, header "." payload))
//
// 密钥为 TermxState.TokenSecret（每账户一份,存库、服务重启不失效）。
package termxtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const headerB64 = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"

// TTL 令牌有效期（与官网仓库令牌一致：1 小时）。
const TTL = time.Hour

type Claims struct {
	Sub   uint   `json:"sub"`
	Scope string `json:"scope"`
	Ep    int    `json:"ep"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

// Sign 签发令牌。
func Sign(secret string, sub uint, epoch int) string {
	now := time.Now()
	cl := Claims{Sub: sub, Scope: "vault", Ep: epoch, Iat: now.Unix(), Exp: now.Add(TTL).Unix()}
	payload, _ := json.Marshal(cl)
	pl := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(headerB64 + "." + pl))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return headerB64 + "." + pl + "." + sig
}

func Verify(secret, token string) (Claims, error) {
	var zero Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return zero, errors.New("令牌格式非法")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), mustB64Decode(parts[2])) {
		return zero, errors.New("令牌签名不符")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, errors.New("令牌载荷非法")
	}
	var cl Claims
	if err := json.Unmarshal(raw, &cl); err != nil {
		return zero, errors.New("令牌载荷非法")
	}
	if cl.Exp < time.Now().Unix() {
		return zero, errors.New("令牌已过期")
	}
	return cl, nil
}

// mustB64Decode 解码失败返回空切片（hmac.Equal 随之判否）,不额外报错分支。
func mustB64Decode(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

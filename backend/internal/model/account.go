package model

import "time"

// Account 登录账户。零知识保险库结构：
//   - 登录验证只存 PBKDF2 单向哈希（AuthSalt+AuthHash）,服务器无法还原主密码;
//   - 业务数据由随机库密钥加密，库密钥只以两种"包裹"形态落库：
//     VaultCipher（主密码派生密钥包裹）/ RecoveryCipher（恢复密钥派生密钥包裹）,
type Account struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	Username            string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	AuthSalt            []byte     `json:"-"`
	AuthHash            []byte     `json:"-"`
	VaultSalt           []byte     `json:"-"`
	VaultCipher         []byte     `json:"-"`
	RecoveryCipher      []byte     `json:"-"`
	RecoveryHash        string     `gorm:"size:64" json:"-"`
	PasswordCipher      []byte     `json:"-"` // 旧数据迁移标记，迁移完清空
	FailedAttempts      int        `gorm:"not null;default:0" json:"-"`
	LockedUntil         *time.Time `json:"-"`
	RecoveryAttempts    int        `gorm:"not null;default:0" json:"-"`
	RecoveryLockedUntil *time.Time `json:"-"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// Session 登录会话。库中只存令牌的 SHA-256 哈希（token_hash）,
type Session struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TokenHash string    `gorm:"column:token_hash;size:64;uniqueIndex;not null" json:"-"`
	AccountID uint      `gorm:"index;not null" json:"account_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

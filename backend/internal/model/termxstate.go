package model

import "time"

// TermxState TermX 桌面客户端的同步仓库状态（每账户至多一行）。
// TermX 的仓库密码体系与 ky 主密码体系相互独立：
//
//	  （salt 32 hex + dk 64 hex，客户端只上传 PBKDF2 派生键，明文密码不出本机）；
//	 - VaultGen 为仓库代数（重置仓库密码 +1，客户端据此清同步基线全量重建）；
//	- VaultEpoch 为令牌代数（重新设置仓库密码 +1,旧仓库令牌即刻失效）;
//	 - TokenSecret 为本行随机生成的 HMAC 密钥，用于签发/校验 TermX 的
//
// 客户端会以"仅元数据"骨架形式呈现待用户补全；如需彻底重来可同时清空
// credentials 表（见 docs 对接说明）。
type TermxState struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	AccountID   uint      `gorm:"uniqueIndex;not null" json:"-"`
	VaultSalt   string    `gorm:"size:64" json:"-"`
	VaultDk     string    `gorm:"size:64" json:"-"`
	VaultGen    int       `gorm:"not null;default:0" json:"-"`
	VaultEpoch  int       `gorm:"not null;default:0" json:"-"`
	TokenSecret string    `gorm:"size:64" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

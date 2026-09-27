package model

import "time"

// Secret 为 ky 原生密文（库密钥加密，Web 端数据密钥可解）；TermxSecret 为
// TermX 线格式密文 JSON {"v":1,"c","n"}（客户端数据密钥加密,同步收发用）
// 深融合：服务端持有两把密钥材料，写入时双向翻译保持两列一致——Web 端对
// 同步条目保持原生编辑体验,TermX 端照常解密（信任模型：服务端可解密）。
type Credential struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	AccountID   uint       `gorm:"index;not null;uniqueIndex:uk_cred_account_client" json:"-"`
	ClientID    *string    `gorm:"size:64;uniqueIndex:uk_cred_account_client" json:"-"`
	Name        string     `gorm:"size:64;not null" json:"name"`
	Protocol    string     `gorm:"size:16;not null;default:ssh" json:"protocol"`
	AuthType    string     `gorm:"size:16;not null;default:password" json:"auth_type"`
	Group       string     `gorm:"size:64" json:"group"`
	KeyID       *uint      `gorm:"index" json:"key_id"`
	Secret      []byte     `json:"-"`
	TermxSecret *string    `gorm:"type:text" json:"-"`
	Remark      string     `gorm:"size:120" json:"remark"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

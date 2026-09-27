package model

import "time"

// SshKey SSH 密钥实体。名称/类型/指纹/备注为明文列（可搜索、可列表展示）；
// 密钥内容（公钥/私钥/解密口令）整体 JSON 序列化后 AES-256-GCM 加密存 Secret，
// 仅在验主密码的 reveal 接口中解密返回
type SshKey struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	AccountID   uint       `gorm:"index;not null" json:"-"`
	Name        string     `gorm:"size:40;not null" json:"name"`
	Type        string     `gorm:"size:16;not null" json:"type"`
	Fingerprint string     `gorm:"size:64" json:"fingerprint"`
	Secret      []byte     `json:"-"`
	Remark      string     `gorm:"size:120" json:"remark"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

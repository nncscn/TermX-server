package model

import "time"

// （启动页/剪贴板清除/自动锁定/回收站保留）;数据库连接等服务器级
type Preference struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	AccountID          uint      `gorm:"uniqueIndex;not null" json:"-"`
	StartPage          string    `gorm:"size:16;not null;default:/dashboard" json:"start_page"`
	ClipboardClear     int       `gorm:"not null;default:0" json:"clipboard_clear"`
	AutolockMinutes    int       `gorm:"not null;default:0" json:"autolock_minutes"`
	TrashRetentionDays int       `gorm:"not null;default:0" json:"trash_retention_days"`
	UpdatedAt          time.Time `json:"updated_at"`
}

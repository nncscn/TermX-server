package repository

import (
	"context"
	"errors"

	"ky/internal/model"

	"gorm.io/gorm"
)

// PreferenceRepository 偏好设置表数据访问对象,依赖通过构造函数注入。
type PreferenceRepository struct {
	db *gorm.DB
}

func NewPreferenceRepository(db *gorm.DB) *PreferenceRepository {
	return &PreferenceRepository{db: db}
}

func (r *PreferenceRepository) GetByAccount(ctx context.Context, accountID uint) (*model.Preference, error) {
	var p model.Preference
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// Save 保存偏好（按唯一索引 account_id 存在则更新，不存在则插入）。
func (r *PreferenceRepository) Save(ctx context.Context, p *model.Preference) error {
	return r.db.WithContext(ctx).Save(p).Error
}

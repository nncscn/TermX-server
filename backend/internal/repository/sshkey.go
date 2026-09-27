package repository

import (
	"context"
	"errors"
	"time"

	"ky/internal/model"

	"gorm.io/gorm"
)

// SshKeyRepository 密钥表数据访问对象,依赖通过构造函数注入。
type SshKeyRepository struct {
	db *gorm.DB
}

func NewSshKeyRepository(db *gorm.DB) *SshKeyRepository {
	return &SshKeyRepository{db: db}
}

func (r *SshKeyRepository) Create(ctx context.Context, k *model.SshKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

func (r *SshKeyRepository) GetByID(ctx context.Context, accountID, id uint) (*model.SshKey, error) {
	var k model.SshKey
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).First(&k, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &k, nil
}

// KeyFilter 密钥列表过滤条件。
type KeyFilter struct {
	Keyword        string
	Type           string
	IncludeDeleted bool
}

func (r *SshKeyRepository) scope(ctx context.Context, accountID uint, f KeyFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.SshKey{}).Where("account_id = ?", accountID)
	if !f.IncludeDeleted {
		q = q.Where("deleted_at IS NULL")
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("name LIKE ? OR fingerprint LIKE ? OR remark LIKE ?", kw, kw, kw)
	}
	return q
}

// List 按条件查询密钥列表（updated_at 倒序）,返回记录与总数。
func (r *SshKeyRepository) List(ctx context.Context, accountID uint, f KeyFilter) ([]model.SshKey, int64, error) {
	var total int64
	if err := r.scope(ctx, accountID, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var keys []model.SshKey
	if err := r.scope(ctx, accountID, f).Order("updated_at DESC").Find(&keys).Error; err != nil {
		return nil, 0, err
	}
	return keys, total, nil
}

func (r *SshKeyRepository) Save(ctx context.Context, k *model.SshKey) error {
	return r.db.WithContext(ctx).Save(k).Error
}

func (r *SshKeyRepository) SoftDelete(ctx context.Context, accountID, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.SshKey{}).
		Where("id = ? AND account_id = ?", id, accountID).
		Update("deleted_at", at).Error
}

func (r *SshKeyRepository) Restore(ctx context.Context, accountID, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.SshKey{}).
		Where("id = ? AND account_id = ?", id, accountID).
		Updates(map[string]any{"deleted_at": nil, "updated_at": at}).Error
}

// Purge 物理删除一条密钥（回收站彻底删除）。
func (r *SshKeyRepository) Purge(ctx context.Context, accountID, id uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND account_id = ?", id, accountID).
		Delete(&model.SshKey{}).Error
}

// PurgeTrashedBefore 物理删除软删时间早于 before 的密钥；before 为 nil 表示清空回收站
func (r *SshKeyRepository) PurgeTrashedBefore(ctx context.Context, accountID uint, before *time.Time) (int64, error) {
	q := r.db.WithContext(ctx).
		Where("account_id = ? AND deleted_at IS NOT NULL", accountID)
	if before != nil {
		q = q.Where("deleted_at < ?", *before)
	}
	res := q.Delete(&model.SshKey{})
	return res.RowsAffected, res.Error
}

func (r *SshKeyRepository) DeleteAll(ctx context.Context, accountID uint) error {
	return r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Delete(&model.SshKey{}).Error
}

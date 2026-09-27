package repository

import (
	"context"
	"errors"
	"time"

	"ky/internal/model"

	"gorm.io/gorm"
)

// CredentialRepository 凭据表数据访问对象,依赖通过构造函数注入。
type CredentialRepository struct {
	db *gorm.DB
}

func NewCredentialRepository(db *gorm.DB) *CredentialRepository {
	return &CredentialRepository{db: db}
}

func (r *CredentialRepository) Create(ctx context.Context, c *model.Credential) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// GetByID 按主键查询当前账户的凭据（含回收站中的记录）,不存在返回 ErrNotFound。
func (r *CredentialRepository) GetByID(ctx context.Context, accountID, id uint) (*model.Credential, error) {
	var c model.Credential
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// CredFilter 凭据列表过滤条件。
type CredFilter struct {
	Keyword        string
	AuthType       string
	Group          string
	IncludeDeleted bool
}

func (r *CredentialRepository) scope(ctx context.Context, accountID uint, f CredFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.Credential{}).Where("account_id = ?", accountID)
	if !f.IncludeDeleted {
		q = q.Where("deleted_at IS NULL")
	}
	if f.AuthType != "" {
		q = q.Where("auth_type = ?", f.AuthType)
	}
	if f.Group != "" {
		q = q.Where("`group` = ?", f.Group)
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("name LIKE ? OR `group` LIKE ? OR remark LIKE ?", kw, kw, kw)
	}
	return q
}

func (r *CredentialRepository) List(ctx context.Context, accountID uint, f CredFilter) ([]model.Credential, int64, error) {
	var total int64
	if err := r.scope(ctx, accountID, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var creds []model.Credential
	if err := r.scope(ctx, accountID, f).
		Order("COALESCE(last_used_at, updated_at) DESC").
		Find(&creds).Error; err != nil {
		return nil, 0, err
	}
	return creds, total, nil
}

// Save 全量保存凭据（更新路径使用）。
func (r *CredentialRepository) Save(ctx context.Context, c *model.Credential) error {
	return r.db.WithContext(ctx).Save(c).Error
}

func (r *CredentialRepository) SoftDelete(ctx context.Context, accountID, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Credential{}).
		Where("id = ? AND account_id = ?", id, accountID).
		Update("deleted_at", at).Error
}

func (r *CredentialRepository) Restore(ctx context.Context, accountID, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Credential{}).
		Where("id = ? AND account_id = ?", id, accountID).
		Updates(map[string]any{"deleted_at": nil, "updated_at": at}).Error
}

func (r *CredentialRepository) Purge(ctx context.Context, accountID, id uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND account_id = ?", id, accountID).
		Delete(&model.Credential{}).Error
}

// PurgeTrashedBefore 物理删除软删时间早于 before 的凭据；before 为 nil 表示清空回收站。
func (r *CredentialRepository) PurgeTrashedBefore(ctx context.Context, accountID uint, before *time.Time) (int64, error) {
	q := r.db.WithContext(ctx).
		Where("account_id = ? AND deleted_at IS NOT NULL", accountID)
	if before != nil {
		q = q.Where("deleted_at < ?", *before)
	}
	res := q.Delete(&model.Credential{})
	return res.RowsAffected, res.Error
}

// DeleteAll 物理删除当前账户全部凭据（含回收站,导入/恢复演示数据前清空）。
func (r *CredentialRepository) DeleteAll(ctx context.Context, accountID uint) error {
	return r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Delete(&model.Credential{}).Error
}

func (r *CredentialRepository) CountRefsGrouped(ctx context.Context, accountID uint) (map[uint]int64, error) {
	var rows []struct {
		KeyID uint
		N     int64
	}
	if err := r.db.WithContext(ctx).Model(&model.Credential{}).
		Select("key_id, COUNT(*) AS n").
		Where("account_id = ? AND deleted_at IS NULL AND key_id IS NOT NULL", accountID).
		Group("key_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[uint]int64, len(rows))
	for _, row := range rows {
		m[row.KeyID] = row.N
	}
	return m, nil
}

func (r *CredentialRepository) UpdateLastUsed(ctx context.Context, accountID, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Credential{}).
		Where("id = ? AND account_id = ?", id, accountID).
		Update("last_used_at", at).Error
}

package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"ky/internal/model"
)

type TermxRepository struct {
	db *gorm.DB
}

func NewTermxRepository(db *gorm.DB) *TermxRepository {
	return &TermxRepository{db: db}
}

// FirstAccountID 返回首个账户 ID（单用户部署的同步绑定账户）。
func (r *TermxRepository) FirstAccountID(ctx context.Context) (uint, error) {
	var acc model.Account
	if err := r.db.WithContext(ctx).Order("id ASC").First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return acc.ID, nil
}

func (r *TermxRepository) StateOf(accountID uint) (*model.TermxState, error) {
	var st model.TermxState
	if err := r.db.Where("account_id = ?", accountID).First(&st).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &st, nil
}

func (r *TermxRepository) SaveState(ctx context.Context, st *model.TermxState) error {
	return r.db.WithContext(ctx).Save(st).Error
}

// updated_at 晚于该时刻的行（增量游标；时间比较由驱动按 time.Time 归一化，
func (r *TermxRepository) CredListSince(ctx context.Context, accountID uint, since *time.Time) ([]model.Credential, error) {
	q := r.db.WithContext(ctx).Model(&model.Credential{}).Where("account_id = ?", accountID)
	if since != nil {
		q = q.Where("updated_at > ?", *since)
	}
	var rows []model.Credential
	if err := q.Order("updated_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// 推送复活）。未命中返回 ErrNotFound
func (r *TermxRepository) CredFindByClientID(ctx context.Context, accountID uint, clientID string) (*model.Credential, error) {
	var c model.Credential
	if err := r.db.WithContext(ctx).
		Where("account_id = ? AND client_id = ?", accountID, clientID).
		First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// CredDistinctGroups 派生分组全集：存活行的非空 Group 去重升序。
// ky 无独立分组表——分组随条目存在,空分组不存在（与 TermX 客户端文档对齐）。
func (r *TermxRepository) CredDistinctGroups(ctx context.Context, accountID uint) ([]string, error) {
	var groups []string
	err := r.db.WithContext(ctx).Model(&model.Credential{}).
		Where("account_id = ? AND deleted_at IS NULL AND `group` <> ''", accountID).
		Distinct().Order("`group` ASC").Pluck("`group`", &groups).Error
	return groups, err
}

func (r *TermxRepository) CredGetByID(ctx context.Context, accountID, id uint) (*model.Credential, error) {
	var c model.Credential
	if err := r.db.WithContext(ctx).
		Where("account_id = ? AND id = ?", accountID, id).
		First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// CredCreate 新建行（幂等键由调用方保证唯一；冲突时数据库报错逐条返回）
func (r *TermxRepository) CredCreate(ctx context.Context, c *model.Credential) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *TermxRepository) CredSave(ctx context.Context, c *model.Credential) error {
	return r.db.WithContext(ctx).Save(c).Error
}

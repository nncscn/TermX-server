package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"ky/internal/model"
)

// 上层据此映射 404,而无需感知 gorm 的存在。
var ErrNotFound = errors.New("记录不存在")

type AuthRepository struct {
	db *gorm.DB
}

func NewAuthRepository(db *gorm.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (r *AuthRepository) CreateAccount(ctx context.Context, account *model.Account) error {
	return r.db.WithContext(ctx).Create(account).Error
}

func (r *AuthRepository) GetByUsername(ctx context.Context, username string) (*model.Account, error) {
	var account model.Account
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *AuthRepository) GetByID(ctx context.Context, id uint) (*model.Account, error) {
	var account model.Account
	err := r.db.WithContext(ctx).First(&account, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// CountAccounts 账户总数（播种前判断表是否为空）。
func (r *AuthRepository) CountAccounts(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Account{}).Count(&n).Error
	return n, err
}

func (r *AuthRepository) SaveLoginState(ctx context.Context, account *model.Account) error {
	return r.db.WithContext(ctx).
		Model(&model.Account{}).
		Where("id = ?", account.ID).
		Updates(map[string]any{
			"failed_attempts": account.FailedAttempts,
			"locked_until":    account.LockedUntil,
		}).Error
}

func (r *AuthRepository) CreateSession(ctx context.Context, session *model.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *AuthRepository) GetValidSession(ctx context.Context, tokenHash string) (*model.Session, error) {
	var session model.Session
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ?", tokenHash, time.Now()).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// VaultUpdate 账户密钥材料的部分更新（零知识结构各层密文/哈希/盐）
type VaultUpdate struct {
	AuthSalt       []byte
	AuthHash       []byte
	VaultSalt      []byte
	VaultCipher    []byte
	RecoveryCipher []byte
	RecoveryHash   string
	ClearLegacy    bool // 清空旧架构 PasswordCipher（迁移完成）
}

func (r *AuthRepository) UpdateVault(ctx context.Context, accountID uint, u VaultUpdate) error {
	updates := map[string]any{}
	if u.AuthSalt != nil {
		updates["auth_salt"] = u.AuthSalt
	}
	if u.AuthHash != nil {
		updates["auth_hash"] = u.AuthHash
	}
	if u.VaultSalt != nil {
		updates["vault_salt"] = u.VaultSalt
	}
	if u.VaultCipher != nil {
		updates["vault_cipher"] = u.VaultCipher
	}
	if u.RecoveryCipher != nil {
		updates["recovery_cipher"] = u.RecoveryCipher
	}
	if u.RecoveryHash != "" {
		updates["recovery_hash"] = u.RecoveryHash
	}
	if u.ClearLegacy {
		updates["password_cipher"] = nil
	}
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&model.Account{}).
		Where("id = ?", accountID).
		Updates(updates).Error
}

// UpdateRecovery 更新恢复密钥（包裹密文 + 哈希）。
func (r *AuthRepository) UpdateRecovery(ctx context.Context, accountID uint, cipher []byte, hash string) error {
	return r.db.WithContext(ctx).
		Model(&model.Account{}).
		Where("id = ?", accountID).
		Updates(map[string]any{"recovery_cipher": cipher, "recovery_hash": hash}).Error
}

func (r *AuthRepository) DeleteSessionsByAccount(ctx context.Context, accountID uint) error {
	return r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Delete(&model.Session{}).Error
}

// DeleteSessionByToken 登出时按令牌哈希删除会话。
func (r *AuthRepository) DeleteSessionByToken(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		Delete(&model.Session{}).Error
}

// DeleteSessionsByAccountExcept 删除账户全部会话但保留指定哈希的会话（改密踢旧设备）
func (r *AuthRepository) DeleteSessionsByAccountExcept(ctx context.Context, accountID uint, keepHash string) error {
	return r.db.WithContext(ctx).
		Where("account_id = ? AND token_hash <> ?", accountID, keepHash).
		Delete(&model.Session{}).Error
}

func (r *AuthRepository) SaveRecoveryState(ctx context.Context, account *model.Account) error {
	return r.db.WithContext(ctx).
		Model(&model.Account{}).
		Where("id = ?", account.ID).
		Updates(map[string]any{
			"recovery_attempts":     account.RecoveryAttempts,
			"recovery_locked_until": account.RecoveryLockedUntil,
		}).Error
}

// ExtendSession 滑动续期：把会话过期时间更新为指定时刻（只延长）。
func (r *AuthRepository) ExtendSession(ctx context.Context, id uint, expiresAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.Session{}).
		Where("id = ?", id).
		Update("expires_at", expiresAt).Error
}

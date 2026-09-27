package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ky/internal/model"
	"ky/internal/pkg/vault"
	"ky/internal/repository"
)

var (
	ErrKeyNotFound = errors.New("密钥不存在")
)

// KeySecret 密钥内容的密文层：整体序列化后加密入库，列表接口不下发。
type KeySecret struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
}

type KeyInfo struct {
	model.SshKey
	RefCount   int64  `json:"ref_count"`
	SecretBlob string `json:"secret_blob"` // 客户端加密的密文（Base64），随元信息下发供本地解密
}

// KeyService 密钥业务逻辑。密文层用账户库密钥（零知识）加解密：
type KeyService struct {
	repo *repository.SshKeyRepository
	refs *repository.CredentialRepository
	auth *AuthService
}

func NewKeyService(repo *repository.SshKeyRepository, refs *repository.CredentialRepository, auth *AuthService) *KeyService {
	return &KeyService{repo: repo, refs: refs, auth: auth}
}

// List 查询密钥列表（元信息 + 被引用数，不含密文）。
func (s *KeyService) List(ctx context.Context, accountID uint, keyword, typ string, includeDeleted bool) ([]KeyInfo, error) {
	keys, _, err := s.repo.List(ctx, accountID, repository.KeyFilter{
		Keyword: keyword, Type: typ, IncludeDeleted: includeDeleted,
	})
	if err != nil {
		return nil, fmt.Errorf("查询密钥列表: %w", err)
	}
	refs, err := s.refs.CountRefsGrouped(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("统计密钥引用: %w", err)
	}
	items := make([]KeyInfo, 0, len(keys))
	for i := range keys {
		items = append(items, KeyInfo{
			SshKey:     keys[i],
			RefCount:   refs[keys[i].ID],
			SecretBlob: base64.StdEncoding.EncodeToString(keys[i].Secret),
		})
	}
	return items, nil
}

// Create 新建密钥：secret 为客户端加密的密文，原样入库（服务端不解开）；
func (s *KeyService) Create(ctx context.Context, accountID uint, name, typ, fingerprintVal string, secret []byte, remark string) (*model.SshKey, error) {
	k := &model.SshKey{
		AccountID:   accountID,
		Name:        name,
		Type:        typ,
		Fingerprint: fingerprintVal,
		Secret:      secret,
		Remark:      remark,
	}
	if err := s.repo.Create(ctx, k); err != nil {
		return nil, fmt.Errorf("创建密钥: %w", err)
	}
	return k, nil
}

func (s *KeyService) Update(ctx context.Context, accountID, id uint, name, typ, fingerprintVal string, secret []byte, remark string) (*model.SshKey, error) {
	k, err := s.repo.GetByID(ctx, accountID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("查询密钥: %w", err)
	}
	k.Name, k.Type, k.Remark = name, typ, remark
	k.Secret = secret
	k.Fingerprint = fingerprintVal
	if err := s.repo.Save(ctx, k); err != nil {
		return nil, fmt.Errorf("保存密钥: %w", err)
	}
	return k, nil
}

func (s *KeyService) SoftDelete(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrKeyNotFound
		}
		return fmt.Errorf("查询密钥: %w", err)
	}
	if err := s.repo.SoftDelete(ctx, accountID, id, time.Now()); err != nil {
		return fmt.Errorf("删除密钥: %w", err)
	}
	return nil
}

func (s *KeyService) Restore(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrKeyNotFound
		}
		return fmt.Errorf("查询密钥: %w", err)
	}
	if err := s.repo.Restore(ctx, accountID, id, time.Now()); err != nil {
		return fmt.Errorf("恢复密钥: %w", err)
	}
	return nil
}

func (s *KeyService) Purge(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrKeyNotFound
		}
		return fmt.Errorf("查询密钥: %w", err)
	}
	if err := s.repo.Purge(ctx, accountID, id); err != nil {
		return fmt.Errorf("彻底删除密钥: %w", err)
	}
	return nil
}

func (s *KeyService) Reveal(ctx context.Context, accountID, id uint, password string) (*KeySecret, error) {
	k, err := s.repo.GetByID(ctx, accountID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("查询密钥: %w", err)
	}
	vk, err := s.auth.VerifyPassword(ctx, accountID, password)
	if err != nil {
		return nil, err
	}
	sec, err := s.decryptSecret(vk, k.Secret)
	if err != nil {
		return nil, err
	}
	return &sec, nil
}

func (s *KeyService) decryptSecret(vk, blob []byte) (KeySecret, error) {
	var sec KeySecret
	raw, err := vault.Open(vk, blob)
	if err != nil {
		return sec, fmt.Errorf("解密密钥内容: %w", err)
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return sec, fmt.Errorf("解析密钥内容: %w", err)
	}
	return sec, nil
}

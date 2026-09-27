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
	ErrCredNotFound = errors.New("凭据不存在")
)

// CredSecret 凭据连接信息的密文层：整体序列化后加密入库，列表接口不下发。
type CredSecret struct {
	Host       string      `json:"host" binding:"required,max=255"`
	Port       *int        `json:"port" binding:"omitempty,min=1,max=65535"`
	Username   string      `json:"username" binding:"max=64"`
	Password   string      `json:"password" binding:"max=128"`
	KeyContent string      `json:"key_content" binding:"max=16384"` // 密钥认证时客户端上传的密钥字符串
	Proxy      ProxySecret `json:"proxy"`
	Serial     *SerialConf `json:"serial"`
}

type ProxySecret struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// SerialConf 串口参数（协议为 serial 时使用）
type SerialConf struct {
	BaudRate int    `json:"baud_rate"`
	DataBits int    `json:"data_bits"`
	StopBits int    `json:"stop_bits"`
	Parity   string `json:"parity"`
}

// CredInput 凭据写入参数（创建与更新共用）。Secret 为客户端加密的密文,原样入库。
type CredInput struct {
	Name     string
	Protocol string
	AuthType string
	Group    string
	KeyID    *uint
	Secret   []byte
	Remark   string
}

// CredentialService 凭据业务逻辑。密文层用账户库密钥（零知识）加解密。
type CredentialService struct {
	repo   *repository.CredentialRepository
	keys   *repository.SshKeyRepository
	auth   *AuthService
	bridge func(accountID uint, c *model.Credential) // TermX 深融合桥：Web 改密后反译 TermX 视图（router 装配注入）
}

func NewCredentialService(repo *repository.CredentialRepository, keys *repository.SshKeyRepository, auth *AuthService) *CredentialService {
	return &CredentialService{repo: repo, keys: keys, auth: auth}
}

// SetTermxBridge 注入 TermX 反向翻译钩子（由 router 在装配期调用，避免服务间循环依赖）。
func (s *CredentialService) SetTermxBridge(fn func(accountID uint, c *model.Credential)) {
	s.bridge = fn
}

type CredItem struct {
	model.Credential
	SecretBlob string `json:"secret_blob"`
}

func (s *CredentialService) List(ctx context.Context, accountID uint, keyword, authType, group string, includeDeleted bool) ([]CredItem, error) {
	creds, _, err := s.repo.List(ctx, accountID, repository.CredFilter{
		Keyword: keyword, AuthType: authType, Group: group, IncludeDeleted: includeDeleted,
	})
	if err != nil {
		return nil, fmt.Errorf("查询凭据列表: %w", err)
	}
	items := make([]CredItem, 0, len(creds))
	for i := range creds {
		items = append(items, CredItem{
			Credential: creds[i],
			SecretBlob: base64.StdEncoding.EncodeToString(creds[i].Secret),
		})
	}
	return items, nil
}

// Create 新建凭据：连接信息加密入库；密钥认证时校验关联密钥存在。
func (s *CredentialService) Create(ctx context.Context, accountID uint, in CredInput) (*model.Credential, error) {
	if err := s.ensureKeyExists(ctx, accountID, in.KeyID); err != nil {
		return nil, err
	}
	c := &model.Credential{
		AccountID: accountID,
		Name:      in.Name,
		Protocol:  in.Protocol,
		AuthType:  in.AuthType,
		Group:     in.Group,
		KeyID:     in.KeyID,
		Secret:    in.Secret, // 客户端加密密文，原样入库
		Remark:    in.Remark,
	}
	if err := s.repo.Create(ctx, c); err != nil {
		return nil, fmt.Errorf("创建凭据: %w", err)
	}
	if s.bridge != nil && len(c.Secret) > 0 {
		s.bridge(accountID, c)
		if c.TermxSecret != nil {
			if err := s.repo.Save(ctx, c); err != nil {
				c.TermxSecret = nil // 补写失败按无视图处理，不改报错语义
			}
		}
	}
	return c, nil
}

// Update 全量更新凭据（重加密连接信息，校验密钥引用）
func (s *CredentialService) Update(ctx context.Context, accountID, id uint, in CredInput) (*model.Credential, error) {
	c, err := s.repo.GetByID(ctx, accountID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCredNotFound
		}
		return nil, fmt.Errorf("查询凭据: %w", err)
	}
	if err := s.ensureKeyExists(ctx, accountID, in.KeyID); err != nil {
		return nil, err
	}
	c.Name, c.Protocol, c.AuthType, c.Group = in.Name, in.Protocol, in.AuthType, in.Group
	c.KeyID, c.Remark = in.KeyID, in.Remark
	if in.Secret != nil {
		c.Secret = in.Secret // 密文原样保存；缺省（元数据编辑）保留原密文
		if s.bridge != nil {
			s.bridge(accountID, c)
		}
	}
	if err := s.repo.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("保存凭据: %w", err)
	}
	return c, nil
}

func (s *CredentialService) SoftDelete(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCredNotFound
		}
		return fmt.Errorf("查询凭据: %w", err)
	}
	if err := s.repo.SoftDelete(ctx, accountID, id, time.Now()); err != nil {
		return fmt.Errorf("删除凭据: %w", err)
	}
	return nil
}

func (s *CredentialService) Restore(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCredNotFound
		}
		return fmt.Errorf("查询凭据: %w", err)
	}
	if err := s.repo.Restore(ctx, accountID, id, time.Now()); err != nil {
		return fmt.Errorf("恢复凭据: %w", err)
	}
	return nil
}

func (s *CredentialService) Purge(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCredNotFound
		}
		return fmt.Errorf("查询凭据: %w", err)
	}
	if err := s.repo.Purge(ctx, accountID, id); err != nil {
		return fmt.Errorf("彻底删除凭据: %w", err)
	}
	return nil
}

func (s *CredentialService) Reveal(ctx context.Context, accountID, id uint, password string) (*CredSecret, error) {
	c, err := s.repo.GetByID(ctx, accountID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCredNotFound
		}
		return nil, fmt.Errorf("查询凭据: %w", err)
	}
	vk, err := s.auth.VerifyPassword(ctx, accountID, password)
	if err != nil {
		return nil, err
	}
	sec, err := s.decryptSecret(vk, c.Secret)
	if err != nil {
		return nil, err
	}
	return &sec, nil
}

// Touch 标记最后使用时间（连接/复制密码等动作触发）。
func (s *CredentialService) Touch(ctx context.Context, accountID, id uint) error {
	if _, err := s.repo.GetByID(ctx, accountID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCredNotFound
		}
		return fmt.Errorf("查询凭据: %w", err)
	}
	if err := s.repo.UpdateLastUsed(ctx, accountID, id, time.Now()); err != nil {
		return fmt.Errorf("标记使用时间: %w", err)
	}
	return nil
}

// ensureKeyExists 认证方式为密钥时校验关联密钥存在且属于当前账户。
func (s *CredentialService) ensureKeyExists(ctx context.Context, accountID uint, keyID *uint) error {
	if keyID == nil || *keyID == 0 {
		return nil
	}
	if _, err := s.keys.GetByID(ctx, accountID, *keyID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrKeyNotFound
		}
		return fmt.Errorf("查询关联密钥: %w", err)
	}
	return nil
}

func (s *CredentialService) decryptSecret(vk, blob []byte) (CredSecret, error) {
	var sec CredSecret
	raw, err := vault.Open(vk, blob)
	if err != nil {
		return sec, fmt.Errorf("解密连接信息: %w", err)
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return sec, fmt.Errorf("解析连接信息: %w", err)
	}
	return sec, nil
}

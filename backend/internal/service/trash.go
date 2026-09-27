package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ky/internal/model"
	"ky/internal/repository"
)

// 回收站条目类型（对外 kind 字段取值）
const (
	TrashKindKey  = "key"
	TrashKindCred = "credential"
)

type TrashItem struct {
	Kind      string    `json:"kind"` // key=密钥 credential=凭据
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	DeletedAt time.Time `json:"deleted_at"`
}

// TrashService 回收站业务逻辑。
type TrashService struct {
	keys  *repository.SshKeyRepository
	creds *repository.CredentialRepository
}

func NewTrashService(keys *repository.SshKeyRepository, creds *repository.CredentialRepository) *TrashService {
	return &TrashService{keys: keys, creds: creds}
}

func (s *TrashService) List(ctx context.Context, accountID uint) ([]TrashItem, error) {
	keys, _, err := s.keys.List(ctx, accountID, repository.KeyFilter{IncludeDeleted: true})
	if err != nil {
		return nil, fmt.Errorf("查询回收站密钥: %w", err)
	}
	creds, _, err := s.creds.List(ctx, accountID, repository.CredFilter{IncludeDeleted: true})
	if err != nil {
		return nil, fmt.Errorf("查询回收站凭据: %w", err)
	}
	items := make([]TrashItem, 0, len(keys)+len(creds))
	for _, k := range keys {
		if k.DeletedAt != nil {
			items = append(items, TrashItem{
				Kind: TrashKindKey, ID: k.ID, Name: k.Name,
				Summary: keySummary(k), DeletedAt: *k.DeletedAt,
			})
		}
	}
	for _, c := range creds {
		if c.DeletedAt != nil {
			items = append(items, TrashItem{
				Kind: TrashKindCred, ID: c.ID, Name: c.Name,
				Summary: credSummary(c), DeletedAt: *c.DeletedAt,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.After(items[j].DeletedAt) })
	return items, nil
}

func (s *TrashService) Empty(ctx context.Context, accountID uint) (int, error) {
	n1, err := s.keys.PurgeTrashedBefore(ctx, accountID, nil)
	if err != nil {
		return 0, fmt.Errorf("清空回收站密钥: %w", err)
	}
	n2, err := s.creds.PurgeTrashedBefore(ctx, accountID, nil)
	if err != nil {
		return 0, fmt.Errorf("清空回收站凭据: %w", err)
	}
	return int(n1 + n2), nil
}

func (s *TrashService) Clean(ctx context.Context, accountID uint, days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	before := time.Now().AddDate(0, 0, -days)
	n1, err := s.keys.PurgeTrashedBefore(ctx, accountID, &before)
	if err != nil {
		return 0, fmt.Errorf("清理过期密钥: %w", err)
	}
	n2, err := s.creds.PurgeTrashedBefore(ctx, accountID, &before)
	if err != nil {
		return 0, fmt.Errorf("清理过期凭据: %w", err)
	}
	return int(n1 + n2), nil
}

func keySummary(k model.SshKey) string {
	fp := k.Fingerprint
	if len(fp) > 20 {
		fp = fp[:20] + "…"
	}
	return fmt.Sprintf("%s · %s", k.Type, fp)
}

// credSummary 回收站摘要：协议 · 分组（无分组显示未分组）。
func credSummary(c model.Credential) string {
	g := c.Group
	if g == "" {
		g = "未分组"
	}
	return fmt.Sprintf("%s · %s", strings.ToUpper(c.Protocol), g)
}

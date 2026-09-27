package service

import (
	"context"
	"errors"
	"fmt"

	"ky/internal/model"
	"ky/internal/repository"
)

var ErrPreferenceInvalid = errors.New("偏好取值不合法")

// 偏好取值域（与前端设置页下拉一致）
var (
	allowedStartPages      = []string{"/dashboard", "/keys", "/credentials"}
	allowedClipboardClear  = []int{0, 10, 30, 60}
	allowedAutolockMinutes = []int{0, 5, 15, 30}
	allowedRetentionDays   = []int{0, 7, 30, 90}
)

type PreferenceService struct {
	repo *repository.PreferenceRepository
}

func NewPreferenceService(repo *repository.PreferenceRepository) *PreferenceService {
	return &PreferenceService{repo: repo}
}

type PreferenceInput struct {
	StartPage          string
	ClipboardClear     int
	AutolockMinutes    int
	TrashRetentionDays int
}

func (s *PreferenceService) Get(ctx context.Context, accountID uint) (*model.Preference, error) {
	p, err := s.repo.GetByAccount(ctx, accountID)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("查询偏好: %w", err)
	}
	p = &model.Preference{
		AccountID:          accountID,
		StartPage:          "/dashboard",
		ClipboardClear:     0,
		AutolockMinutes:    0,
		TrashRetentionDays: 0,
	}
	if err := s.repo.Save(ctx, p); err != nil {
		return nil, fmt.Errorf("初始化偏好: %w", err)
	}
	return p, nil
}

func (s *PreferenceService) Update(ctx context.Context, accountID uint, in PreferenceInput) (*model.Preference, error) {
	if !strIn(allowedStartPages, in.StartPage) ||
		!intIn(allowedClipboardClear, in.ClipboardClear) ||
		!intIn(allowedAutolockMinutes, in.AutolockMinutes) ||
		!intIn(allowedRetentionDays, in.TrashRetentionDays) {
		return nil, ErrPreferenceInvalid
	}
	p, err := s.Get(ctx, accountID)
	if err != nil {
		return nil, err
	}
	p.StartPage = in.StartPage
	p.ClipboardClear = in.ClipboardClear
	p.AutolockMinutes = in.AutolockMinutes
	p.TrashRetentionDays = in.TrashRetentionDays
	if err := s.repo.Save(ctx, p); err != nil {
		return nil, fmt.Errorf("保存偏好: %w", err)
	}
	return p, nil
}

func strIn(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// intIn 判断取值是否在枚举内。
func intIn(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

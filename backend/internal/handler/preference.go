package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type PreferenceHandler struct {
	svc *service.PreferenceService
}

func NewPreferenceHandler(svc *service.PreferenceService) *PreferenceHandler {
	return &PreferenceHandler{svc: svc}
}

type SavePrefsReq struct {
	StartPage          string `json:"start_page" binding:"required,max=16"`
	ClipboardClear     int    `json:"clipboard_clear" binding:"min=0,max=3600"`
	AutolockMinutes    int    `json:"autolock_minutes" binding:"min=0,max=1440"`
	TrashRetentionDays int    `json:"trash_retention_days" binding:"min=0,max=365"`
}

func (h *PreferenceHandler) Get(c *gin.Context) {
	accountID := c.GetUint("account_id")
	p, err := h.svc.Get(c.Request.Context(), accountID)
	if err != nil {
		response.ServerError(c, "读取设置失败")
		return
	}
	response.Success(c, p)
}

// Update 保存偏好 PUT /api/v1/settings
func (h *PreferenceHandler) Update(c *gin.Context) {
	var req SavePrefsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	p, err := h.svc.Update(c.Request.Context(), accountID, service.PreferenceInput{
		StartPage:          req.StartPage,
		ClipboardClear:     req.ClipboardClear,
		AutolockMinutes:    req.AutolockMinutes,
		TrashRetentionDays: req.TrashRetentionDays,
	})
	if err != nil {
		if errors.Is(err, service.ErrPreferenceInvalid) {
			response.ParamError(c, service.ErrPreferenceInvalid.Error())
			return
		}
		response.ServerError(c, "保存设置失败")
		return
	}
	response.Success(c, p)
}

package handler

import (
	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type TrashHandler struct {
	svc *service.TrashService
}

func NewTrashHandler(svc *service.TrashService) *TrashHandler {
	return &TrashHandler{svc: svc}
}

// trashCleanReq 按保留天数清理过期条目
type trashCleanReq struct {
	Days int `json:"days" binding:"required,min=1,max=3650"`
}

// List 回收站列表（密钥+凭据合并） GET /api/v1/trash
func (h *TrashHandler) List(c *gin.Context) {
	accountID := c.GetUint("account_id")
	items, err := h.svc.List(c.Request.Context(), accountID)
	if err != nil {
		response.ServerError(c, "查询回收站失败")
		return
	}
	response.Success(c, gin.H{"list": items, "total": len(items)})
}

// Empty 清空回收站 POST /api/v1/trash/empty
func (h *TrashHandler) Empty(c *gin.Context) {
	accountID := c.GetUint("account_id")
	n, err := h.svc.Empty(c.Request.Context(), accountID)
	if err != nil {
		response.ServerError(c, "清空回收站失败")
		return
	}
	response.Success(c, gin.H{"purged": n})
}

// Clean 清理超过保留天数的条目 POST /api/v1/trash/clean
func (h *TrashHandler) Clean(c *gin.Context) {
	var req trashCleanReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	n, err := h.svc.Clean(c.Request.Context(), accountID, req.Days)
	if err != nil {
		response.ServerError(c, "清理回收站失败")
		return
	}
	response.Success(c, gin.H{"purged": n})
}

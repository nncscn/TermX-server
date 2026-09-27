package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type DataHandler struct {
	svc *service.DataService
}

func NewDataHandler(svc *service.DataService) *DataHandler {
	return &DataHandler{svc: svc}
}

// 凭据 key_id 引用包内密钥条目的 id，服务端导入时自动重映射。
type ImportReq struct {
	Password    string                   `json:"password" binding:"required,min=1,max=128"`
	Keys        []service.ImportKeyItem  `json:"keys" binding:"required,dive"`
	Credentials []service.ImportCredItem `json:"credentials" binding:"required,dive"`
}

func (h *DataHandler) Export(c *gin.Context) {
	var req RevealReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	keys, creds, err := h.svc.ExportBackup(c.Request.Context(), accountID, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrVerifyLocked):
			response.TooFrequent(c, service.ErrVerifyLocked.Error())
		case errors.Is(err, service.ErrPasswordWrong):
			response.PasswordWrong(c, service.ErrPasswordWrong.Error())
		default:
			response.ServerError(c, "导出数据失败")
		}
		return
	}
	response.Success(c, gin.H{"keys": keys, "credentials": creds})
}

func (h *DataHandler) Import(c *gin.Context) {
	var req ImportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	nKeys, nCreds, err := h.svc.ImportBackup(c.Request.Context(), accountID, req.Password, req.Keys, req.Credentials)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrVerifyLocked):
			response.TooFrequent(c, service.ErrVerifyLocked.Error())
		case errors.Is(err, service.ErrPasswordWrong):
			response.PasswordWrong(c, service.ErrPasswordWrong.Error())
		default:
			response.ServerError(c, "导入数据失败")
		}
		return
	}
	response.Success(c, gin.H{"keys": nKeys, "credentials": nCreds})
}

// 验主密码后保存（自动建库 + 自动重启）、撤销待生效变更
package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type DbConfigHandler struct {
	svc *service.DbConfigService
}

func NewDbConfigHandler(svc *service.DbConfigService) *DbConfigHandler {
	return &DbConfigHandler{svc: svc}
}

type DbConfigReq struct {
	Engine         string `json:"engine" binding:"required,oneof=sqlite mysql postgresql sqlserver"`
	Host           string `json:"host" binding:"max=255"`
	Port           int    `json:"port" binding:"omitempty,min=1,max=65535"`
	User           string `json:"user" binding:"max=64"`
	Password       string `json:"password" binding:"max=128"`
	Name           string `json:"name" binding:"max=64"`
	File           string `json:"file" binding:"max=255"` // sqlite 数据文件路径
	MasterPassword string `json:"master_password" binding:"omitempty,max=128"`
	MigrateData    bool   `json:"migrate_data"`
}

func (r DbConfigReq) toInput() service.DbConnInput {
	return service.DbConnInput{
		Engine: r.Engine, Host: r.Host, Port: r.Port,
		User: r.User, Password: r.Password, Name: r.Name, File: r.File,
		Migrate: r.MigrateData,
	}
}

// errResp 统一错误映射（测试/保存/撤销共用）
func dbCfgErrResp(c *gin.Context, err error, action string) {
	var conflict *service.DbNameConflict
	switch {
	case errors.As(err, &conflict):
		response.Conflict(c, "目标库已存在，已自动避开同名库",
			gin.H{"requested_name": conflict.Requested, "suggested_name": conflict.Suggested})
	case errors.Is(err, service.ErrPasswordWrong):
		response.PasswordWrong(c, service.ErrPasswordWrong.Error())
	case errors.Is(err, service.ErrVerifyLocked), errors.Is(err, service.ErrRestartCooldown):
		response.TooFrequent(c, err.Error())
	case errors.Is(err, service.ErrDbCfgInvalid), errors.Is(err, service.ErrDbSameTarget):
		response.ParamError(c, err.Error())
	case errors.Is(err, service.ErrDbConnect):
		response.ConnectError(c, err.Error())
	default:
		response.ServerError(c, action+"失败")
	}
}

func (h *DbConfigHandler) Get(c *gin.Context) {
	cfg, err := h.svc.Get()
	if err != nil {
		response.ServerError(c, "读取数据库配置失败")
		return
	}
	response.Success(c, cfg)
}

// Test 真实连通测试（不建库、不落盘） POST /api/v1/settings/database/test
func (h *DbConfigHandler) Test(c *gin.Context) {
	var req DbConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.Test(req.toInput()); err != nil {
		dbCfgErrResp(c, err, "测试连接")
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "连接成功"})
}

// Save 验主密码保存并切换：自动建库 → 连通测试 → 备份写回 config.yaml → 进程自重启。
// PUT /api/v1/settings/database
func (h *DbConfigHandler) Save(c *gin.Context) {
	var req DbConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if req.MasterPassword == "" {
		response.ParamError(c, "请输入主密码")
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Save(c.Request.Context(), accountID, req.toInput(), req.MasterPassword); err != nil {
		dbCfgErrResp(c, err, "保存数据库设置")
		return
	}
	response.Success(c, gin.H{
		"saved":      true,
		"restarting": true,
		"message":    "已保存并自动重启后端（约 3~10 秒），完成后自动连接新库",
	})
}

type RevertReq struct {
	MasterPassword string `json:"master_password" binding:"required,min=1,max=128"`
}

func (h *DbConfigHandler) Revert(c *gin.Context) {
	var req RevertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Revert(c.Request.Context(), accountID, req.MasterPassword); err != nil {
		dbCfgErrResp(c, err, "撤销变更")
		return
	}
	response.Success(c, gin.H{"saved": true, "message": "已撤销待生效变更，恢复为当前运行配置"})
}

type CheckPortReq struct {
	Port int `json:"port" binding:"required,min=1,max=65535"`
}

func (h *DbConfigHandler) CheckPort(c *gin.Context) {
	var req CheckPortReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	occupied, detail := h.svc.CheckPort(req.Port)
	response.Success(c, gin.H{"port": req.Port, "occupied": occupied, "message": detail})
}

package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type SetupHandler struct {
	svc *service.SetupService
}

func NewSetupHandler(svc *service.SetupService) *SetupHandler {
	return &SetupHandler{svc: svc}
}

// SetupStatusResp GET /api/v1/setup/status 的响应字段见 service
// Status 查询初始化状态 GET /api/v1/setup/status
func (h *SetupHandler) Status(c *gin.Context) {
	configured, err := h.svc.Status(c.Request.Context())
	if err != nil {
		response.ServerError(c, "查询初始化状态失败")
		return
	}
	response.Success(c, gin.H{"configured": configured})
}

type SetupDbReq struct {
	Engine   string `json:"engine" binding:"required,oneof=sqlite mysql postgresql sqlserver"`
	Host     string `json:"host" binding:"max=255"`
	Port     int    `json:"port" binding:"omitempty,min=1,max=65535"`
	User     string `json:"user" binding:"max=64"`
	Password string `json:"password" binding:"max=128"`
	Name     string `json:"name" binding:"max=64"`
	File     string `json:"file" binding:"max=255"`
}

func (r SetupDbReq) toInput() service.DbConnInput {
	return service.DbConnInput{
		Engine: r.Engine, Host: r.Host, Port: r.Port,
		User: r.User, Password: r.Password, Name: r.Name, File: r.File,
	}
}

// SetupServerReq 服务配置参数（测试与 complete 共用）。
type SetupServerReq struct {
	Host     string `json:"host" binding:"required,max=255"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	HTTPS    bool   `json:"https"`
	CertFile string `json:"cert_file" binding:"max=255"`
	KeyFile  string `json:"key_file" binding:"max=255"`
}

func (r SetupServerReq) toInput() service.ServerInput {
	return service.ServerInput{
		Host: r.Host, Port: r.Port, HTTPS: r.HTTPS,
		CertFile: r.CertFile, KeyFile: r.KeyFile,
	}
}

// TestConnection 数据库连通测试 POST /api/v1/setup/test-connection
// （外部引擎验服务器连通 + 建库权限预警；目标库在 complete 时自动创建）
func (h *SetupHandler) TestConnection(c *gin.Context) {
	if !h.sameOriginGuard(c) {
		return
	}
	var req SetupDbReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	info, err := h.svc.TestConnection(c.Request.Context(), req.toInput())
	if err != nil {
		h.mapErr(c, err, "测试连接")
		return
	}
	response.Success(c, info)
}

// TestServer 服务配置可行性验证 POST /api/v1/setup/test-server
func (h *SetupHandler) TestServer(c *gin.Context) {
	if !h.sameOriginGuard(c) {
		return
	}
	var req SetupServerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if err := h.svc.TestServer(c.Request.Context(), req.toInput()); err != nil {
		h.mapErr(c, err, "验证服务配置")
		return
	}
	message := "监听地址可用"
	if req.HTTPS {
		message = "监听地址可用，证书校验通过"
	}
	response.Success(c, gin.H{"ok": true, "message": message})
}

// 留空时服务端自动随机生成——主密钥日常由系统自动管理，无需用户经手；
type SetupCompleteReq struct {
	Database      SetupDbReq     `json:"database" binding:"required"`
	Server        SetupServerReq `json:"server" binding:"required"`
	TermxBasePath string         `json:"termx_base_path" binding:"omitempty,max=33"`
	Username      string         `json:"username" binding:"required,min=2,max=64"`
	Password      string         `json:"password" binding:"required,min=8,max=128"`
	MasterKey     string         `json:"master_key" binding:"omitempty,len=43"`
}

// Complete 执行安装 POST /api/v1/setup/complete（响应先行返回，重启后台进行）
func (h *SetupHandler) Complete(c *gin.Context) {
	if !h.sameOriginGuard(c) {
		return
	}
	var req SetupCompleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	in := service.SetupInput{
		Database:  req.Database.toInput(),
		Server:    req.Server.toInput(),
		TermxPath: req.TermxBasePath,
		Username:  req.Username,
		Password:  req.Password,
		MasterKey: req.MasterKey,
	}
	result, err := h.svc.Complete(c.Request.Context(), in)
	if err != nil {
		h.mapErr(c, err, "初始化")
		return
	}
	response.Success(c, result)
}

func (h *SetupHandler) mapErr(c *gin.Context, err error, action string) {
	var conflict *service.DbNameConflict
	switch {
	case errors.As(err, &conflict):
		response.Conflict(c, conflict.Error(),
			gin.H{"requested_name": conflict.Requested, "suggested_name": conflict.Suggested})
	case errors.Is(err, service.ErrSetupDone):
		response.Forbidden(c, err.Error())
	case errors.Is(err, service.ErrSetupParam), errors.Is(err, service.ErrDbCfgInvalid),
		errors.Is(err, service.ErrDbSameTarget):
		response.ParamError(c, err.Error())
	case errors.Is(err, service.ErrSetupPort), errors.Is(err, service.ErrSetupCert),
		errors.Is(err, service.ErrDbConnect):
		response.ConnectError(c, err.Error())
	default:
		response.ServerError(c, action+"失败")
	}
}

// sameOriginGuard 同源绑定：安装引导的敏感接口（测试/完成）要求请求来自
// 无 Origin 的直接调用（curl/脚本）与跨源来源一律拒绝,封死跨机抢注与
// 无浏览器的 SSRF。零感知：正常安装流程不增加任何输入。
func (h *SetupHandler) sameOriginGuard(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	if origin == "" {
		response.Fail(c, http.StatusUnauthorized, 40101, "非法的安装请求来源")
		return false
	}
	host := c.Request.Host
	for _, prefix := range []string{"http://" + host, "https://" + host} {
		if origin == prefix {
			return true
		}
	}
	response.Fail(c, http.StatusUnauthorized, 40101, "非法的安装请求来源")
	return false
}

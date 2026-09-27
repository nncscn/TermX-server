package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type CredentialHandler struct {
	svc *service.CredentialService
}

func NewCredentialHandler(svc *service.CredentialService) *CredentialHandler {
	return &CredentialHandler{svc: svc}
}

type ListCredsReq struct {
	Keyword        string `form:"keyword"`
	AuthType       string `form:"auth_type"`
	Group          string `form:"group"`
	IncludeDeleted bool   `form:"include_deleted"`
}

// 密文层分离：SecretBlob 为客户端 AES-256-GCM 加密的密文（Base64），原样入库。
// 客户端端到端加密，Web 端解不开也无须重传；Create 必填（见各 handler）。
type SaveCredReq struct {
	Name       string `json:"name" binding:"required,min=2,max=40"`
	Protocol   string `json:"protocol" binding:"required,oneof=ssh rdp vnc telnet serial"`
	AuthType   string `json:"auth_type" binding:"required,oneof=password key"`
	Group      string `json:"group" binding:"max=40"`
	KeyID      *uint  `json:"key_id"`
	SecretBlob string `json:"secret_blob" binding:"omitempty,min=28"`
	Remark     string `json:"remark" binding:"max=120"`
}

func (r SaveCredReq) toInput() (service.CredInput, error) {
	if r.SecretBlob == "" {
		return service.CredInput{
			Name: r.Name, Protocol: r.Protocol, AuthType: r.AuthType,
			Group: r.Group, KeyID: r.KeyID, Secret: nil, Remark: r.Remark,
		}, nil
	}
	blob, err := decodeSecretBlob(r.SecretBlob)
	if err != nil {
		return service.CredInput{}, err
	}
	return service.CredInput{
		Name: r.Name, Protocol: r.Protocol, AuthType: r.AuthType,
		Group: r.Group, KeyID: r.KeyID, Secret: blob, Remark: r.Remark,
	}, nil
}

// List 查询凭据列表 GET /api/v1/credentials
func (h *CredentialHandler) List(c *gin.Context) {
	var req ListCredsReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	items, err := h.svc.List(c.Request.Context(), accountID, req.Keyword, req.AuthType, req.Group, req.IncludeDeleted)
	if err != nil {
		response.ServerError(c, "查询凭据失败")
		return
	}
	response.Success(c, gin.H{"list": items, "total": len(items)})
}

func (h *CredentialHandler) Create(c *gin.Context) {
	var req SaveCredReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	if req.SecretBlob == "" {
		response.ParamError(c, "secret_blob 不能为空")
		return
	}
	in, err := req.toInput()
	if err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	item, err := h.svc.Create(c.Request.Context(), accountID, in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrKeyNotFound):
			response.ParamError(c, "所选密钥不存在")
		case errors.Is(err, service.ErrVaultGone), errors.Is(err, service.ErrVaultLegacy):
			response.Unauthorized(c, err.Error())
		default:
			response.ServerError(c, "创建凭据失败")
		}
		return
	}
	response.Success(c, item)
}

// Update 更新凭据 PUT /api/v1/credentials/:id
func (h *CredentialHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req SaveCredReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	in, err := req.toInput()
	if err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	item, err := h.svc.Update(c.Request.Context(), accountID, id, in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCredNotFound):
			response.NotFound(c, "凭据不存在")
		case errors.Is(err, service.ErrKeyNotFound):
			response.ParamError(c, "所选密钥不存在")
		case errors.Is(err, service.ErrVaultGone), errors.Is(err, service.ErrVaultLegacy):
			response.Unauthorized(c, err.Error())
		default:
			response.ServerError(c, "保存凭据失败")
		}
		return
	}
	response.Success(c, item)
}

func (h *CredentialHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.SoftDelete(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrCredNotFound):
			response.NotFound(c, "凭据不存在")
		default:
			response.ServerError(c, "删除凭据失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

func (h *CredentialHandler) Restore(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Restore(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrCredNotFound):
			response.NotFound(c, "凭据不存在")
		default:
			response.ServerError(c, "恢复凭据失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// Purge 彻底删除 POST /api/v1/credentials/:id/purge
func (h *CredentialHandler) Purge(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Purge(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrCredNotFound):
			response.NotFound(c, "凭据不存在")
		default:
			response.ServerError(c, "彻底删除凭据失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// Reveal 验主密码解密连接信息 POST /api/v1/credentials/:id/reveal
func (h *CredentialHandler) Reveal(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req RevealReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	sec, err := h.svc.Reveal(c.Request.Context(), accountID, id, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCredNotFound):
			response.NotFound(c, "凭据不存在")
		case errors.Is(err, service.ErrVerifyLocked):
			response.TooFrequent(c, service.ErrVerifyLocked.Error())
		case errors.Is(err, service.ErrVaultLegacy):
			response.Unauthorized(c, service.ErrVaultLegacy.Error())
		case errors.Is(err, service.ErrPasswordWrong):
			response.PasswordWrong(c, service.ErrPasswordWrong.Error())
		default:
			response.ServerError(c, "解密凭据失败")
		}
		return
	}
	response.Success(c, sec)
}

func (h *CredentialHandler) Touch(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Touch(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrCredNotFound):
			response.NotFound(c, "凭据不存在")
		default:
			response.ServerError(c, "标记使用时间失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

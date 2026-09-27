package handler

import (
	"encoding/base64"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

// KeyHandler 密钥接口。
type KeyHandler struct {
	svc *service.KeyService
}

func NewKeyHandler(svc *service.KeyService) *KeyHandler {
	return &KeyHandler{svc: svc}
}

type ListKeysReq struct {
	Keyword        string `form:"keyword"`
	Type           string `form:"type"`
	IncludeDeleted bool   `form:"include_deleted"`
}

// SaveKeyReq 创建/更新密钥请求体。SecretBlob 为客户端 AES-256-GCM 加密的密文
// （Base64），服务端原样入库、中间不解开；Fingerprint 由客户端计算提交
type SaveKeyReq struct {
	Name        string `json:"name" binding:"required,min=2,max=40"`
	Type        string `json:"type" binding:"required,oneof=ed25519 rsa2048 rsa4096 ecdsa"`
	Fingerprint string `json:"fingerprint" binding:"max=64"`
	SecretBlob  string `json:"secret_blob" binding:"required,min=28"`
	Remark      string `json:"remark" binding:"max=120"`
}

// decodeSecretBlob 解码客户端密文（Base64 → 原始字节，原样入库）
func decodeSecretBlob(b64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) < 28 { // 12B nonce + 16B tag 起步
		return nil, errors.New("密文格式不合法")
	}
	return raw, nil
}

// RevealReq 敏感内容解锁请求（主密码），各模块 reveal/export 共用。
type RevealReq struct {
	Password string `json:"password" binding:"required,min=1,max=128"`
}

func parseID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.ParamError(c, "id 必须是正整数")
		return 0, false
	}
	return uint(id), true
}

// List 查询密钥列表 GET /api/v1/keys
func (h *KeyHandler) List(c *gin.Context) {
	var req ListKeysReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	items, err := h.svc.List(c.Request.Context(), accountID, req.Keyword, req.Type, req.IncludeDeleted)
	if err != nil {
		response.ServerError(c, "查询密钥失败")
		return
	}
	response.Success(c, gin.H{"list": items, "total": len(items)})
}

func (h *KeyHandler) Create(c *gin.Context) {
	var req SaveKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	blob, err := decodeSecretBlob(req.SecretBlob)
	if err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	item, err := h.svc.Create(c.Request.Context(), accountID, req.Name, req.Type, req.Fingerprint, blob, req.Remark)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrVaultGone), errors.Is(err, service.ErrVaultLegacy):
			response.Unauthorized(c, err.Error())
		default:
			response.ServerError(c, "创建密钥失败")
		}
		return
	}
	response.Success(c, item)
}

func (h *KeyHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req SaveKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	blob, err := decodeSecretBlob(req.SecretBlob)
	if err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	item, err := h.svc.Update(c.Request.Context(), accountID, id, req.Name, req.Type, req.Fingerprint, blob, req.Remark)
	if err != nil {
		if errors.Is(err, service.ErrKeyNotFound) {
			response.NotFound(c, "密钥不存在")
			return
		}
		response.ServerError(c, "保存密钥失败")
		return
	}
	response.Success(c, item)
}

func (h *KeyHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.SoftDelete(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrKeyNotFound):
			response.NotFound(c, "密钥不存在")
		default:
			response.ServerError(c, "删除密钥失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// Restore 从回收站恢复 POST /api/v1/keys/:id/restore
func (h *KeyHandler) Restore(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Restore(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrKeyNotFound):
			response.NotFound(c, "密钥不存在")
		default:
			response.ServerError(c, "恢复密钥失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// Purge 彻底删除 POST /api/v1/keys/:id/purge
func (h *KeyHandler) Purge(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountID := c.GetUint("account_id")
	if err := h.svc.Purge(c.Request.Context(), accountID, id); err != nil {
		switch {
		case errors.Is(err, service.ErrKeyNotFound):
			response.NotFound(c, "密钥不存在")
		default:
			response.ServerError(c, "彻底删除密钥失败")
		}
		return
	}
	response.Success(c, gin.H{"ok": true})
}

func (h *KeyHandler) Reveal(c *gin.Context) {
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
		case errors.Is(err, service.ErrKeyNotFound):
			response.NotFound(c, "密钥不存在")
		case errors.Is(err, service.ErrVerifyLocked):
			response.TooFrequent(c, service.ErrVerifyLocked.Error())
		case errors.Is(err, service.ErrVaultLegacy):
			response.Unauthorized(c, service.ErrVaultLegacy.Error())
		case errors.Is(err, service.ErrPasswordWrong):
			response.PasswordWrong(c, service.ErrPasswordWrong.Error())
		default:
			response.ServerError(c, "解密密钥失败")
		}
		return
	}
	response.Success(c, sec)
}

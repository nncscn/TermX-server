package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ky/internal/middleware"
	"ky/internal/service"
)

type TermxHandler struct {
	svc *service.TermxService
	//key 启动时解析出的生效密钥（config 显式值或自动生成，见 pkg/termxkey）
	key string
}

func NewTermxHandler(svc *service.TermxService, key string) *TermxHandler {
	return &TermxHandler{svc: svc, key: key}
}

func termxFail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"message": msg})
}

// termxErrStatus 哨兵错误 → HTTP 状态码（文案用 err.Error()）。
func termxErrStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrTermxVaultNotSet),
		errors.Is(err, service.ErrTermxBadMaterial),
		errors.Is(err, service.ErrTermxBadSince):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrTermxVaultAlready):
		return http.StatusConflict
	case errors.Is(err, service.ErrTermxVaultWrong):
		return http.StatusUnauthorized
	}
	return http.StatusInternalServerError
}

func (h *TermxHandler) Verify(c *gin.Context) {
	var body struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		// 空/坏 body 也走统一密钥文案（不区分原因,防探测）
		body.Key = ""
	}
	if ok, msg := middleware.TermxKeyMatch(h.key, body.Key); !ok {
		termxFail(c, http.StatusUnauthorized, msg)
		return
	}
	uid, err := h.svc.AccountID(c.Request.Context())
	if err != nil {
		termxFail(c, http.StatusUnauthorized, "服务端无可用账户")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "hasVault": h.svc.HasVault(uid)})
}

// VaultParams 取仓库盐/代数（X-Server-Key 鉴权）。
func (h *TermxHandler) VaultParams(c *gin.Context) {
	salt, gen, err := h.svc.VaultParams(c.GetUint("account_id"))
	if err != nil {
		termxFail(c, termxErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"salt": salt, "iterations": 250000, "gen": gen})
}

// VaultSetup 首次设置仓库密码（免验证码——密钥即信任根）
func (h *TermxHandler) VaultSetup(c *gin.Context) {
	var body struct {
		Salt string `json:"salt"`
		Dk   string `json:"dk"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		termxFail(c, http.StatusBadRequest, "请求体非法")
		return
	}
	token, err := h.svc.VaultSetup(c.Request.Context(), c.GetUint("account_id"), body.Salt, body.Dk)
	if err != nil {
		termxFail(c, termxErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "vaultToken": token})
}

func (h *TermxHandler) VaultUnlock(c *gin.Context) {
	var body struct {
		Dk string `json:"dk"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		termxFail(c, http.StatusBadRequest, "请求体非法")
		return
	}
	token, err := h.svc.VaultUnlock(c.GetUint("account_id"), body.Dk)
	if err != nil {
		termxFail(c, termxErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "vaultToken": token})
}

func (h *TermxHandler) ListEntries(c *gin.Context) {
	var body struct {
		Since string `json:"since"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		body.Since = "" // 空 body 视为全量
	}
	entries, groups, serverTime, err := h.svc.ListEntries(c.Request.Context(), c.GetUint("account_id"), body.Since)
	if err != nil {
		termxFail(c, termxErrStatus(err), err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "groups": groups, "serverTime": serverTime})
}

func (h *TermxHandler) PushEntries(c *gin.Context) {
	var body struct {
		Entries []service.TermxEntryIn `json:"entries"`
		Groups  *[]string              `json:"groups"` // 忽略：分组由条目派生
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		termxFail(c, http.StatusBadRequest, "请求体非法")
		return
	}
	acks, serverTime, err := h.svc.PushEntries(c.Request.Context(), c.GetUint("account_id"), body.Entries)
	if err != nil {
		termxFail(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": acks, "serverTime": serverTime})
}

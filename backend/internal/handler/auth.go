package handler

import (
	"errors"

	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
	"ky/internal/service"
)

type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler 构造 handler
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// LoginReq 登录请求。连续失败达到阈值后须携带验证码字段。
type LoginReq struct {
	Username      string                 `json:"username" binding:"required,min=2,max=64"`
	Password      string                 `json:"password" binding:"required,min=1,max=128"`
	CaptchaID     string                 `json:"captcha_id"`
	CaptchaClicks []service.CaptchaPoint `json:"captcha_clicks"`
}

// CaptchaResp GET /api/v1/auth/captcha 的响应由 service 组装

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}

	result, err := h.svc.Login(c.Request.Context(), req.Username, req.Password, req.CaptchaID, req.CaptchaClicks)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			response.Unauthorized(c, service.ErrInvalidCredentials.Error())
		case errors.Is(err, service.ErrAccountLocked):
			response.Unauthorized(c, service.ErrAccountLocked.Error())
		case errors.Is(err, service.ErrCaptchaRequired):
			response.Unauthorized(c, service.ErrCaptchaRequired.Error())
		case errors.Is(err, service.ErrCaptchaWrong):
			response.Unauthorized(c, service.ErrCaptchaWrong.Error())
		default:
			response.ServerError(c, "登录失败")
		}
		return
	}
	out := gin.H{"token": result.Token, "expires_at": result.ExpiresAt}
	if result.MigratedRecoveryKey != "" {
		// 旧数据迁移完成：一次性下发新恢复密钥（前端弹窗展示）
		out["recovery_key"] = result.MigratedRecoveryKey
	}
	if result.DataKey != "" {
		out["data_key"] = result.DataKey
	}
	response.Success(c, out)
}

func (h *AuthHandler) GetCaptcha(c *gin.Context) {
	response.Success(c, h.svc.GetCaptcha())
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token := bearerToken(c)
	if token == "" {
		response.Unauthorized(c, "缺少令牌")
		return
	}
	if err := h.svc.Logout(c.Request.Context(), token); err != nil {
		response.ServerError(c, "退出登录失败")
		return
	}
	response.Success(c, gin.H{"logged_out": true})
}

type ChangePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required,min=1,max=128"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

// ChangePassword POST /api/v1/auth/password/change（需登录）
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	currentToken := bearerToken(c)
	if err := h.svc.ChangePassword(c.Request.Context(), accountID, req.OldPassword, req.NewPassword, currentToken); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			// 主密码输错用 40102：与 40101（会话失效）区分，避免前端误判为过期跳登录
			response.PasswordWrong(c, "原密码不正确")
		case errors.Is(err, service.ErrVerifyLocked):
			response.TooFrequent(c, service.ErrVerifyLocked.Error())
		default:
			response.ServerError(c, "修改密码失败")
		}
		return
	}
	response.Success(c, gin.H{"changed": true})
}

type GenerateRecoveryKeyReq struct {
	Password string `json:"password" binding:"required,min=1,max=128"`
}

// GenerateRecoveryKey POST /api/v1/auth/recovery/generate（需登录，明文仅返回一次）
func (h *AuthHandler) GenerateRecoveryKey(c *gin.Context) {
	var req GenerateRecoveryKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	accountID := c.GetUint("account_id")
	recovery, err := h.svc.GenerateRecoveryKey(c.Request.Context(), accountID, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPasswordWrong):
			response.PasswordWrong(c, service.ErrPasswordWrong.Error())
		case errors.Is(err, service.ErrVerifyLocked):
			response.TooFrequent(c, service.ErrVerifyLocked.Error())
		default:
			response.ServerError(c, "生成恢复密钥失败")
		}
		return
	}
	response.Success(c, gin.H{"recovery_key": recovery})
}

type ForgotVerifyReq struct {
	Username    string `json:"username" binding:"required,min=2,max=64"`
	RecoveryKey string `json:"recovery_key" binding:"required,min=8,max=128"`
}

func (h *AuthHandler) ForgotVerify(c *gin.Context) {
	var req ForgotVerifyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	ticket, err := h.svc.ForgotVerify(c.Request.Context(), req.Username, req.RecoveryKey)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRecoveryInvalid):
			response.Unauthorized(c, service.ErrRecoveryInvalid.Error())
		case errors.Is(err, service.ErrRecoveryNotBound):
			response.Unauthorized(c, service.ErrRecoveryNotBound.Error())
		case errors.Is(err, service.ErrAccountLocked):
			response.Unauthorized(c, service.ErrAccountLocked.Error())
		default:
			response.ServerError(c, "校验恢复密钥失败")
		}
		return
	}
	response.Success(c, gin.H{"reset_ticket": ticket})
}

type ForgotResetReq struct {
	ResetTicket string `json:"reset_ticket" binding:"required,min=16,max=128"`
	RecoveryKey string `json:"recovery_key" binding:"required,min=8,max=128"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

// ForgotReset POST /api/v1/auth/forgot/reset
func (h *AuthHandler) ForgotReset(c *gin.Context) {
	var req ForgotResetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, err.Error())
		return
	}
	err := h.svc.ForgotReset(c.Request.Context(), req.ResetTicket, req.RecoveryKey, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrResetTicketInvalid), errors.Is(err, service.ErrRecoveryInvalid):
			response.Unauthorized(c, err.Error())
		case errors.Is(err, service.ErrRecoveryUnmigrated):
			response.ParamError(c, err.Error())
		default:
			response.ServerError(c, "重置密码失败")
		}
		return
	}
	response.Success(c, gin.H{"reset": true})
}

func bearerToken(c *gin.Context) string {
	const prefix = "Bearer "
	auth := c.GetHeader("Authorization")
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):]
	}
	return ""
}

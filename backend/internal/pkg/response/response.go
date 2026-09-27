package response

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	CodeSuccess       = 0
	CodeParamError    = 40001
	CodeUnauthorized  = 40101
	CodeForbidden     = 40301
	CodePasswordWrong = 40102
	CodeNameConflict  = 40901
	CodeNotFound      = 40401
	CodeTooFrequent   = 42901
	CodeConnectError  = 42201
	CodeServerError   = 50000
)

type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: CodeSuccess, Message: "ok", Data: data})
}

func Fail(c *gin.Context, httpStatus, code int, message string) {
	c.JSON(httpStatus, Body{Code: code, Message: message})
}

// ParamError 参数错误（HTTP 400）。对外只回统一文案,不泄露字段名/校验规则
// 等接口结构细节（I1 加固）;细节记入服务端日志便于排查
func ParamError(c *gin.Context, message string) {
	if message != "" && message != "参数不合法" {
		log.Printf("[param] %s %s: %s", c.Request.Method, c.Request.URL.Path, message)
	}
	Fail(c, http.StatusBadRequest, CodeParamError, "参数不合法")
}

func NotFound(c *gin.Context, message string) {
	Fail(c, http.StatusNotFound, CodeNotFound, message)
}

// Unauthorized 会话级未登录/令牌失效（业务码 40101，HTTP 层 200）
// 前端据此清令牌并整页跳回登录页；输错密码/验证码属预期业务流，
func Unauthorized(c *gin.Context, message string) {
	Fail(c, http.StatusOK, CodeUnauthorized, message)
}

func Forbidden(c *gin.Context, message string) {
	Fail(c, http.StatusForbidden, CodeForbidden, message)
}

// PasswordWrong 主密码校验失败（reveal 解密/改密/备份导出导入等）。
// 与 40101 区分：主密码输错不应触发前端"会话过期跳登录"，业务码 40102、HTTP 层 200
func PasswordWrong(c *gin.Context, message string) {
	Fail(c, http.StatusOK, CodePasswordWrong, message)
}

// Conflict 目标资源名冲突（如目标库名已存在）。业务码 40901，HTTP 层 200，
func Conflict(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, Body{Code: CodeNameConflict, Message: message, Data: data})
}

func TooFrequent(c *gin.Context, message string) {
	Fail(c, http.StatusOK, CodeTooFrequent, message)
}

func ConnectError(c *gin.Context, message string) {
	Fail(c, http.StatusOK, CodeConnectError, message)
}

func ServerError(c *gin.Context, message string) {
	Fail(c, http.StatusInternalServerError, CodeServerError, message)
}

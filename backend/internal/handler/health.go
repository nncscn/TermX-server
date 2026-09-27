package handler

import (
	"github.com/gin-gonic/gin"

	"ky/internal/pkg/response"
)

func Health(c *gin.Context) {
	response.Success(c, gin.H{"status": "ok"})
}

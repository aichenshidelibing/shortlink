package api

import "github.com/gin-gonic/gin"

func JSON(c *gin.Context, code int, data interface{}) {
	c.JSON(code, gin.H{"code": code, "data": data})
}

func Error(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"code": code, "error": msg})
}

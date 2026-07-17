package middleware

import (
	"net/http"
	"shortlink/internal/service"

	"github.com/gin-gonic/gin"
)

func BanCheck(banSvc *service.BanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		banned, reason, err := banSvc.IsBanned(c.Request.Context(), ip)
		if err != nil {
			c.Next()
			return
		}
		if banned {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "banned", "reason": reason})
			return
		}
		c.Next()
	}
}

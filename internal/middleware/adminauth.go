package middleware

import (
	"net/http"
	"shortlink/internal/auth"

	"github.com/gin-gonic/gin"
)

func AdminAuth(sessionMgr *auth.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		adminID, err := sessionMgr.Validate(c.Request)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("admin_id", adminID)
		c.Next()
	}
}

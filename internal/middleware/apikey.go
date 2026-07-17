package middleware

import (
	"net/http"
	"shortlink/internal/model"
	"shortlink/internal/service"

	"github.com/gin-gonic/gin"
)

func RequireAPIKeyPermission(keySvc *service.APIKeyService, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get("api_key")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key required"})
			return
		}
		key, ok := v.(*model.APIKey)
		if !ok || !keySvc.HasPermission(key, permission) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "api key permission denied"})
			return
		}
		if err := keySvc.CheckQuota(c.Request.Context(), key, 1); err != nil {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
			return
		}
		c.Next()
	}
}

func APIKeyAuth(keySvc *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key required"})
			return
		}

		apiKey, err := keySvc.Validate(c.Request.Context(), key)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			return
		}

		c.Set("api_key_id", apiKey.ID)
		c.Set("api_key", apiKey)
		c.Next()
	}
}

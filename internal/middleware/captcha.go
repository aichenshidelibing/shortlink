package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"shortlink/internal/config"
	"shortlink/internal/service"
	"strings"

	"github.com/gin-gonic/gin"
)

type captchaPayload struct {
	Provider       string `json:"captcha_provider"`
	CaptchaToken   string `json:"captcha_token"`
	CapToken       string `json:"cap_token"`
	CFTurnstile    string `json:"cf_turnstile_response"`
	CFResponse     string `json:"cf_response"`
	TurnstileToken string `json:"turnstile_token"`
}

func CaptchaGuard(captchaSvc *service.CaptchaService, cfCfg *config.CloudflareConfig, action service.CaptchaAction) gin.HandlerFunc {
	return func(c *gin.Context) {
		if captchaSvc == nil {
			c.Next()
			return
		}

		decision := captchaSvc.Decide(c.Request.Context(), action, c.ClientIP())
		if !decision.Required {
			c.Next()
			return
		}

		provider := strings.ToLower(decision.Provider)
		if provider == "turnstile" && !turnstileConfigured(ActiveCFTurnstileConfig(cfCfg)) {
			provider = "cap"
		}
		switch provider {
		case "turnstile":
			token := ExtractTurnstileToken(c)
			if token == "" {
				captchaJSON(c, http.StatusPreconditionRequired, "captcha_escalation_required", "turnstile", decision.Reason)
				return
			}
			if err := VerifyTurnstile(c.Request.Context(), ActiveCFTurnstileConfig(cfCfg), token, c.ClientIP()); err != nil {
				_, _ = captchaSvc.RecordFailure(c.Request.Context(), action, c.ClientIP())
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "captcha_failed", "captcha": gin.H{"provider": "turnstile", "reason": "verify_failed"}})
				return
			}
			captchaSvc.ClearFailure(c.Request.Context(), action, c.ClientIP())
			c.Next()
		case "cap":
			token := extractCapToken(c)
			if token == "" {
				captchaJSON(c, http.StatusPreconditionRequired, "captcha_required", "cap", decision.Reason)
				return
			}
			if err := captchaSvc.VerifyCap(c.Request.Context(), token, c.ClientIP()); err != nil {
				count, _ := captchaSvc.RecordFailure(c.Request.Context(), action, c.ClientIP())
				cfg := captchaSvc.Config()
				if cfg.EscalationProvider == "turnstile" && cfg.FailureThreshold > 0 && count >= int64(cfg.FailureThreshold) && turnstileConfigured(ActiveCFTurnstileConfig(cfCfg)) {
					captchaJSON(c, http.StatusPreconditionRequired, "captcha_escalation_required", "turnstile", "failure_threshold")
					return
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "captcha_failed", "captcha": gin.H{"provider": "cap", "reason": "verify_failed"}})
				return
			}
			captchaSvc.ClearFailure(c.Request.Context(), action, c.ClientIP())
			c.Next()
		default:
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "captcha provider is not supported"})
		}
	}
}

func turnstileConfigured(cfg *config.CloudflareConfig) bool {
	return cfg != nil && cfg.Enabled && strings.TrimSpace(cfg.SiteKey) != "" && strings.TrimSpace(cfg.SecretKey) != ""
}

func captchaJSON(c *gin.Context, status int, errText, provider, reason string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": errText,
		"captcha": gin.H{
			"required": true,
			"provider": provider,
			"reason":   reason,
		},
	})
}

func extractCapToken(c *gin.Context) string {
	for _, key := range []string{"cap_token", "captcha_token", "cap-token"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			return v
		}
	}
	for _, key := range []string{"X-CAP-Token", "X-Captcha-Token"} {
		if v := strings.TrimSpace(c.GetHeader(key)); v != "" {
			return v
		}
	}
	payload := readCaptchaJSON(c)
	for _, v := range []string{payload.CapToken, payload.CaptchaToken} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readCaptchaJSON(c *gin.Context) captchaPayload {
	contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if !strings.EqualFold(contentType, "application/json") || c.Request.Body == nil {
		return captchaPayload{}
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return captchaPayload{}
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	if len(body) == 0 {
		return captchaPayload{}
	}
	var payload captchaPayload
	_ = json.Unmarshal(body, &payload)
	return payload
}

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"shortlink/internal/config"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const cfVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var globalCFConfig atomic.Value // *config.CloudflareConfig

type cfResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// SetCFTurnstileConfig updates the Turnstile verification config at runtime.
func SetCFTurnstileConfig(cfg *config.CloudflareConfig) {
	if cfg != nil {
		copy := *cfg
		globalCFConfig.Store(&copy)
	}
}

func getCFTurnstileConfig(fallback *config.CloudflareConfig) *config.CloudflareConfig {
	if v := globalCFConfig.Load(); v != nil {
		return v.(*config.CloudflareConfig)
	}
	return fallback
}

func ActiveCFTurnstileConfig(fallback *config.CloudflareConfig) *config.CloudflareConfig {
	return getCFTurnstileConfig(fallback)
}

func readTurnstileJSONToken(c *gin.Context) string {
	contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if !strings.EqualFold(contentType, "application/json") || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Response string `json:"cf_turnstile_response"`
		Alt      string `json:"cf_response"`
		Token    string `json:"turnstile_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	switch {
	case payload.Response != "":
		return payload.Response
	case payload.Alt != "":
		return payload.Alt
	default:
		return payload.Token
	}
}

func ExtractTurnstileToken(c *gin.Context) string {
	token := c.PostForm("cf_turnstile_response")
	if token == "" {
		token = c.GetHeader("X-CF-Turnstile-Token")
	}
	if token == "" {
		token, _ = c.GetPostForm("cf_response")
	}
	if token == "" {
		token = readTurnstileJSONToken(c)
	}
	return strings.TrimSpace(token)
}

func VerifyTurnstile(ctx context.Context, cfg *config.CloudflareConfig, token, remoteIP string) error {
	if cfg == nil || !cfg.Enabled || cfg.SecretKey == "" {
		return fmt.Errorf("turnstile is not configured")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("turnstile token required")
	}
	data := url.Values{}
	data.Set("secret", cfg.SecretKey)
	data.Set("response", token)
	if remoteIP != "" {
		data.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfVerifyURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("turnstile decode failed: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("turnstile verification failed: %s", strings.Join(result.ErrorCodes, ","))
	}
	return nil
}

func CFTurnstile(cfg *config.CloudflareConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		active := getCFTurnstileConfig(cfg)
		if active == nil || !active.Enabled || active.SecretKey == "" {
			c.Next()
			return
		}

		token := ExtractTurnstileToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "turnstile token required"})
			return
		}
		if err := VerifyTurnstile(c.Request.Context(), active, token, c.ClientIP()); err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "turnstile verification failed"})
			return
		}
		c.Next()
	}
}

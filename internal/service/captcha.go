package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"shortlink/internal/config"
	"shortlink/internal/repository"
	"strings"
	"sync/atomic"
	"time"
)

type CaptchaAction string

const (
	CaptchaActionCreateLink   CaptchaAction = "create_link"
	CaptchaActionSubmitReport CaptchaAction = "submit_report"
	CaptchaActionAdminLogin   CaptchaAction = "admin_login"
)

type CaptchaDecision struct {
	Required bool
	Provider string
	Reason   string
}

type CaptchaService struct {
	cache  *repository.CacheRepository
	client *http.Client
	cfg    atomic.Value // *config.CaptchaConfig
}

type capVerifyRequest struct {
	Secret   string `json:"secret"`
	Response string `json:"response"`
}

type capVerifyResponse struct {
	Success bool     `json:"success"`
	Errors  []string `json:"errors,omitempty"`
}

func NewCaptchaService(cfg *config.CaptchaConfig, cache *repository.CacheRepository) *CaptchaService {
	s := &CaptchaService{
		cache:  cache,
		client: &http.Client{Timeout: 5 * time.Second},
	}
	s.ReloadConfig(cfg)
	return s
}

func (s *CaptchaService) ReloadConfig(cfg *config.CaptchaConfig) {
	copy := normalizeCaptchaConfig(cfg)
	s.cfg.Store(&copy)
}

func (s *CaptchaService) Config() *config.CaptchaConfig {
	if v := s.cfg.Load(); v != nil {
		copy := *(v.(*config.CaptchaConfig))
		return &copy
	}
	copy := normalizeCaptchaConfig(nil)
	return &copy
}

func (s *CaptchaService) PublicConfig() config.CaptchaConfig {
	cfg := s.Config()
	cfg.Cap.SecretKey = ""
	cfg.PlayCaptcha.SecretKey = ""
	return *cfg
}

func (s *CaptchaService) Decide(ctx context.Context, action CaptchaAction, ip string) CaptchaDecision {
	cfg := s.Config()
	if !captchaActive(cfg) {
		return CaptchaDecision{}
	}
	if s != nil && s.cache != nil {
		if _, err := s.cache.Get(ctx, s.escalationKey(action, ip)); err == nil {
			return CaptchaDecision{Required: true, Provider: "turnstile", Reason: "risk_escalated"}
		}
	}
	return CaptchaDecision{Required: true, Provider: cfg.NormalProvider, Reason: "normal"}
}

func (s *CaptchaService) VerifyCap(ctx context.Context, token, remoteIP string) error {
	cfg := s.Config()
	if !captchaActive(cfg) {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("cap token required")
	}
	verifyURL := strings.TrimSpace(cfg.Cap.VerifyURL)
	secret := strings.TrimSpace(cfg.Cap.SecretKey)
	if verifyURL == "" || secret == "" {
		return fmt.Errorf("cap is not configured")
	}
	body, _ := json.Marshal(capVerifyRequest{Secret: secret, Response: token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if remoteIP != "" {
		req.Header.Set("X-Forwarded-For", remoteIP)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cap verify returned status %d", resp.StatusCode)
	}
	var out capVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("cap verification failed")
	}
	return nil
}

func (s *CaptchaService) RecordFailure(ctx context.Context, action CaptchaAction, ip string) (int64, error) {
	cfg := s.Config()
	if s == nil || s.cache == nil {
		return 0, fmt.Errorf("captcha cache is unavailable")
	}
	window := time.Duration(cfg.RiskWindowSeconds) * time.Second
	count, err := s.cache.IncrementWithTTL(ctx, s.failureKey(action, ip), window)
	if err != nil {
		return 0, err
	}
	if cfg.FailureThreshold > 0 && count >= int64(cfg.FailureThreshold) {
		_ = s.MarkEscalated(ctx, action, ip)
	}
	return count, nil
}

func (s *CaptchaService) ClearFailure(ctx context.Context, action CaptchaAction, ip string) {
	if s == nil || s.cache == nil {
		return
	}
	_ = s.cache.Delete(ctx, s.failureKey(action, ip))
}

func (s *CaptchaService) MarkEscalated(ctx context.Context, action CaptchaAction, ip string) error {
	cfg := s.Config()
	if s == nil || s.cache == nil {
		return fmt.Errorf("captcha cache is unavailable")
	}
	return s.cache.Set(ctx, s.escalationKey(action, ip), "1", time.Duration(cfg.RiskWindowSeconds)*time.Second)
}

func normalizeCaptchaConfig(cfg *config.CaptchaConfig) config.CaptchaConfig {
	var out config.CaptchaConfig
	if cfg != nil {
		out = *cfg
	}
	out.Provider = strings.ToLower(strings.TrimSpace(out.Provider))
	if out.Provider == "" {
		out.Provider = "cap"
	}
	out.Mode = strings.ToLower(strings.TrimSpace(out.Mode))
	if out.Mode == "" {
		out.Mode = "adaptive"
	}
	out.NormalProvider = strings.ToLower(strings.TrimSpace(out.NormalProvider))
	if out.NormalProvider == "" {
		out.NormalProvider = out.Provider
	}
	out.EscalationProvider = strings.ToLower(strings.TrimSpace(out.EscalationProvider))
	if out.EscalationProvider == "" {
		out.EscalationProvider = "turnstile"
	}
	if out.FailureThreshold <= 0 {
		out.FailureThreshold = 3
	}
	if out.RiskWindowSeconds <= 0 {
		out.RiskWindowSeconds = 600
	}
	return out
}

func captchaActive(cfg *config.CaptchaConfig) bool {
	if cfg == nil || !cfg.Enabled {
		return false
	}
	return cfg.Provider == "cap"
}

func (s *CaptchaService) failureKey(action CaptchaAction, ip string) string {
	return "captcha:fail:" + string(action) + ":" + hashKey(ip)
}

func (s *CaptchaService) escalationKey(action CaptchaAction, ip string) string {
	return "captcha:escalate:" + string(action) + ":" + hashKey(ip)
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:8])
}

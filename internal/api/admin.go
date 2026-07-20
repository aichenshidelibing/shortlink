package api

import (
	"bufio"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"runtime"
	"shortlink/internal/auth"
	"shortlink/internal/config"
	"shortlink/internal/middleware"
	"shortlink/internal/model"
	"shortlink/internal/service"
	"shortlink/internal/version"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// bootTime is captured on process start so SystemStatus can report a real
// uptime instead of the placeholder "running" string.
var bootTime = time.Now()

type AdminHandler struct {
	adminSvc      *service.AdminService
	linkSvc       *service.LinkService
	apiKeySvc     *service.APIKeyService
	statsSvc      *service.StatsService
	banSvc        *service.BanService
	wordFilterSvc *service.WordFilterService
	noticeSvc     *service.NoticeService
	reportSvc     *service.ReportService
	modSvc        *service.ModerationService
	auditSvc      *service.AuditService
	domainSvc     *service.DomainService
	captchaSvc    *service.CaptchaService
	sessionMgr    *auth.SessionManager
}

func NewAdminHandler(adminSvc *service.AdminService, linkSvc *service.LinkService, apiKeySvc *service.APIKeyService, statsSvc *service.StatsService, banSvc *service.BanService, wordFilterSvc *service.WordFilterService, noticeSvc *service.NoticeService, reportSvc *service.ReportService, modSvc *service.ModerationService, auditSvc *service.AuditService, domainSvc *service.DomainService, captchaSvc *service.CaptchaService, sessionMgr *auth.SessionManager) *AdminHandler {
	return &AdminHandler{
		adminSvc: adminSvc, linkSvc: linkSvc, apiKeySvc: apiKeySvc,
		statsSvc: statsSvc, banSvc: banSvc, wordFilterSvc: wordFilterSvc,
		noticeSvc: noticeSvc, reportSvc: reportSvc, modSvc: modSvc,
		auditSvc: auditSvc, domainSvc: domainSvc, captchaSvc: captchaSvc, sessionMgr: sessionMgr,
	}
}

func (h *AdminHandler) audit(actorType, actorID, action, resource, resourceID string, c *gin.Context, metadata string) {
	if h.auditSvc == nil || c == nil {
		return
	}
	h.auditSvc.Record(c.Request.Context(), actorType, actorID, action, resource, resourceID, c.ClientIP(), c.GetHeader("User-Agent"), metadata)
}

// ── Auth ──

func (h *AdminHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		TOTP     string `json:"totp"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	admin, err := h.adminSvc.Login(c.Request.Context(), req.Username, req.Password, req.TOTP)
	if err != nil {
		h.noticeSvc.SendLoginEvent(c.Request.Context(), false, c.ClientIP())
		h.audit("admin", req.Username, "login_failed", "admin", req.Username, c, "")
		Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	h.noticeSvc.SendLoginEvent(c.Request.Context(), true, c.ClientIP())
	h.audit("admin", strconv.Itoa(admin.ID), "login_success", "admin", req.Username, c, "")
	h.sessionMgr.Create(c.Writer, c.Request, admin.ID)
	JSON(c, http.StatusOK, gin.H{"message": "login success"})
}

func (h *AdminHandler) Logout(c *gin.Context) {
	h.audit("admin", "", "logout", "session", "", c, "")
	h.sessionMgr.Clear(c.Writer)
	JSON(c, http.StatusOK, gin.H{"message": "logout success"})
}

func (h *AdminHandler) GetTOTP(c *gin.Context) {
	uri := h.adminSvc.GetTOTPURI(c.Request.Context())
	admin, _ := h.adminSvc.GetAdmin(c.Request.Context())
	verified := admin != nil && admin.TOTPVerified
	JSON(c, http.StatusOK, gin.H{"uri": uri, "account": "admin", "issuer": "Shortlink", "verified": verified})
}

func (h *AdminHandler) VerifyTOTP(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.adminSvc.VerifyTOTP(c.Request.Context(), req.Code); err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	h.audit("admin", "", "totp_verify", "admin", "totp", c, "")
	JSON(c, http.StatusOK, gin.H{"message": "TOTP activated"})
}

// ── Dashboard ──

func (h *AdminHandler) Dashboard(c *gin.Context) {
	ctx := c.Request.Context()

	_, totalLinks, err := h.linkSvc.List(ctx, 1, 1)
	if err != nil {
		totalLinks = 0
	}

	totalClicks, err := h.linkSvc.SumClicks(ctx)
	if err != nil {
		totalClicks = 0
	}

	reportCount := 0
	if reports, err := h.reportSvc.List(ctx, 0); err == nil {
		reportCount = len(reports)
	}

	activeBans, _ := h.banSvc.CountActive(ctx)

	JSON(c, http.StatusOK, gin.H{
		"total_links":     totalLinks,
		"total_clicks":    totalClicks,
		"pending_reports": reportCount,
		"active_bans":     activeBans,
	})
}

// ── Links (admin only sees code/click count, NOT URL) ──

func (h *AdminHandler) ListLinks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	links, total, err := h.linkSvc.List(c.Request.Context(), page, size)
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	var result []gin.H
	for _, l := range links {
		result = append(result, gin.H{
			"short_code":   l.ShortCode,
			"expires_at":   l.ExpiresAt,
			"has_password": l.PasswordHash != "",
			"click_count":  l.ClickCount,
			"created_at":   l.CreatedAt,
		})
	}
	JSON(c, http.StatusOK, gin.H{"list": result, "total": total})
}

func (h *AdminHandler) DeleteLink(c *gin.Context) {
	code := c.Param("code")
	if err := h.linkSvc.Delete(c.Request.Context(), code); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "link_delete", "link", code, c, "")
	JSON(c, http.StatusOK, gin.H{"message": "deleted"})
}

// ── API Keys ──

func (h *AdminHandler) ListAPIKeys(c *gin.Context) {
	keys, err := h.apiKeySvc.List(c.Request.Context())
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(c, http.StatusOK, keys)
}

func (h *AdminHandler) CreateAPIKey(c *gin.Context) {
	var req struct {
		Name           string   `json:"name"`
		Purpose        string   `json:"purpose"`
		Permissions    []string `json:"permissions"`
		QuotaPerMinute int      `json:"quota_per_minute"`
		QuotaPerDay    int      `json:"quota_per_day"`
		QuotaPerMonth  int      `json:"quota_per_month"`
		AllowedDomains string   `json:"allowed_domains"`
		DeniedDomains  string   `json:"denied_domains"`
		ExpiresAt      string   `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	exp, ok := parseOptionalTime(c, req.ExpiresAt)
	if !ok {
		return
	}

	plainKey, key, err := h.apiKeySvc.CreateWithOptions(c.Request.Context(), service.APIKeyOptions{
		Name: req.Name, Purpose: req.Purpose, Permissions: req.Permissions,
		QuotaPerMinute: req.QuotaPerMinute, QuotaPerDay: req.QuotaPerDay, QuotaPerMonth: req.QuotaPerMonth,
		AllowedDomains: req.AllowedDomains, DeniedDomains: req.DeniedDomains, ExpiresAt: exp,
	})
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "apikey_create", "apikey", strconv.FormatUint(key.ID, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"api_key": plainKey, "id": key.ID, "expires_at": key.ExpiresAt})
}

func (h *AdminHandler) UpdateAPIKey(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req struct {
		Name           string   `json:"name"`
		Purpose        string   `json:"purpose"`
		Permissions    []string `json:"permissions"`
		QuotaPerMinute int      `json:"quota_per_minute"`
		QuotaPerDay    int      `json:"quota_per_day"`
		QuotaPerMonth  int      `json:"quota_per_month"`
		AllowedDomains string   `json:"allowed_domains"`
		DeniedDomains  string   `json:"denied_domains"`
		ExpiresAt      string   `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	exp, ok := parseOptionalTime(c, req.ExpiresAt)
	if !ok {
		return
	}
	if err := h.apiKeySvc.Update(c.Request.Context(), id, service.APIKeyOptions{
		Name: req.Name, Purpose: req.Purpose, Permissions: req.Permissions,
		QuotaPerMinute: req.QuotaPerMinute, QuotaPerDay: req.QuotaPerDay, QuotaPerMonth: req.QuotaPerMonth,
		AllowedDomains: req.AllowedDomains, DeniedDomains: req.DeniedDomains, ExpiresAt: exp,
	}); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "apikey_update", "apikey", strconv.FormatUint(id, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"message": "updated"})
}

func (h *AdminHandler) RevokeAPIKey(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.apiKeySvc.Revoke(c.Request.Context(), id); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "apikey_revoke", "apikey", strconv.FormatUint(id, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"message": "revoked"})
}

func (h *AdminHandler) DeleteAPIKey(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.apiKeySvc.Delete(c.Request.Context(), id); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "apikey_delete", "apikey", strconv.FormatUint(id, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"message": "deleted"})
}

func parseOptionalTime(c *gin.Context, raw string) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		Error(c, http.StatusBadRequest, "invalid expires_at format, use RFC3339")
		return nil, false
	}
	if t.IsZero() {
		return nil, true
	}
	return &t, true
}

// ── Stats ──

func (h *AdminHandler) GetStats(c *gin.Context) {
	code := c.Param("code")
	stats, err := h.statsSvc.GetLinkStats(c.Request.Context(), code)
	if err != nil {
		Error(c, http.StatusNotFound, err.Error())
		return
	}
	JSON(c, http.StatusOK, stats)
}

// ── Settings ──

func (h *AdminHandler) GetSettings(c *gin.Context) {
	settings, err := h.adminSvc.GetSettings(c.Request.Context())
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	var decrypted string
	if settings.SettingsEnc != "" {
		decrypted, _ = h.adminSvc.DecryptSettings(settings.SettingsEnc)
	}
	decrypted = h.settingsWithRuntimeDefaults(decrypted)

	JSON(c, http.StatusOK, gin.H{
		"suffix":            settings.Suffix,
		"suffix_changed_at": settings.SuffixChangedAt,
		"shortlink_length":  settings.ShortlinkLength,
		"settings":          decrypted,
		"version":           version.Get(),
	})
}

func (h *AdminHandler) SaveSettings(c *gin.Context) {
	var req struct {
		ShortlinkLength int    `json:"shortlink_length"`
		Settings        string `json:"settings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	settings, err := h.adminSvc.GetSettings(c.Request.Context())
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	if req.ShortlinkLength > 0 {
		if req.ShortlinkLength < 4 || req.ShortlinkLength > 12 {
			Error(c, http.StatusBadRequest, "shortlink_length must be between 4 and 12")
			return
		}
		settings.ShortlinkLength = req.ShortlinkLength
	}
	// Merge new settings into the existing JSON blob so a partial PUT (e.g. a
	// scripted change of just `rate_create`) doesn't wipe every unrelated
	// key. Without this, saving from a client that doesn't know every field
	// silently clears the ones it omitted.
	mergedJSON := ""
	if req.Settings != "" {
		var current map[string]interface{}
		if settings.SettingsEnc != "" {
			if plain, derr := h.adminSvc.DecryptSettings(settings.SettingsEnc); derr == nil && plain != "" {
				_ = json.Unmarshal([]byte(plain), &current)
			}
		}
		if current == nil {
			current = make(map[string]interface{})
		}
		var incoming map[string]interface{}
		if err := json.Unmarshal([]byte(req.Settings), &incoming); err != nil {
			Error(c, http.StatusBadRequest, "invalid settings json")
			return
		}
		for k, v := range incoming {
			current[k] = v
		}
		buf, _ := json.Marshal(current)
		mergedJSON = string(buf)
		settings.SettingsEnc = h.adminSvc.EncryptSettings(mergedJSON)
	}

	if err := h.adminSvc.SaveSettings(c.Request.Context(), settings); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	suffixChanged := false
	currentSuffix := settings.Suffix
	if reconciledSuffix, changed, err := h.adminSvc.ReconcileSuffixForShortlinkLength(c.Request.Context(), settings.ShortlinkLength); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	} else {
		suffixChanged = changed
		currentSuffix = reconciledSuffix
	}

	if mergedJSON != "" {
		h.noticeSvc.ReloadFromJSON(mergedJSON)
		h.reloadRateLimit(mergedJSON)
		h.reloadTurnstile(mergedJSON)
		h.reloadCaptcha(mergedJSON)
		h.reloadModeration(mergedJSON)
		h.reloadVersion(mergedJSON)
		// Refresh CSP so newly-configured images/videos/icons are not blocked
		// by the img-src / media-src whitelist on the very next request.
		middleware.ReloadCSPAssets(c.Request.Context(), h.adminSvc)
	}

	h.audit("admin", "", "settings_save", "settings", "", c, "")
	resp := gin.H{"message": "saved", "version": version.Get()}
	if suffixChanged {
		baseURL := requestBaseURL(c)
		h.noticeSvc.SendSuffixChanged(c.Request.Context(), currentSuffix, baseURL)
		resp["suffix_changed"] = true
		resp["suffix"] = currentSuffix
		resp["admin_url"] = baseURL + "/" + currentSuffix + "/"
	}
	JSON(c, http.StatusOK, resp)
}

func (h *AdminHandler) settingsWithRuntimeDefaults(settingsJSON string) string {
	var raw map[string]interface{}
	if strings.TrimSpace(settingsJSON) != "" {
		_ = json.Unmarshal([]byte(settingsJSON), &raw)
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}
	if h != nil {
		setDefault := func(key string, value interface{}) {
			if _, ok := raw[key]; !ok {
				raw[key] = value
			}
		}
		setRuntimeString := func(key, value string) {
			current, _ := raw[key].(string)
			current = strings.TrimSpace(current)
			badExample := strings.Contains(current, "cap.example.com") || strings.EqualFold(current, "admin")
			if current == "" || badExample {
				raw[key] = value
			}
		}
		if h.captchaSvc != nil {
			cfg := h.captchaSvc.Config()
			setDefault("captcha_enabled", cfg.Enabled)
			setDefault("captcha_provider", cfg.Provider)
			setDefault("captcha_mode", cfg.Mode)
			setDefault("captcha_normal_provider", cfg.NormalProvider)
			setDefault("captcha_escalation_provider", cfg.EscalationProvider)
			setDefault("captcha_failure_threshold", cfg.FailureThreshold)
			setDefault("captcha_risk_window_seconds", cfg.RiskWindowSeconds)
			setRuntimeString("cap_site_key", cfg.Cap.SiteKey)
			setRuntimeString("cap_api_endpoint", cfg.Cap.APIEndpoint)
			setRuntimeString("cap_verify_url", cfg.Cap.VerifyURL)
			setRuntimeString("cap_secret_key", cfg.Cap.SecretKey)
		}
		if h.noticeSvc != nil {
			cfg := h.noticeSvc.Config()
			setDefault("feishu", cfg.Feishu.Enabled)
			setDefault("feishu_webhook", cfg.Feishu.Webhook)
			setDefault("feishu_secret", cfg.Feishu.Secret)
			setDefault("telegram", cfg.Telegram.Enabled)
			setDefault("tg_token", cfg.Telegram.BotToken)
			setDefault("tg_chat", cfg.Telegram.ChatID)
			setDefault("dingtalk", cfg.Dingtalk.Enabled)
			setDefault("ding_webhook", cfg.Dingtalk.Webhook)
			setDefault("ding_secret", cfg.Dingtalk.Secret)
			setDefault("wecom", cfg.WeCom.Enabled)
			setDefault("wecom_webhook", cfg.WeCom.Webhook)
			setDefault("bark", cfg.Bark.Enabled)
			setDefault("bark_key", cfg.Bark.Key)
			setDefault("bark_endpoint", cfg.Bark.Endpoint)
			setDefault("discord", cfg.Discord.Enabled)
			setDefault("discord_webhook", cfg.Discord.Webhook)
			setDefault("email", cfg.Email.Enabled)
			setDefault("email_host", cfg.Email.Host)
			setDefault("email_port", cfg.Email.Port)
			setDefault("email_user", cfg.Email.User)
			setDefault("email_pass", cfg.Email.Pass)
			setDefault("email_from", cfg.Email.From)
			setDefault("email_to", cfg.Email.To)
			setDefault("webhook", cfg.Webhook.Enabled)
			setDefault("webhook_url", cfg.Webhook.URL)
			setDefault("webhook_secret", cfg.Webhook.Secret)
		}
	}
	buf, _ := json.Marshal(raw)
	return string(buf)
}

func (h *AdminHandler) RotateSuffix(c *gin.Context) {
	newSuffix, err := h.adminSvc.RotateSuffix(c.Request.Context())
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	baseURL := requestBaseURL(c)
	h.noticeSvc.SendSuffixChanged(c.Request.Context(), newSuffix, baseURL)
	h.audit("admin", "", "suffix_rotate", "admin", newSuffix, c, "")

	JSON(c, http.StatusOK, gin.H{"suffix": newSuffix})
}

func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	} else if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto == "https" || proto == "http" {
		scheme = proto
	}
	host := c.Request.Host
	if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	host = sanitizeURLHost(host)
	if host == "" {
		host = "127.0.0.1"
	}
	return scheme + "://" + host
}

// ── Reports ──

func (h *AdminHandler) ListReports(c *gin.Context) {
	status, _ := strconv.Atoi(c.DefaultQuery("status", "0"))
	reports, err := h.reportSvc.List(c.Request.Context(), status)
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(c, http.StatusOK, reports)
}

// AutoProcessReports runs the bot + scanner + AI on all pending reports.
func (h *AdminHandler) AutoProcessReports(c *gin.Context) {
	reports, err := h.reportSvc.List(c.Request.Context(), 0)
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	if len(reports) == 0 {
		JSON(c, http.StatusOK, gin.H{"processed": 0, "message": "no pending reports"})
		return
	}

	// Load bot config from settings
	botCfg := &service.DefaultBotConfig
	settings, _ := h.adminSvc.GetSettings(c.Request.Context())
	if settings != nil && settings.SettingsEnc != "" {
		plain, _ := h.adminSvc.DecryptSettings(settings.SettingsEnc)
		var s struct {
			BotMode         string  `json:"bot_mode"`
			BotMinTrust     float64 `json:"bot_min_trust"`
			BotMinReports   int64   `json:"bot_min_reports"`
			BotMaxAutoDaily int64   `json:"bot_max_auto_daily"`
		}
		if err := json.Unmarshal([]byte(plain), &s); err == nil && s.BotMode != "" {
			botCfg.Mode = service.BotMode(s.BotMode)
			if s.BotMinTrust > 0 {
				botCfg.MinTrust = s.BotMinTrust
			}
			if s.BotMinReports > 0 {
				botCfg.MinReports = s.BotMinReports
			}
			if s.BotMaxAutoDaily > 0 {
				botCfg.MaxAutoDaily = s.BotMaxAutoDaily
			}
		}
	}

	approved, rejected, pending := 0, 0, 0
	for _, report := range reports {
		decryptedURL, _, err := h.linkSvc.Resolve(c.Request.Context(), report.ShortCode)
		if err != nil {
			rejected++
			h.reportSvc.Handle(c.Request.Context(), report.ID, false, "auto:link-gone")
			continue
		}

		action, reason := h.reportSvc.AutoProcessReport(c.Request.Context(), &report, decryptedURL, botCfg)

		switch action {
		case "approve":
			_ = h.linkSvc.Delete(c.Request.Context(), report.ShortCode)
			h.reportSvc.Handle(c.Request.Context(), report.ID, true, reason)
			approved++
		case "reject":
			h.reportSvc.Handle(c.Request.Context(), report.ID, false, reason)
			rejected++
		default:
			pending++
		}
	}

	JSON(c, http.StatusOK, gin.H{
		"processed": approved + rejected,
		"approved":  approved,
		"rejected":  rejected,
		"pending":   pending,
		"total":     len(reports),
		"bot_mode":  botCfg.Mode,
	})
}

func (h *AdminHandler) HandleReport(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req struct {
		Action string `json:"action"` // "approve_delete", "reject", "ban_reporter"
	}
	c.ShouldBindJSON(&req)

	switch req.Action {
	case "approve_delete":
		if err := h.reportSvc.ApproveAndDelete(c.Request.Context(), id, h.linkSvc); err != nil {
			Error(c, http.StatusInternalServerError, err.Error())
			return
		}
	case "ban_reporter":
		// Find the report so we can ban the ACTUAL reporter's IP hash,
		// not the currently-authenticated admin's IP.
		reports, _ := h.reportSvc.List(c.Request.Context(), -1)
		var reporterHash string
		for _, r := range reports {
			if r.ID == id {
				reporterHash = r.ReporterIP
				break
			}
		}
		if reporterHash == "" {
			Error(c, http.StatusNotFound, "report not found")
			return
		}
		_ = h.reportSvc.BanReporterByHash(c.Request.Context(), reporterHash, "admin manual ban")
		_ = h.reportSvc.Handle(c.Request.Context(), id, false, "manual-ban")
	default:
		_ = h.reportSvc.Handle(c.Request.Context(), id, false, "manual")
	}
	h.audit("admin", "", "report_handle", "report", strconv.FormatUint(id, 10), c, req.Action)
	JSON(c, http.StatusOK, gin.H{"message": "handled"})
}

// ── Audit & Domains ──

func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	if h.auditSvc == nil {
		JSON(c, http.StatusOK, gin.H{"list": []gin.H{}, "total": 0})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	logs, total, err := h.auditSvc.List(c.Request.Context(), (page-1)*size, size)
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(c, http.StatusOK, gin.H{"list": logs, "total": total})
}

func (h *AdminHandler) ListDomains(c *gin.Context) {
	if h.domainSvc == nil {
		JSON(c, http.StatusOK, []model.Domain{})
		return
	}
	domains, err := h.domainSvc.List(c.Request.Context())
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(c, http.StatusOK, domains)
}

func (h *AdminHandler) CreateDomain(c *gin.Context) {
	var d model.Domain
	if err := c.ShouldBindJSON(&d); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.domainSvc.Create(c.Request.Context(), &d); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "domain_create", "domain", d.Hostname, c, "")
	JSON(c, http.StatusOK, d)
}

func (h *AdminHandler) UpdateDomain(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var d model.Domain
	if err := c.ShouldBindJSON(&d); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.domainSvc.Update(c.Request.Context(), id, &d); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "domain_update", "domain", strconv.FormatUint(id, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"message": "updated"})
}

func (h *AdminHandler) DeleteDomain(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.domainSvc.Delete(c.Request.Context(), id); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "domain_delete", "domain", strconv.FormatUint(id, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"message": "deleted"})
}

// ── Bans ──

func (h *AdminHandler) ListBans(c *gin.Context) {
	bans, err := h.banSvc.List(c.Request.Context())
	if err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(c, http.StatusOK, bans)
}

func (h *AdminHandler) Unban(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.banSvc.Unban(c.Request.Context(), id); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "unban", "ban", strconv.FormatUint(id, 10), c, "")
	JSON(c, http.StatusOK, gin.H{"message": "unbanned"})
}

func (h *AdminHandler) ReloadWordFilter(c *gin.Context) {
	if err := h.wordFilterSvc.Reload(c.Request.Context()); err != nil {
		Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	h.audit("admin", "", "wordfilter_reload", "wordfilter", "", c, "")
	JSON(c, http.StatusOK, gin.H{"message": "reloaded"})
}

// ── Status ──

func (h *AdminHandler) SystemStatus(c *gin.Context) {
	ctx := c.Request.Context()

	dbStatus := "ok"
	redisStatus := "ok"

	sqlDB := h.adminSvc.GetDB()
	if sqlDB == nil {
		dbStatus = "未连接"
	} else if err := sqlDB.WithContext(ctx).Raw("SELECT 1").Error; err != nil {
		dbStatus = "异常"
	}

	redisOk, _ := h.adminSvc.CheckRedis(ctx)
	if redisOk {
		redisStatus = "正常"
	} else {
		redisStatus = "未配置"
	}

	// Real runtime metrics — replaces the previous placeholder strings.
	up := time.Since(bootTime)
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// Current suffix (from persisted admin settings) so ops can confirm
	// the daily-rotation and manual-rotate wired everything through.
	suffix := ""
	if s, err := h.adminSvc.GetSettings(ctx); err == nil && s != nil {
		suffix = s.Suffix
	}

	activeBans, _ := h.banSvc.CountActive(ctx)
	pendingReports := 0
	if reports, err := h.reportSvc.List(ctx, 0); err == nil {
		pendingReports = len(reports)
	}

	container := readCgroupMemory()
	hostMem := readHostMemory()
	load := readLoadAvg()
	disk := readDiskUsage("/")

	JSON(c, http.StatusOK, gin.H{
		"database": gin.H{"status": dbStatus, "type": "MySQL"},
		"redis":    gin.H{"status": redisStatus},
		"server": gin.H{
			"version":         version.Get(),
			"uptime_seconds":  int64(up.Seconds()),
			"uptime":          formatDuration(up),
			"go_version":      runtime.Version(),
			"cpu_count":       runtime.NumCPU(),
			"goroutines":      runtime.NumGoroutine(),
			"mem_alloc_mb":    bytesToMB(mem.Alloc),
			"mem_heap_mb":     bytesToMB(mem.HeapAlloc),
			"mem_sys_mb":      bytesToMB(mem.Sys),
			"mem_stack_mb":    bytesToMB(mem.StackInuse),
			"num_gc":          mem.NumGC,
			"last_gc_seconds": secondsSinceUnixNano(mem.LastGC),
		},
		"container": container,
		"host":      gin.H{"memory": hostMem, "load": load, "disk": disk},
		"admin": gin.H{
			"suffix":          suffix,
			"active_bans":     activeBans,
			"pending_reports": pendingReports,
		},
	})
}

func bytesToMB(v uint64) uint64 { return v / 1024 / 1024 }

func secondsSinceUnixNano(n uint64) int64 {
	if n == 0 {
		return -1
	}
	return int64(time.Since(time.Unix(0, int64(n))).Seconds())
}

func readUintFile(path string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "max" || s == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

func readCgroupMemory() gin.H {
	usage, okUsage := readUintFile("/sys/fs/cgroup/memory.current")
	limit, okLimit := readUintFile("/sys/fs/cgroup/memory.max")
	if !okUsage {
		usage, okUsage = readUintFile("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	}
	if !okLimit {
		limit, okLimit = readUintFile("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	}
	percent := 0.0
	if okUsage && okLimit && limit > 0 && limit < math.MaxInt64 {
		percent = math.Round(float64(usage)/float64(limit)*1000) / 10
	}
	return gin.H{
		"memory_usage_mb": bytesToMB(usage),
		"memory_limit_mb": func() uint64 {
			if okLimit {
				return bytesToMB(limit)
			}
			return 0
		}(),
		"memory_percent": percent,
		"limit_known":    okLimit && limit > 0 && limit < math.MaxInt64,
	}
}

func readHostMemory() gin.H {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return gin.H{}
	}
	defer f.Close()
	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64) // kB
		vals[strings.TrimSuffix(fields[0], ":")] = v * 1024
	}
	total := vals["MemTotal"]
	available := vals["MemAvailable"]
	used := uint64(0)
	if total > available {
		used = total - available
	}
	percent := 0.0
	if total > 0 {
		percent = math.Round(float64(used)/float64(total)*1000) / 10
	}
	return gin.H{"total_mb": bytesToMB(total), "available_mb": bytesToMB(available), "used_mb": bytesToMB(used), "used_percent": percent}
}

func readLoadAvg() gin.H {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return gin.H{}
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return gin.H{}
	}
	return gin.H{"load1": fields[0], "load5": fields[1], "load15": fields[2]}
}

func readDiskUsage(path string) gin.H {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return gin.H{}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	percent := 0.0
	if total > 0 {
		percent = math.Round(float64(used)/float64(total)*1000) / 10
	}
	return gin.H{"path": path, "total_mb": bytesToMB(total), "used_mb": bytesToMB(used), "free_mb": bytesToMB(free), "used_percent": percent}
}

// formatDuration renders a duration like "3d 4h 12m" — short, no seconds
// after the first hour. Used only for admin-facing display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return strconv.FormatInt(int64(d.Seconds()), 10) + "s"
	}
	days := int64(d / (24 * time.Hour))
	hours := int64(d%(24*time.Hour)) / int64(time.Hour)
	mins := int64(d%time.Hour) / int64(time.Minute)
	switch {
	case days > 0:
		return strconv.FormatInt(days, 10) + "d " + strconv.FormatInt(hours, 10) + "h " + strconv.FormatInt(mins, 10) + "m"
	case hours > 0:
		return strconv.FormatInt(hours, 10) + "h " + strconv.FormatInt(mins, 10) + "m"
	default:
		return strconv.FormatInt(mins, 10) + "m"
	}
}

// GetVersion returns the current runtime version. Safe to expose without
// authentication — the value is intentionally displayed on the admin UI
// side-bar (and public pages if desired).
func (h *AdminHandler) GetVersion(c *gin.Context) {
	JSON(c, http.StatusOK, gin.H{"version": version.Get()})
}

// ── Helpers ──

func (h *AdminHandler) reloadRateLimit(settingsJSON string) {
	var s struct {
		RateCreate    int    `json:"rate_create"`
		RateRedirect  int    `json:"rate_redirect"`
		RateWhitelist string `json:"rate_whitelist"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &s); err != nil {
		return
	}
	if s.RateCreate > 0 || s.RateRedirect > 0 {
		cfg := middleware.GetRateLimitConfig()
		newCfg := *cfg
		if s.RateCreate > 0 {
			newCfg.CreatePerMinute = s.RateCreate
		}
		if s.RateRedirect > 0 {
			newCfg.RedirectPerMinute = s.RateRedirect
		}
		middleware.SetRateLimitConfig(&newCfg)
	}
	middleware.SetRateLimitWhitelist(s.RateWhitelist)
}

func (h *AdminHandler) reloadReportConfig(settingsJSON string) {
	if h.reportSvc == nil {
		return
	}
	var s struct {
		DailyLimit   int `json:"report_daily_limit"`
		MinInterval  int `json:"report_min_interval"`
		AutoBanAfter int `json:"report_auto_ban"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &s); err != nil {
		return
	}
	h.reportSvc.ReloadConfig(service.ReportConfig{DailyLimit: s.DailyLimit, MinInterval: s.MinInterval, AutoBanAfter: s.AutoBanAfter})
}

func (h *AdminHandler) reloadTurnstile(settingsJSON string) {
	var s struct {
		CFEnabled bool   `json:"cf_enabled"`
		CFSiteKey string `json:"cf_site_key"`
		CFSecret  string `json:"cf_secret_key"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &s); err != nil {
		return
	}
	middleware.SetCFTurnstileConfig(&config.CloudflareConfig{
		Enabled:   s.CFEnabled,
		SiteKey:   s.CFSiteKey,
		SecretKey: s.CFSecret,
	})
}

func (h *AdminHandler) reloadCaptcha(settingsJSON string) {
	if h.captchaSvc == nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &raw); err != nil {
		return
	}
	cfg := h.captchaSvc.Config()
	setBool := func(key string, dest *bool) {
		if v, ok := raw[key]; ok {
			_ = json.Unmarshal(v, dest)
		}
	}
	setInt := func(key string, dest *int) {
		if v, ok := raw[key]; ok {
			_ = json.Unmarshal(v, dest)
		}
	}
	setString := func(key string, dest *string) {
		if v, ok := raw[key]; ok {
			_ = json.Unmarshal(v, dest)
		}
	}
	setBool("captcha_enabled", &cfg.Enabled)
	setString("captcha_provider", &cfg.Provider)
	setString("captcha_mode", &cfg.Mode)
	setString("captcha_normal_provider", &cfg.NormalProvider)
	setString("captcha_escalation_provider", &cfg.EscalationProvider)
	setInt("captcha_failure_threshold", &cfg.FailureThreshold)
	setInt("captcha_risk_window_seconds", &cfg.RiskWindowSeconds)
	setString("cap_site_key", &cfg.Cap.SiteKey)
	setString("cap_secret_key", &cfg.Cap.SecretKey)
	setString("cap_verify_url", &cfg.Cap.VerifyURL)
	setString("cap_api_endpoint", &cfg.Cap.APIEndpoint)
	setBool("playcaptcha_enabled", &cfg.PlayCaptcha.Enabled)
	setString("playcaptcha_site_key", &cfg.PlayCaptcha.SiteKey)
	setString("playcaptcha_secret_key", &cfg.PlayCaptcha.SecretKey)
	setString("playcaptcha_endpoint", &cfg.PlayCaptcha.Endpoint)
	h.captchaSvc.ReloadConfig(cfg)
}

func (h *AdminHandler) reloadModeration(settingsJSON string) {
	var s struct {
		ModEnabled   bool   `json:"mod_enabled"`
		ModOpenAIKey string `json:"mod_openai_key"`
		ModAutoDel   bool   `json:"mod_auto_delete"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &s); err != nil {
		return
	}
	h.modSvc.ReloadConfig(&service.ModerationConfig{
		Enabled:    s.ModEnabled,
		OpenAIKey:  s.ModOpenAIKey,
		AutoDelete: s.ModAutoDel,
	})
}

// reloadVersion applies a runtime version override from the admin settings
// JSON. Empty string resets to the compile-time default so operators can
// revert the display without redeploying.
func (h *AdminHandler) reloadVersion(settingsJSON string) {
	var s struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &s); err != nil {
		return
	}
	version.Set(s.Version)
}

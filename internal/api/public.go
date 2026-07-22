package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"shortlink/internal/config"
	"shortlink/internal/model"
	"shortlink/internal/service"
	"shortlink/internal/version"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type publicMessage struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Level   string `json:"level"`
	Enabled bool   `json:"enabled"`
}

type dynamicSettings struct {
	SettingsLoaded    bool `json:"-"`
	QRShowDirectSet   bool `json:"-"`
	TTLAllowNeverSet  bool `json:"-"`
	CaptchaEnabledSet bool `json:"-"`

	CFEnabled                 bool            `json:"cf_enabled"`
	CFSiteKey                 string          `json:"cf_site_key"`
	CFSecret                  string          `json:"cf_secret_key"`
	CaptchaEnabled            bool            `json:"captcha_enabled"`
	CaptchaProvider           string          `json:"captcha_provider"`
	CaptchaMode               string          `json:"captcha_mode"`
	CaptchaNormalProvider     string          `json:"captcha_normal_provider"`
	CaptchaEscalationProvider string          `json:"captcha_escalation_provider"`
	CaptchaFailureThreshold   int             `json:"captcha_failure_threshold"`
	CaptchaRiskWindowSeconds  int             `json:"captcha_risk_window_seconds"`
	CapSiteKey                string          `json:"cap_site_key"`
	CapAPIEndpoint            string          `json:"cap_api_endpoint"`
	BGEnabled                 bool            `json:"bg_enabled"`
	BGURL                     string          `json:"bg_url"`
	BGType                    string          `json:"bg_type"`
	FaviconURL                string          `json:"favicon_url"`
	PublicMessages            []publicMessage `json:"public_messages"`
	TTLDefault                int             `json:"ttl_default_seconds"`
	TTLMax                    int             `json:"ttl_max_seconds"`
	TTLAllowNever             bool            `json:"ttl_allow_never"`
	TTLOptions                []int           `json:"ttl_options"`
	QRShowDirect              bool            `json:"qr_show_direct"`
	QRAllowUser               bool            `json:"qr_allow_user_customize"`
	QRDefaultText             string          `json:"qr_default_text"`
	QRDefaultTpl              string          `json:"qr_default_template"`
	QRLogoURL                 string          `json:"qr_logo_url"`
	QRLogoEnabled             bool            `json:"qr_logo_enabled"`
	DomainAllowlist           string          `json:"domain_allowlist"`
	DomainBlocklist           string          `json:"domain_blocklist"`
	RiskInterstitialEnabled   bool            `json:"risk_interstitial_enabled"`
	RiskInterstitialThreshold int             `json:"risk_interstitial_threshold"`
	RiskBlockThreshold        int             `json:"risk_block_threshold"`
}

type PublicHandler struct {
	linkSvc       *service.LinkService
	apiKeySvc     *service.APIKeyService
	wordFilterSvc *service.WordFilterService
	banSvc        *service.BanService
	adminSvc      *service.AdminService
	reportSvc     *service.ReportService
	modSvc        *service.ModerationService
	safeScanner   *service.SafeScanner
	statusSvc     *service.StatusService
	cfg           *config.Config
}

func NewPublicHandler(linkSvc *service.LinkService, apiKeySvc *service.APIKeyService, wordFilterSvc *service.WordFilterService, banSvc *service.BanService,
	adminSvc *service.AdminService, reportSvc *service.ReportService, modSvc *service.ModerationService, safeScanner *service.SafeScanner, statusSvc *service.StatusService, cfg *config.Config) *PublicHandler {
	return &PublicHandler{
		linkSvc: linkSvc, apiKeySvc: apiKeySvc, wordFilterSvc: wordFilterSvc, banSvc: banSvc,
		adminSvc: adminSvc, reportSvc: reportSvc, modSvc: modSvc, safeScanner: safeScanner, statusSvc: statusSvc, cfg: cfg,
	}
}

func (h *PublicHandler) loadDynamicSettings() *dynamicSettings {
	ds := &dynamicSettings{}
	if h == nil || h.adminSvc == nil {
		return ds
	}
	settings, err := h.adminSvc.GetSettings(context.Background())
	if err != nil || settings.SettingsEnc == "" {
		return ds
	}
	plain, err := h.adminSvc.DecryptSettings(settings.SettingsEnc)
	if err != nil {
		return ds
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plain), &raw); err == nil {
		ds.SettingsLoaded = true
		_, ds.QRShowDirectSet = raw["qr_show_direct"]
		_, ds.TTLAllowNeverSet = raw["ttl_allow_never"]
		_, ds.CaptchaEnabledSet = raw["captcha_enabled"]
	}
	_ = json.Unmarshal([]byte(plain), ds)
	return ds
}

type linkPolicyInput struct {
	URL                 string
	CustomCode          string
	Password            string
	ExpiresAt           string
	TTL                 int
	QRText              string
	QRTemplate          string
	ApplyDefaultTTL     bool
	EnforceAPIKeyDomain bool
}

type linkPolicyResult struct {
	NormalizedURL string
	Host          string
	ExpiresAt     *time.Time
	Decision      service.DomainDecision
	QRText        string
	QRTemplate    string
	QRShowDirect  bool
	QRLogoURL     string
	QRLogoEnabled bool
}

type policyError struct {
	status  int
	message string
}

func (e *policyError) Error() string { return e.message }

func (h *PublicHandler) writePolicyError(c *gin.Context, err *policyError) {
	if err != nil {
		Error(c, err.status, err.message)
	}
}

func (h *PublicHandler) checkWordFilterTexts(c *gin.Context, texts ...string) *policyError {
	for _, text := range texts {
		if text == "" {
			continue
		}
		found, level, match := h.wordFilterSvc.Check(text)
		if !found {
			continue
		}
		switch level {
		case 1:
			return &policyError{status: http.StatusBadRequest, message: "content contains prohibited words"}
		case 2:
			h.banSvc.BanIP(c.Request.Context(), c.ClientIP(), "word_medium", 1*60*60)
			return &policyError{status: http.StatusForbidden, message: "content violates policy, IP temporarily banned"}
		case 3:
			h.banSvc.BanIP(c.Request.Context(), c.ClientIP(), "word_heavy", 24*60*60)
			return &policyError{status: http.StatusForbidden, message: "severe violation, IP banned"}
		default:
			return &policyError{status: http.StatusBadRequest, message: "content contains prohibited words: " + match}
		}
	}
	return nil
}

type ttlPolicy struct {
	Default    int
	Max        int
	AllowNever bool
	Options    []int
}

func effectiveTTLAllowNever(ds *dynamicSettings) bool {
	return normalizeTTLPolicy(ds).AllowNever
}

func normalizeTTLPolicy(ds *dynamicSettings) ttlPolicy {
	policy := ttlPolicy{
		Default:    0,
		Max:        365 * 24 * 3600,
		AllowNever: true,
		Options:    []int{0, 3600, 86400, 604800, 2592000},
	}
	if ds != nil && ds.SettingsLoaded {
		if ds.TTLMax > 0 {
			policy.Max = ds.TTLMax
		}
		if ds.TTLDefault > 0 {
			policy.Default = ds.TTLDefault
		}
		if ds.TTLAllowNeverSet {
			policy.AllowNever = ds.TTLAllowNever
		} else {
			policy.AllowNever = ds.TTLMax == 0 && ds.TTLDefault == 0
		}
		if len(ds.TTLOptions) > 0 {
			policy.Options = ds.TTLOptions
		}
	}
	policy.Options = normalizeTTLOptions(policy.Options, policy.AllowNever, policy.Max)
	if policy.Default < 0 {
		policy.Default = 0
	}
	if policy.Default > 0 && policy.Max > 0 && policy.Default > policy.Max {
		policy.Default = policy.Max
	}
	if !policy.AllowNever && policy.Default == 0 {
		for _, opt := range policy.Options {
			if opt > 0 {
				policy.Default = opt
				break
			}
		}
	}
	if len(policy.Options) == 0 {
		if policy.AllowNever {
			policy.Options = []int{0}
		} else if policy.Default > 0 {
			policy.Options = []int{policy.Default}
		}
	}
	return policy
}

func normalizeTTLOptions(options []int, allowNever bool, maxTTL int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(options))
	for _, opt := range options {
		if opt < 0 {
			continue
		}
		if opt == 0 && !allowNever {
			continue
		}
		if opt > 0 && maxTTL > 0 && opt > maxTTL {
			continue
		}
		if seen[opt] {
			continue
		}
		seen[opt] = true
		result = append(result, opt)
	}
	sort.Ints(result)
	return result
}

func qrShowDirectEnabled(ds *dynamicSettings) bool {
	if ds == nil || !ds.SettingsLoaded || !ds.QRShowDirectSet {
		return true
	}
	return ds.QRShowDirect
}

func normalizeQRTemplate(tpl string) string {
	switch strings.ToLower(strings.TrimSpace(tpl)) {
	case "classic", "card", "compact":
		return strings.ToLower(strings.TrimSpace(tpl))
	default:
		return "classic"
	}
}

func (h *PublicHandler) resolveQRPolicy(ds *dynamicSettings, qrText, qrTemplate string, allowUserInput bool) (string, string, *policyError) {
	if ds == nil {
		ds = &dynamicSettings{}
	}
	text := strings.TrimSpace(qrText)
	templateInput := strings.TrimSpace(qrTemplate)
	if (text != "" || templateInput != "") && !allowUserInput && !ds.QRAllowUser {
		return "", "", &policyError{status: http.StatusBadRequest, message: "qr customization is disabled"}
	}
	if text == "" {
		text = strings.TrimSpace(ds.QRDefaultText)
	}
	if len([]rune(text)) > 120 {
		return "", "", &policyError{status: http.StatusBadRequest, message: "qr text too long"}
	}
	tpl := qrTemplate
	if strings.TrimSpace(tpl) == "" {
		tpl = ds.QRDefaultTpl
	}
	tpl = normalizeQRTemplate(tpl)
	return text, tpl, nil
}

func (h *PublicHandler) parseExpiryWithPolicy(ds *dynamicSettings, expiresAt string, ttl int, applyDefault bool) (*time.Time, bool, *policyError) {
	var expAt *time.Time
	setExp := false
	now := time.Now()
	policy := normalizeTTLPolicy(ds)

	if ttl < 0 {
		return nil, false, &policyError{status: http.StatusBadRequest, message: "ttl must not be negative"}
	}
	if strings.TrimSpace(expiresAt) != "" {
		setExp = true
		if strings.EqualFold(strings.TrimSpace(expiresAt), "never") {
			if !policy.AllowNever {
				return nil, false, &policyError{status: http.StatusBadRequest, message: "permanent links are not allowed"}
			}
		} else if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
			expAt = &t
		} else if d, durErr := time.ParseDuration(expiresAt); durErr == nil {
			t := now.Add(d)
			expAt = &t
		} else {
			return nil, false, &policyError{status: http.StatusBadRequest, message: "invalid expires_at"}
		}
	} else if ttl > 0 {
		setExp = true
		t := now.Add(time.Duration(ttl) * time.Second)
		expAt = &t
	}

	if expAt != nil {
		if expAt.Before(now) {
			return nil, false, &policyError{status: http.StatusBadRequest, message: "expires_at must be in the future"}
		}
		if policy.Max > 0 && expAt.After(now.Add(time.Duration(policy.Max)*time.Second)) {
			return nil, false, &policyError{status: http.StatusBadRequest, message: "expiration exceeds maximum TTL"}
		}
	} else if !setExp && applyDefault && !policy.AllowNever {
		if policy.Default <= 0 {
			return nil, false, &policyError{status: http.StatusBadRequest, message: "default TTL is required when permanent links are disabled"}
		}
		t := now.Add(time.Duration(policy.Default) * time.Second)
		expAt = &t
		setExp = true
	}

	return expAt, setExp, nil
}

func (h *PublicHandler) evaluateDestinationPolicy(c *gin.Context, rawURL string, ds *dynamicSettings, enforceAPIKeyDomain bool) (*service.NormalizedURL, service.DomainDecision, *policyError) {
	normalized, err := service.NormalizeDestinationURL(rawURL, false)
	if err != nil {
		return nil, service.DomainDecision{}, &policyError{status: http.StatusBadRequest, message: err.Error()}
	}
	if enforceAPIKeyDomain {
		if keyAny, ok := c.Get("api_key"); ok {
			if key, ok := keyAny.(*model.APIKey); ok {
				if err := h.apiKeySvc.CheckDomainAllowed(key, normalized.Host); err != nil {
					return nil, service.DomainDecision{}, &policyError{status: http.StatusForbidden, message: err.Error()}
				}
			}
		}
	}
	if flagged, reason := h.modSvc.CheckURL(c.Request.Context(), normalized.URL); flagged {
		return nil, service.DomainDecision{}, &policyError{status: http.StatusBadRequest, message: "URL flagged by content moderation: " + reason}
	}

	var scan *service.ScanResult
	if h.safeScanner != nil {
		scan = h.safeScanner.ScanURL(normalized.URL)
	}
	if ds == nil {
		ds = &dynamicSettings{}
	}
	interstitialEnabled := ds.RiskInterstitialEnabled || (ds.RiskInterstitialThreshold == 0 && ds.RiskBlockThreshold == 0 && ds.DomainAllowlist == "" && ds.DomainBlocklist == "")
	decision := service.EvaluateDomainPolicy(normalized.Host, scan, service.DomainPolicyConfig{
		Allowlist:           ds.DomainAllowlist,
		Blocklist:           ds.DomainBlocklist,
		InterstitialEnabled: interstitialEnabled,
		WarnThreshold:       ds.RiskInterstitialThreshold,
		BlockThreshold:      ds.RiskBlockThreshold,
	})
	if decision.Blocked {
		return nil, service.DomainDecision{}, &policyError{status: http.StatusBadRequest, message: "URL blocked by risk policy"}
	}
	return normalized, decision, nil
}

func (h *PublicHandler) validateCreateLinkPolicy(c *gin.Context, input linkPolicyInput) (*linkPolicyResult, *policyError) {
	ds := h.loadDynamicSettings()
	qrText, qrTemplate, qrErr := h.resolveQRPolicy(ds, input.QRText, input.QRTemplate, false)
	if qrErr != nil {
		return nil, qrErr
	}
	if err := h.checkWordFilterTexts(c, input.URL, input.CustomCode, input.Password, qrText); err != nil {
		return nil, err
	}
	normalized, decision, err := h.evaluateDestinationPolicy(c, input.URL, ds, input.EnforceAPIKeyDomain)
	if err != nil {
		return nil, err
	}
	expAt, _, err := h.parseExpiryWithPolicy(ds, input.ExpiresAt, input.TTL, input.ApplyDefaultTTL)
	if err != nil {
		return nil, err
	}
	logoEnabled := ds != nil && ds.QRLogoEnabled && strings.TrimSpace(ds.QRLogoURL) != ""
	logoURL := ""
	if logoEnabled {
		logoURL = strings.TrimSpace(ds.QRLogoURL)
	}
	return &linkPolicyResult{
		NormalizedURL: normalized.URL,
		Host:          normalized.Host,
		ExpiresAt:     expAt,
		Decision:      decision,
		QRText:        qrText,
		QRTemplate:    qrTemplate,
		QRShowDirect:  qrShowDirectEnabled(ds),
		QRLogoURL:     logoURL,
		QRLogoEnabled: logoEnabled,
	}, nil
}

func (h *PublicHandler) markRiskIfNeeded(ctx context.Context, code string, decision service.DomainDecision) {
	if decision.Score > 0 || decision.Warn {
		buf, _ := json.Marshal(decision.Reasons)
		_ = h.linkSvc.MarkRisk(ctx, code, decision.Score, decision.Level, string(buf), decision.Warn)
	}
}

func publicRequestBaseURL(c *gin.Context, trustForwarded bool) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	} else if trustForwarded && strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	if trustForwarded {
		if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" {
			host = strings.Split(forwardedHost, ",")[0]
		}
	}
	host = sanitizeURLHost(host)
	if host == "" {
		host = "127.0.0.1"
	}
	return scheme + "://" + host
}

func sanitizeURLHost(host string) string {
	host = strings.TrimSpace(strings.Split(host, ",")[0])
	if host == "" || strings.ContainsAny(host, "/\\?#@") {
		return ""
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		if h == "" || strings.ContainsAny(h, "/\\?#@") {
			return ""
		}
		return net.JoinHostPort(h, p)
	}
	return host
}

func buildShortURL(baseURL, code string) string {
	return strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(code)
}

func buildManageURL(baseURL, code, token string) string {
	return strings.TrimRight(baseURL, "/") + "/manage?code=" + url.QueryEscape(code) + "&token=" + url.QueryEscape(token)
}

// ── CreateLink ──

func (h *PublicHandler) CreateLink(c *gin.Context) {
	var req struct {
		URL          string `json:"url" form:"url" binding:"required"`
		CustomCode   string `json:"custom_code" form:"custom_code"`
		Password     string `json:"password" form:"password"`
		ExpiresAt    string `json:"expires_at" form:"expires_at"`
		TTL          int    `json:"ttl" form:"ttl"`
		IsOnce       bool   `json:"is_once" form:"is_once"`
		PrivacyAgree bool   `json:"privacy_agree" form:"privacy_agree"`
		Visibility   int    `json:"visibility" form:"visibility"`
		QRText       string `json:"qr_text" form:"qr_text"`
		QRTemplate   string `json:"qr_template" form:"qr_template"`
	}

	if err := c.ShouldBind(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	apiKeyIDAny, isAPIKeyRequest := c.Get("api_key_id")
	if h.cfg.Features.RequirePrivacyAgree && !isAPIKeyRequest && !req.PrivacyAgree {
		Error(c, http.StatusBadRequest, "must agree to privacy policy and disclaimer")
		return
	}
	if !h.cfg.Features.AllowCustomCode {
		req.CustomCode = ""
	}

	policy, policyErr := h.validateCreateLinkPolicy(c, linkPolicyInput{
		URL:                 req.URL,
		CustomCode:          req.CustomCode,
		Password:            req.Password,
		ExpiresAt:           req.ExpiresAt,
		TTL:                 req.TTL,
		QRText:              req.QRText,
		QRTemplate:          req.QRTemplate,
		ApplyDefaultTTL:     true,
		EnforceAPIKeyDomain: isAPIKeyRequest,
	})
	if policyErr != nil {
		h.writePolicyError(c, policyErr)
		return
	}

	var createdByAPIKeyID *uint64
	if isAPIKeyRequest {
		if id, ok := apiKeyIDAny.(uint64); ok {
			createdByAPIKeyID = &id
		}
	}
	result, err := h.linkSvc.CreateWithOptions(c.Request.Context(), service.CreateOptions{OriginalURL: policy.NormalizedURL, CustomCode: req.CustomCode, Password: req.Password, ExpiresAt: policy.ExpiresAt, IsOnce: req.IsOnce, Visibility: req.Visibility, CreatedByAPIKeyID: createdByAPIKeyID})
	if err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if policy.QRText != "" || policy.QRTemplate != "" {
		qrText, qrTemplate := policy.QRText, policy.QRTemplate
		_ = h.linkSvc.UpdateByUser(c.Request.Context(), result.Link.ShortCode, result.EditToken, service.UserUpdateOptions{QRText: &qrText, QRTemplate: &qrTemplate})
	}
	h.markRiskIfNeeded(c.Request.Context(), result.Link.ShortCode, policy.Decision)

	baseURL := publicRequestBaseURL(c, h.cfg.Server.TrustedProxy)
	shortURL := buildShortURL(baseURL, result.Link.ShortCode)
	manageURL := buildManageURL(baseURL, result.Link.ShortCode, result.EditToken)
	JSON(c, http.StatusOK, gin.H{
		"short_code":      result.Link.ShortCode,
		"short_url":       shortURL,
		"edit_token":      result.EditToken,
		"manage_url":      manageURL,
		"qr_show_direct":  policy.QRShowDirect,
		"qr_text":         policy.QRText,
		"qr_template":     policy.QRTemplate,
		"qr_logo_enabled": policy.QRLogoEnabled,
		"qr_logo_url":     policy.QRLogoURL,
	})
}

// ── GetConfig ──

func (h *PublicHandler) GetConfig(c *gin.Context) {
	bgEnabled := h.cfg.Background.Enabled
	bgURL := h.cfg.Background.URL
	bgType := h.cfg.Background.Type
	if bgType == "" {
		bgType = "image"
	}
	cfEnabled := h.cfg.Cloudflare.Enabled
	cfSiteKey := h.cfg.Cloudflare.SiteKey
	captchaCfg := h.cfg.Captcha

	faviconURL := ""
	messages := []gin.H{}
	ttlPolicyCfg := normalizeTTLPolicy(nil)
	ttlDefault := ttlPolicyCfg.Default
	ttlMax := ttlPolicyCfg.Max
	ttlAllowNever := ttlPolicyCfg.AllowNever
	ttlOptions := ttlPolicyCfg.Options
	qrShowDirect := true
	qrAllowUser := false
	qrDefaultText := ""
	qrDefaultTpl := "classic"
	qrLogoURL := ""
	qrLogoEnabled := false
	if ds := h.loadDynamicSettings(); ds != nil {
		if ds.CFEnabled || ds.CFSiteKey != "" {
			cfEnabled = ds.CFEnabled
			cfSiteKey = ds.CFSiteKey
		}
		if ds.CaptchaEnabledSet || ds.CaptchaProvider != "" || ds.CapSiteKey != "" || ds.CapAPIEndpoint != "" {
			if ds.CaptchaEnabledSet {
				captchaCfg.Enabled = ds.CaptchaEnabled
			}
			if ds.CaptchaProvider != "" {
				captchaCfg.Provider = ds.CaptchaProvider
			}
			if ds.CaptchaMode != "" {
				captchaCfg.Mode = ds.CaptchaMode
			}
			if ds.CaptchaNormalProvider != "" {
				captchaCfg.NormalProvider = ds.CaptchaNormalProvider
			}
			if ds.CaptchaEscalationProvider != "" {
				captchaCfg.EscalationProvider = ds.CaptchaEscalationProvider
			}
			if ds.CaptchaFailureThreshold > 0 {
				captchaCfg.FailureThreshold = ds.CaptchaFailureThreshold
			}
			if ds.CaptchaRiskWindowSeconds > 0 {
				captchaCfg.RiskWindowSeconds = ds.CaptchaRiskWindowSeconds
			}
			if ds.CapSiteKey != "" {
				captchaCfg.Cap.SiteKey = ds.CapSiteKey
			}
			if ds.CapAPIEndpoint != "" {
				captchaCfg.Cap.APIEndpoint = ds.CapAPIEndpoint
			}
			captchaCfg.Cap.SecretKey = ""
		}
		if ds.BGURL != "" {
			// A configured URL should visibly take effect even if an older admin UI
			// did not persist bg_enabled=true. Clearing the URL disables it.
			bgEnabled = true
			bgURL = ds.BGURL
			bgType = ds.BGType
		}
		faviconURL = ds.FaviconURL
		ttlPolicyCfg = normalizeTTLPolicy(ds)
		ttlDefault = ttlPolicyCfg.Default
		ttlMax = ttlPolicyCfg.Max
		ttlAllowNever = ttlPolicyCfg.AllowNever
		ttlOptions = ttlPolicyCfg.Options
		qrShowDirect = qrShowDirectEnabled(ds)
		qrAllowUser = ds.QRAllowUser
		qrDefaultText = ds.QRDefaultText
		if ds.QRDefaultTpl != "" {
			qrDefaultTpl = normalizeQRTemplate(ds.QRDefaultTpl)
		}
		qrLogoEnabled = ds.QRLogoEnabled && strings.TrimSpace(ds.QRLogoURL) != ""
		if qrLogoEnabled {
			qrLogoURL = strings.TrimSpace(ds.QRLogoURL)
		}
		for _, m := range ds.PublicMessages {
			title := strings.TrimSpace(m.Title)
			content := strings.TrimSpace(m.Content)
			if !m.Enabled || content == "" {
				continue
			}
			level := strings.TrimSpace(m.Level)
			if level == "" {
				level = "info"
			}
			messages = append(messages, gin.H{"title": title, "content": content, "level": level})
		}
	}

	JSON(c, http.StatusOK, gin.H{
		"cf_enabled":                  cfEnabled,
		"cf_site_key":                 cfSiteKey,
		"captcha_enabled":             captchaCfg.Enabled,
		"captcha_provider":            captchaCfg.Provider,
		"captcha_mode":                captchaCfg.Mode,
		"captcha_normal_provider":     captchaCfg.NormalProvider,
		"captcha_escalation_provider": captchaCfg.EscalationProvider,
		"captcha_failure_threshold":   captchaCfg.FailureThreshold,
		"captcha_risk_window_seconds": captchaCfg.RiskWindowSeconds,
		"cap_site_key":                captchaCfg.Cap.SiteKey,
		"cap_api_endpoint":            captchaCfg.Cap.APIEndpoint,
		"bg_enabled":                  bgEnabled,
		"bg_url":                      bgURL,
		"bg_type":                     bgType,
		"require_privacy":             h.cfg.Features.RequirePrivacyAgree,
		"allow_custom":                h.cfg.Features.AllowCustomCode,
		"version":                     version.Get(),
		"favicon_url":                 faviconURL,
		"public_messages":             messages,
		"ttl_default_seconds":         ttlDefault,
		"ttl_max_seconds":             ttlMax,
		"ttl_allow_never":             ttlAllowNever,
		"ttl_options":                 ttlOptions,
		"qr_show_direct":              qrShowDirect,
		"qr_allow_user_customize":     qrAllowUser,
		"qr_default_text":             qrDefaultText,
		"qr_default_template":         qrDefaultTpl,
		"qr_logo_url":                 qrLogoURL,
		"qr_logo_enabled":             qrLogoEnabled,
	})
}

// ── PublicStatus ──

func (h *PublicHandler) PublicStatus(c *gin.Context) {
	if h.statusSvc == nil {
		Error(c, http.StatusServiceUnavailable, "status unavailable")
		return
	}
	status, err := h.statusSvc.PublicStatus(c.Request.Context(), time.Now())
	if err != nil {
		Error(c, http.StatusServiceUnavailable, "status unavailable")
		return
	}
	JSON(c, http.StatusOK, status)
}

func (h *PublicHandler) BatchCreateLinks(c *gin.Context) {
	var req struct {
		Items []struct {
			URL        string `json:"url"`
			CustomCode string `json:"custom_code"`
			Password   string `json:"password"`
			TTL        int    `json:"ttl"`
			ExpiresAt  string `json:"expires_at"`
			IsOnce     bool   `json:"is_once"`
			Visibility int    `json:"visibility"`
			QRText     string `json:"qr_text"`
			QRTemplate string `json:"qr_template"`
		} `json:"items"`
		Options struct {
			ContinueOnError bool `json:"continue_on_error"`
		} `json:"options"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	if len(req.Items) > 50 {
		Error(c, http.StatusBadRequest, "too many items, max 50")
		return
	}
	if keyAny, ok := c.Get("api_key"); ok && len(req.Items) > 1 {
		if key, ok := keyAny.(*model.APIKey); ok {
			if err := h.apiKeySvc.CheckQuota(c.Request.Context(), key, len(req.Items)-1); err != nil {
				Error(c, http.StatusTooManyRequests, err.Error())
				return
			}
		}
	}
	baseURL := publicRequestBaseURL(c, h.cfg.Server.TrustedProxy)
	var createdByAPIKeyID *uint64
	if keyIDAny, ok := c.Get("api_key_id"); ok {
		if id, ok := keyIDAny.(uint64); ok {
			createdByAPIKeyID = &id
		}
	}
	results := []gin.H{}
	created, failed := 0, 0
	seen := map[string]bool{}
	for i, item := range req.Items {
		customCode := item.CustomCode
		if !h.cfg.Features.AllowCustomCode {
			customCode = ""
		}
		if customCode != "" {
			if seen[customCode] {
				failed++
				results = append(results, gin.H{"index": i, "ok": false, "error": "duplicate custom_code in batch"})
				if !req.Options.ContinueOnError {
					break
				}
				continue
			}
			seen[customCode] = true
		}

		policy, policyErr := h.validateCreateLinkPolicy(c, linkPolicyInput{
			URL:                 item.URL,
			CustomCode:          customCode,
			Password:            item.Password,
			ExpiresAt:           item.ExpiresAt,
			TTL:                 item.TTL,
			QRText:              item.QRText,
			QRTemplate:          item.QRTemplate,
			ApplyDefaultTTL:     true,
			EnforceAPIKeyDomain: true,
		})
		if policyErr != nil {
			failed++
			results = append(results, gin.H{"index": i, "ok": false, "error": policyErr.message})
			if !req.Options.ContinueOnError {
				break
			}
			continue
		}

		res, err := h.linkSvc.CreateWithOptions(c.Request.Context(), service.CreateOptions{OriginalURL: policy.NormalizedURL, CustomCode: customCode, Password: item.Password, ExpiresAt: policy.ExpiresAt, IsOnce: item.IsOnce, Visibility: item.Visibility, CreatedByAPIKeyID: createdByAPIKeyID})
		if err != nil {
			failed++
			results = append(results, gin.H{"index": i, "ok": false, "error": err.Error()})
			if !req.Options.ContinueOnError {
				break
			}
			continue
		}
		if policy.QRText != "" || policy.QRTemplate != "" {
			qrText, qrTemplate := policy.QRText, policy.QRTemplate
			if err := h.linkSvc.UpdateByUser(c.Request.Context(), res.Link.ShortCode, res.EditToken, service.UserUpdateOptions{QRText: &qrText, QRTemplate: &qrTemplate}); err != nil {
				failed++
				results = append(results, gin.H{"index": i, "ok": false, "error": err.Error()})
				if !req.Options.ContinueOnError {
					break
				}
				continue
			}
		}
		h.markRiskIfNeeded(c.Request.Context(), res.Link.ShortCode, policy.Decision)
		created++
		shortURL := buildShortURL(baseURL, res.Link.ShortCode)
		manageURL := buildManageURL(baseURL, res.Link.ShortCode, res.EditToken)
		results = append(results, gin.H{"index": i, "ok": true, "short_code": res.Link.ShortCode, "short_url": shortURL, "edit_token": res.EditToken, "manage_url": manageURL, "qr_show_direct": policy.QRShowDirect, "qr_text": policy.QRText, "qr_template": policy.QRTemplate, "qr_logo_enabled": policy.QRLogoEnabled, "qr_logo_url": policy.QRLogoURL})
	}
	JSON(c, http.StatusOK, gin.H{"total": len(req.Items), "created": created, "failed": failed, "results": results})
}

// ── User-managed links ──

func (h *PublicHandler) ManagePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"><title>管理短链</title><style>body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Noto Sans SC',sans-serif;background:#eef4ff;color:#152033;min-height:100vh;display:flex;align-items:center;justify-content:center;margin:0}.card{width:min(520px,92vw);background:#fff;border-radius:24px;padding:30px;box-shadow:0 24px 70px rgba(28,59,118,.16)}h1{margin:0 0 8px}.sub{color:#64748b;margin-bottom:22px}.fld{margin:12px 0}.fld label{display:block;font-size:12px;font-weight:800;color:#52627a;margin-bottom:6px}.fld input{width:100%;box-sizing:border-box;padding:13px 14px;border:1px solid #dbe6f5;border-radius:14px;font-size:14px}.row{display:flex;gap:10px;margin-top:18px}.btn{flex:1;border:0;border-radius:14px;padding:13px;background:#3157ff;color:#fff;font-weight:900;cursor:pointer}.btn.danger{background:#dd3b4a}.msg{margin-top:12px;font-size:13px;color:#dd3b4a}.ok{color:#16a36a}</style></head><body><div class="card"><h1>管理短链</h1><div class="sub">使用创建时返回的管理链接编辑或删除短链。</div><div class="fld"><label>短码</label><input id="code"></div><div class="fld"><label>编辑令牌</label><input id="token"></div><div class="fld"><label>新目标链接</label><input id="url" placeholder="https://example.com"></div><div class="fld"><label>访问密码（留空不修改）</label><input id="password" type="password"></div><div class="fld"><label>二维码文字</label><input id="qr_text" maxlength="120"></div><div class="row"><button class="btn" onclick="save()">保存修改</button><button class="btn danger" onclick="delLink()">删除短链</button></div><div class="msg" id="msg"></div></div><script>const q=new URLSearchParams(location.search);code.value=q.get('code')||'';token.value=q.get('token')||'';async function save(){msg.textContent='';const body={edit_token:token.value,url:url.value,password:password.value,qr_text:qr_text.value};const r=await fetch('/api/links/'+encodeURIComponent(code.value)+'/manage',{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});const d=await r.json();msg.className='msg '+(r.ok?'ok':'');msg.textContent=r.ok?'已保存':(d.error||'保存失败')}async function delLink(){if(!confirm('确认删除此短链？'))return;const r=await fetch('/api/links/'+encodeURIComponent(code.value)+'/manage',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({edit_token:token.value})});const d=await r.json();msg.className='msg '+(r.ok?'ok':'');msg.textContent=r.ok?'已删除':(d.error||'删除失败')}</script></body></html>`))
}

func (h *PublicHandler) GetManagedLink(c *gin.Context) {
	code := c.Param("code")
	token := c.Query("token")
	link, url, err := h.linkSvc.GetByEditToken(c.Request.Context(), code, token)
	if err != nil {
		Error(c, http.StatusNotFound, err.Error())
		return
	}
	JSON(c, http.StatusOK, gin.H{"short_code": link.ShortCode, "url": url, "expires_at": link.ExpiresAt, "has_password": link.PasswordHash != "", "is_once": link.IsOnce, "visibility": link.Visibility, "qr_text": link.QRText, "qr_template": link.QRTemplate})
}

func (h *PublicHandler) UpdateManagedLink(c *gin.Context) {
	code := c.Param("code")
	var req struct {
		EditToken     string `json:"edit_token"`
		URL           string `json:"url"`
		Password      string `json:"password"`
		ClearPassword bool   `json:"clear_password"`
		ExpiresAt     string `json:"expires_at"`
		TTL           int    `json:"ttl"`
		IsOnce        *bool  `json:"is_once"`
		Visibility    *int   `json:"visibility"`
		QRText        string `json:"qr_text"`
		QRTemplate    string `json:"qr_template"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(req.EditToken) == "" {
		Error(c, http.StatusBadRequest, "edit token is required")
		return
	}
	link, _, err := h.linkSvc.GetByEditToken(c.Request.Context(), code, req.EditToken)
	if err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	ds := h.loadDynamicSettings()
	qrText, qrTemplate := "", ""
	if strings.TrimSpace(req.QRText) != "" || strings.TrimSpace(req.QRTemplate) != "" {
		var qrErr *policyError
		qrText, qrTemplate, qrErr = h.resolveQRPolicy(ds, req.QRText, req.QRTemplate, false)
		if qrErr != nil {
			h.writePolicyError(c, qrErr)
			return
		}
	}
	if policyErr := h.checkWordFilterTexts(c, req.URL, req.Password, qrText); policyErr != nil {
		h.writePolicyError(c, policyErr)
		return
	}

	expAt, setExp, policyErr := h.parseExpiryWithPolicy(ds, req.ExpiresAt, req.TTL, false)
	if policyErr != nil {
		h.writePolicyError(c, policyErr)
		return
	}

	urlToUpdate := req.URL
	decision := service.DomainDecision{Level: "safe"}
	if strings.TrimSpace(req.URL) != "" {
		normalized, evaluated, policyErr := h.evaluateDestinationPolicy(c, req.URL, ds, false)
		if policyErr != nil {
			h.writePolicyError(c, policyErr)
			return
		}
		if link.CreatedByAPIKeyID != nil {
			key, err := h.apiKeySvc.GetByID(c.Request.Context(), *link.CreatedByAPIKeyID)
			if err != nil {
				Error(c, http.StatusForbidden, err.Error())
				return
			}
			if err := h.apiKeySvc.CheckDomainAllowed(key, normalized.Host); err != nil {
				Error(c, http.StatusForbidden, err.Error())
				return
			}
		}
		urlToUpdate = normalized.URL
		decision = evaluated
	}

	var pw *string
	if req.Password != "" || req.ClearPassword {
		pw = &req.Password
	}
	var qr, qrTpl *string
	if strings.TrimSpace(req.QRText) != "" || strings.TrimSpace(req.QRTemplate) != "" {
		qr = &qrText
		qrTpl = &qrTemplate
	}
	if err := h.linkSvc.UpdateByUser(c.Request.Context(), code, req.EditToken, service.UserUpdateOptions{URL: urlToUpdate, Password: pw, ClearPassword: req.ClearPassword, ExpiresAt: expAt, SetExpiresAt: setExp, IsOnce: req.IsOnce, Visibility: req.Visibility, QRText: qr, QRTemplate: qrTpl}); err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.URL) != "" {
		h.markRiskIfNeeded(c.Request.Context(), code, decision)
	}
	JSON(c, http.StatusOK, gin.H{"message": "saved"})
}

func (h *PublicHandler) DeleteManagedLink(c *gin.Context) {
	var req struct {
		EditToken string `json:"edit_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.EditToken == "" {
		req.EditToken = c.Query("token")
	}
	if err := h.linkSvc.DeleteByUser(c.Request.Context(), c.Param("code"), req.EditToken); err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	JSON(c, http.StatusOK, gin.H{"message": "deleted"})
}

// ── Report ──

func (h *PublicHandler) SubmitReport(c *gin.Context) {
	var req struct {
		ShortCode  string `json:"short_code" binding:"required"`
		Reason     string `json:"reason" binding:"required"`
		CustomText string `json:"custom_text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, http.StatusBadRequest, "invalid request")
		return
	}

	// Run custom text through word filter
	if req.CustomText != "" {
		if found, level, match := h.wordFilterSvc.Check(req.CustomText); found {
			h.handleWordFilter(c, level, match)
			return
		}
		if flagged, reason := h.modSvc.CheckReportText(c.Request.Context(), req.CustomText); flagged {
			Error(c, http.StatusBadRequest, "content flagged: "+reason)
			return
		}
	}

	ip := c.ClientIP()
	if err := h.reportSvc.Submit(c.Request.Context(), req.ShortCode, req.Reason, req.CustomText, ip); err != nil {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	JSON(c, http.StatusOK, gin.H{"message": "report submitted, thank you"})
}

// ── Word filter helper ──

func (h *PublicHandler) handleWordFilter(c *gin.Context, level int, match string) {
	switch level {
	case 1:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "content contains prohibited words"})
	case 2:
		h.banSvc.BanIP(c.Request.Context(), c.ClientIP(), "word_medium", 1*60*60)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "content violates policy, IP temporarily banned"})
	case 3:
		h.banSvc.BanIP(c.Request.Context(), c.ClientIP(), "word_heavy", 24*60*60)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "severe violation, IP banned"})
	}
}

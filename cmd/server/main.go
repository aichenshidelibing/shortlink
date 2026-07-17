package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"shortlink/internal/api"
	"shortlink/internal/auth"
	"shortlink/internal/config"
	"shortlink/internal/crypto"
	"shortlink/internal/filter"
	"shortlink/internal/middleware"
	"shortlink/internal/repository"
	"shortlink/internal/service"
	"shortlink/internal/version"
	"shortlink/internal/worker"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func configureTrustedProxies(r *gin.Engine, enabled bool) error {
	if !enabled {
		return r.SetTrustedProxies(nil)
	}
	return r.SetTrustedProxies([]string{
		"127.0.0.1",
		"::1",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	})
}

func rawSetting(settingsJSON, key string) (json.RawMessage, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &raw); err != nil {
		return nil, false
	}
	v, ok := raw[key]
	return v, ok
}

func mountCapProxy(r gin.IRouter, target string) {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "http://cap:3000"
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "cap service unavailable", http.StatusBadGateway)
	}
	handler := gin.WrapH(http.StripPrefix("/cap", proxy))
	r.Any("/cap", handler)
	r.Any("/cap/*path", handler)
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		panic(fmt.Sprintf("load config: %v", err))
	}

	log, errLog := zap.NewProduction()
	if errLog != nil {
		panic(fmt.Sprintf("init logger: %v", errLog))
	}
	if cfg.Server.Mode == "debug" {
		log, errLog = zap.NewDevelopment()
		if errLog != nil {
			panic(fmt.Sprintf("init dev logger: %v", errLog))
		}
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	defer log.Sync()

	if cfg.Admin.Password == "" {
		log.Fatal("ADMIN_PASSWORD is required")
	}
	if cfg.Encryption.Key == "" {
		log.Fatal("ENCRYPTION_KEY is required")
	}
	if cfg.Admin.SessionSecret == "" {
		cfg.Admin.SessionSecret = cfg.Encryption.Key
	}
	if cfg.Shortlink.HashidsSalt == "" {
		cfg.Shortlink.HashidsSalt = cfg.Encryption.Key
	}

	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatal("connect db", zap.Error(err))
	}
	if err := repository.AutoMigrate(db); err != nil {
		log.Fatal("migrate db", zap.Error(err))
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Warn("redis ping failed", zap.Error(err))
	}

	strongKey := crypto.MustGetKey(cfg.Encryption.Key)
	strong := crypto.NewStrongCrypto(strongKey)
	weak := crypto.NewWeakCrypto(cfg.Encryption.Key)
	cryptoMgr := crypto.NewCryptoManager(strong, weak)

	linkRepo := repository.NewLinkRepository(db)
	adminRepo := repository.NewAdminRepository(db)
	apiKeyRepo := repository.NewAPIKeyRepository(db)
	clickRepo := repository.NewClickRepository(db)
	banRepo := repository.NewBanRepository(db)
	wordRepo := repository.NewWordRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cacheRepo := repository.NewCacheRepository(rdb)

	dfa := filter.NewDFA()

	adminSvc := service.NewAdminService(&cfg.Admin, adminRepo, auth.NewTOTP(), auth.NewSessionManager(cfg.Admin.SessionSecret), cryptoMgr, db, rdb, log)
	linkSvc := service.NewLinkService(&cfg.Shortlink, linkRepo, cacheRepo, adminRepo, strong, log)
	apiKeySvc := service.NewAPIKeyService(apiKeyRepo, log, cacheRepo)
	statsSvc := service.NewStatsService(clickRepo, linkRepo, log)
	banSvc := service.NewBanService(banRepo, strong, log)
	auditSvc := service.NewAuditService(auditRepo, log)
	domainSvc := service.NewDomainService(domainRepo)
	wordFilterSvc := service.NewWordFilterService(wordRepo, dfa, log)
	noticeSvc := service.NewNoticeService(&cfg.Notification, log)
	safeScanner := service.NewSafeScanner(log)
	modSvc := service.NewModerationService(&service.ModerationConfig{}, log)
	captchaSvc := service.NewCaptchaService(&cfg.Captcha, cacheRepo)
	reportBot := service.NewReportBot(db, log)
	reportSvc := service.NewReportService(linkRepo, safeScanner, modSvc, reportBot, log)

	if err := adminSvc.InitAdmin(context.Background()); err != nil {
		log.Fatal("init admin", zap.Error(err))
	}

	// Bootstrap runtime state (version override, notice channels, rate-limit,
	// moderation) from the persisted admin settings. Without this the app
	// only sees admin-panel overrides after the operator re-saves the form.
	if s, err := adminSvc.GetSettings(context.Background()); err == nil && s != nil && s.SettingsEnc != "" {
		if plain, derr := adminSvc.DecryptSettings(s.SettingsEnc); derr == nil && plain != "" {
			var boot struct {
				Version                   string `json:"version"`
				RateCreate                int    `json:"rate_create"`
				RateRedirect              int    `json:"rate_redirect"`
				RateWhitelist             string `json:"rate_whitelist"`
				CFEnabled                 bool   `json:"cf_enabled"`
				CFSiteKey                 string `json:"cf_site_key"`
				CFSecret                  string `json:"cf_secret_key"`
				CaptchaEnabled            bool   `json:"captcha_enabled"`
				CaptchaProvider           string `json:"captcha_provider"`
				CaptchaMode               string `json:"captcha_mode"`
				CaptchaNormalProvider     string `json:"captcha_normal_provider"`
				CaptchaEscalationProvider string `json:"captcha_escalation_provider"`
				CaptchaFailureThreshold   int    `json:"captcha_failure_threshold"`
				CaptchaRiskWindowSeconds  int    `json:"captcha_risk_window_seconds"`
				CapSiteKey                string `json:"cap_site_key"`
				CapSecretKey              string `json:"cap_secret_key"`
				CapVerifyURL              string `json:"cap_verify_url"`
				CapAPIEndpoint            string `json:"cap_api_endpoint"`
				ModEnabled                bool   `json:"mod_enabled"`
				ModOpenAIKey              string `json:"mod_openai_key"`
				ModAutoDel                bool   `json:"mod_auto_delete"`
			}
			if err := json.Unmarshal([]byte(plain), &boot); err == nil {
				if boot.Version != "" {
					version.Set(boot.Version)
				}
				_, hasCaptchaEnabled := rawSetting(plain, "captcha_enabled")
				_, hasCaptchaProvider := rawSetting(plain, "captcha_provider")
				_, hasCapSiteKey := rawSetting(plain, "cap_site_key")
				_, hasCapSecretKey := rawSetting(plain, "cap_secret_key")
				_, hasCapVerifyURL := rawSetting(plain, "cap_verify_url")
				_, hasCapAPIEndpoint := rawSetting(plain, "cap_api_endpoint")
				if boot.RateCreate > 0 || boot.RateRedirect > 0 {
					rc := *middleware.GetRateLimitConfig()
					if boot.RateCreate > 0 {
						rc.CreatePerMinute = boot.RateCreate
					}
					if boot.RateRedirect > 0 {
						rc.RedirectPerMinute = boot.RateRedirect
					}
					middleware.SetRateLimitConfig(&rc)
				}
				middleware.SetRateLimitWhitelist(boot.RateWhitelist)
				middleware.SetCFTurnstileConfig(&config.CloudflareConfig{
					Enabled:   boot.CFEnabled,
					SiteKey:   boot.CFSiteKey,
					SecretKey: boot.CFSecret,
				})
				captchaCfg := captchaSvc.Config()
				if hasCaptchaEnabled {
					captchaCfg.Enabled = boot.CaptchaEnabled
				}
				if hasCaptchaProvider {
					captchaCfg.Provider = boot.CaptchaProvider
				}
				if boot.CaptchaMode != "" {
					captchaCfg.Mode = boot.CaptchaMode
				}
				if boot.CaptchaNormalProvider != "" {
					captchaCfg.NormalProvider = boot.CaptchaNormalProvider
				}
				if boot.CaptchaEscalationProvider != "" {
					captchaCfg.EscalationProvider = boot.CaptchaEscalationProvider
				}
				if boot.CaptchaFailureThreshold > 0 {
					captchaCfg.FailureThreshold = boot.CaptchaFailureThreshold
				}
				if boot.CaptchaRiskWindowSeconds > 0 {
					captchaCfg.RiskWindowSeconds = boot.CaptchaRiskWindowSeconds
				}
				if hasCapSiteKey {
					captchaCfg.Cap.SiteKey = boot.CapSiteKey
				}
				if hasCapSecretKey {
					captchaCfg.Cap.SecretKey = boot.CapSecretKey
				}
				if hasCapVerifyURL {
					captchaCfg.Cap.VerifyURL = boot.CapVerifyURL
				}
				if hasCapAPIEndpoint {
					captchaCfg.Cap.APIEndpoint = boot.CapAPIEndpoint
				}
				captchaSvc.ReloadConfig(captchaCfg)
				modSvc.ReloadConfig(&service.ModerationConfig{
					Enabled: boot.ModEnabled, OpenAIKey: boot.ModOpenAIKey, AutoDelete: boot.ModAutoDel,
				})
			}
			noticeSvc.ReloadFromJSON(plain)
		}
	}
	// Populate CSP background whitelist from persisted settings.
	middleware.ReloadCSPAssets(context.Background(), adminSvc)
	log.Info("shortlink starting", zap.String("version", version.Get()))

	if err := wordFilterSvc.Reload(context.Background()); err != nil {
		log.Warn("reload word filter", zap.Error(err))
	}

	// Seed default word list from GitHub Sensitive-lexicon (first run only)
	if _, err := os.Stat("data/default-words.txt"); err == nil {
		if n, err := wordFilterSvc.SeedFromFile(context.Background(), "data/default-words.txt", 1); err != nil {
			log.Warn("seed default words failed", zap.Error(err))
		} else if n > 0 {
			log.Info("seeded default sensitive words", zap.Int("count", n))
		}
	}

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	clickWorker := worker.NewClickWorker(clickRepo, log)
	clickWorker.Start(appCtx)

	// Periodic expired link cleanup (every hour)
	go func(ctx context.Context) {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n, err := linkSvc.CleanupExpired(ctx); err != nil {
					log.Warn("cleanup expired failed", zap.Error(err))
				} else if n > 0 {
					log.Info("cleaned expired links", zap.Int64("count", n))
				}
			case <-ctx.Done():
				return
			}
		}
	}(appCtx)

	// Periodic code recycling release (every 6 hours)
	go func(ctx context.Context) {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n, err := linkSvc.ReleaseCodes(ctx); err != nil {
					log.Warn("release codes failed", zap.Error(err))
				} else if n > 0 {
					log.Info("released recycled codes", zap.Int64("count", n))
				}
			case <-ctx.Done():
				return
			}
		}
	}(appCtx)

	r := gin.New()
	if err := configureTrustedProxies(r, cfg.Server.TrustedProxy); err != nil {
		log.Fatal("configure trusted proxies", zap.Error(err))
	}
	r.Use(gin.Recovery())
	r.Use(middleware.BodyLimit(0))
	r.Use(middleware.InputSanitize())
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.SuffixCheck(adminSvc, cacheRepo))
	r.Use(middleware.BanCheck(banSvc))
	r.Use(middleware.RateLimit(cacheRepo, &cfg.RateLimit))

	publicHandler := api.NewPublicHandler(linkSvc, apiKeySvc, wordFilterSvc, banSvc, adminSvc, reportSvc, modSvc, safeScanner, cfg)
	redirectHandler := api.NewRedirectHandler(linkSvc, linkRepo, clickRepo, clickWorker, strong, statsSvc)
	adminHandler := api.NewAdminHandler(adminSvc, linkSvc, apiKeySvc, statsSvc, banSvc, wordFilterSvc, noticeSvc, reportSvc, modSvc, auditSvc, domainSvc, captchaSvc, auth.NewSessionManager(cfg.Admin.SessionSecret))

	// Static
	r.StaticFile("/", "./web/public/dist/index.html")
	r.StaticFile("/index.html", "./web/public/dist/index.html")
	r.StaticFile("/help", "./web/public/dist/help.html")

	// Public API
	mountCapProxy(r, os.Getenv("CAP_INTERNAL_URL"))
	r.GET("/api/config", publicHandler.GetConfig)
	r.GET("/api/status", publicHandler.PublicStatus)
	r.POST("/api/links", middleware.CaptchaGuard(captchaSvc, &cfg.Cloudflare, service.CaptchaActionCreateLink), publicHandler.CreateLink)
	r.GET("/api/links/:code/manage", publicHandler.GetManagedLink)
	r.PATCH("/api/links/:code/manage", publicHandler.UpdateManagedLink)
	r.DELETE("/api/links/:code/manage", publicHandler.DeleteManagedLink)
	r.GET("/manage", publicHandler.ManagePage)
	r.POST("/api/report", middleware.CaptchaGuard(captchaSvc, &cfg.Cloudflare, service.CaptchaActionSubmitReport), publicHandler.SubmitReport)

	apiV1 := r.Group("/api/v1")
	apiV1.Use(middleware.APIKeyAuth(apiKeySvc))
	{
		apiV1.POST("/links", middleware.RequireAPIKeyPermission(apiKeySvc, "links:create"), publicHandler.CreateLink)
		apiV1.POST("/links/batch", middleware.RequireAPIKeyPermission(apiKeySvc, "links:batch_create"), publicHandler.BatchCreateLinks)
	}

	// Bootstrap the admin suffix. Route wiring below is anchored at a fixed
	// internal alias — the external suffix is applied dynamically by
	// AdminMux, so rotation works without a restart.
	suffix, _ := adminSvc.GetOrCreateSuffix(context.Background())
	log.Info("Admin suffix", zap.String("suffix", suffix))

	// Admin engine — mounted under a stable alias. Requests get here after
	// AdminMux rewrites /{active-suffix}/* to /__admin/*.
	adminEngine := gin.New()
	if err := configureTrustedProxies(adminEngine, cfg.Server.TrustedProxy); err != nil {
		log.Fatal("configure admin trusted proxies", zap.Error(err))
	}
	adminEngine.Use(gin.Recovery())
	adminEngine.Use(middleware.BodyLimit(0))
	adminEngine.Use(middleware.InputSanitize())
	adminEngine.Use(middleware.SecurityHeaders())
	adminEngine.Use(middleware.BanCheck(banSvc))
	adminEngine.Use(middleware.RateLimit(cacheRepo, &cfg.RateLimit))

	adminGroup := adminEngine.Group(middleware.AdminInternalPrefix)
	{
		// Public admin endpoints (no auth needed): login/logout only.
		// TOTP QR / verify were previously public — but that leaked the
		// otpauth URI (including secret) to anyone who knew the suffix,
		// and let unauthenticated callers flip TOTPVerified. Moved to authd.
		adminGroup.POST("/api/login", middleware.CaptchaGuard(captchaSvc, &cfg.Cloudflare, service.CaptchaActionAdminLogin), adminHandler.Login)
		adminGroup.POST("/api/logout", adminHandler.Logout)
		adminGroup.GET("/api/version", adminHandler.GetVersion)

		authd := adminGroup.Group("/api")
		authd.Use(middleware.AdminAuth(auth.NewSessionManager(cfg.Admin.SessionSecret)))
		{
			authd.GET("/totp", adminHandler.GetTOTP)
			authd.POST("/totp/verify", adminHandler.VerifyTOTP)
			authd.GET("/dashboard", adminHandler.Dashboard)
			authd.GET("/links", adminHandler.ListLinks)
			authd.DELETE("/links/:code", adminHandler.DeleteLink)
			authd.GET("/apikeys", adminHandler.ListAPIKeys)
			authd.POST("/apikeys", adminHandler.CreateAPIKey)
			authd.PATCH("/apikeys/:id", adminHandler.UpdateAPIKey)
			authd.POST("/apikeys/:id/revoke", adminHandler.RevokeAPIKey)
			authd.DELETE("/apikeys/:id", adminHandler.DeleteAPIKey)
			authd.GET("/stats/:code", adminHandler.GetStats)
			authd.GET("/settings", adminHandler.GetSettings)
			authd.PUT("/settings", adminHandler.SaveSettings)
			authd.POST("/settings/rotate-suffix", adminHandler.RotateSuffix)
			authd.GET("/bans", adminHandler.ListBans)
			authd.DELETE("/bans/:id", adminHandler.Unban)
			authd.POST("/wordfilter/reload", adminHandler.ReloadWordFilter)
			authd.GET("/status", adminHandler.SystemStatus)
			// Reports
			authd.GET("/reports", adminHandler.ListReports)
			authd.POST("/reports/:id", adminHandler.HandleReport)
			authd.POST("/reports/auto-process", adminHandler.AutoProcessReports)
			authd.GET("/audit", adminHandler.ListAuditLogs)
			authd.GET("/domains", adminHandler.ListDomains)
			authd.POST("/domains", adminHandler.CreateDomain)
			authd.PATCH("/domains/:id", adminHandler.UpdateDomain)
			authd.DELETE("/domains/:id", adminHandler.DeleteDomain)
		}

		adminGroup.Static("/dashboard", "./web/admin/dist")
		adminGroup.StaticFile("/", "./web/admin/dist/index.html")
	}

	// /:code catch-all MUST be registered on the public engine only.
	r.POST("/:code", redirectHandler.SubmitPassword)
	r.GET("/:code", redirectHandler.Redirect)

	// Wire the mux and hook it up to suffix changes so RotateSuffix takes
	// effect immediately.
	adminMux := middleware.NewAdminMux(r, adminEngine, suffix)
	adminSvc.OnSuffixChange(func(s string) {
		adminMux.SetSuffix(s)
		middleware.InvalidateSuffixCache()
		_ = cacheRepo.DeleteSuffix(context.Background())
		log.Info("admin suffix hot-swapped", zap.String("suffix", s))
	})

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func(ctx context.Context) {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := adminSvc.RotateSuffix(context.Background()); err == nil {
					newSuffix, _ := adminSvc.GetSettings(context.Background())
					if newSuffix != nil {
						noticeSvc.SendSuffixChanged(context.Background(), newSuffix.Suffix, "")
					}
				} else {
					log.Warn("rotate admin suffix failed", zap.Error(err))
				}
			case <-ctx.Done():
				return
			}
		}
	}(appCtx)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: adminMux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("server", zap.Error(err))
		}
	}()

	<-quit
	cancelApp()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown", zap.Error(err))
	}
}

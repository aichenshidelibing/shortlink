package service

import (
	"context"
	"encoding/json"
	"fmt"
	"shortlink/internal/config"
	"shortlink/internal/notice"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const noticeSendTimeout = 8 * time.Second

type NoticeService struct {
	mu        sync.RWMutex
	providers []notice.Notifier
	log       *zap.Logger
	baseCfg   atomic.Value // config.NotificationConfig
}

func NewNoticeService(cfg *config.NotificationConfig, log *zap.Logger) *NoticeService {
	s := &NoticeService{log: log}
	s.ReloadFromConfig(cfg)
	return s
}

func (s *NoticeService) ReloadFromConfig(cfg *config.NotificationConfig) {
	copy := normalizeNotificationConfig(cfg)
	s.baseCfg.Store(&copy)
	s.initFromConfig(&copy)
}

func (s *NoticeService) currentConfig() config.NotificationConfig {
	if v := s.baseCfg.Load(); v != nil {
		return *(v.(*config.NotificationConfig))
	}
	return normalizeNotificationConfig(nil)
}

func (s *NoticeService) initFromConfig(cfg *config.NotificationConfig) {
	if cfg == nil {
		return
	}
	providers := make([]notice.Notifier, 0, 8)
	s.addProvider(&providers, cfg.Feishu.Enabled, "feishu", map[string]string{"webhook": cfg.Feishu.Webhook}, func() notice.Notifier {
		return notice.NewFeishuNotifier(cfg.Feishu.Webhook, cfg.Feishu.Secret)
	})
	s.addProvider(&providers, cfg.Telegram.Enabled, "telegram", map[string]string{"bot_token": cfg.Telegram.BotToken, "chat_id": cfg.Telegram.ChatID}, func() notice.Notifier {
		return notice.NewTelegramNotifier(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
	})
	s.addProvider(&providers, cfg.Dingtalk.Enabled, "dingtalk", map[string]string{"webhook": cfg.Dingtalk.Webhook}, func() notice.Notifier {
		return notice.NewDingtalkNotifier(cfg.Dingtalk.Webhook, cfg.Dingtalk.Secret)
	})
	s.addProvider(&providers, cfg.WeCom.Enabled, "wecom", map[string]string{"webhook": cfg.WeCom.Webhook}, func() notice.Notifier {
		return notice.NewWeComNotifier(cfg.WeCom.Webhook)
	})
	s.addProvider(&providers, cfg.Bark.Enabled, "bark", map[string]string{"key": cfg.Bark.Key}, func() notice.Notifier {
		return notice.NewBarkNotifier(cfg.Bark.Key, cfg.Bark.Endpoint)
	})
	s.addProvider(&providers, cfg.Discord.Enabled, "discord", map[string]string{"webhook": cfg.Discord.Webhook}, func() notice.Notifier {
		return notice.NewDiscordNotifier(cfg.Discord.Webhook)
	})
	s.addProvider(&providers, cfg.Email.Enabled, "email", map[string]string{"host": cfg.Email.Host, "to": cfg.Email.To}, func() notice.Notifier {
		return notice.NewEmailNotifier(cfg.Email.Host, cfg.Email.Port, cfg.Email.User, cfg.Email.Pass, cfg.Email.From, cfg.Email.To)
	})
	s.addProvider(&providers, cfg.Webhook.Enabled, "webhook", map[string]string{"url": cfg.Webhook.URL}, func() notice.Notifier {
		return notice.NewWebhookNotifier(cfg.Webhook.URL, cfg.Webhook.Secret)
	})
	s.providers = providers
}

type noticeSettings struct {
	Feishu         bool   `json:"feishu"`
	FeishuWebhook  string `json:"feishu_webhook"`
	FeishuSecret   string `json:"feishu_secret"`
	Telegram       bool   `json:"telegram"`
	TgToken        string `json:"tg_token"`
	TgChat         string `json:"tg_chat"`
	Dingtalk       bool   `json:"dingtalk"`
	DingWebhook    string `json:"ding_webhook"`
	DingSecret     string `json:"ding_secret"`
	WeCom          bool   `json:"wecom"`
	WeComWebhook   string `json:"wecom_webhook"`
	Bark           bool   `json:"bark"`
	BarkKey        string `json:"bark_key"`
	BarkEndpoint   string `json:"bark_endpoint"`
	Discord        bool   `json:"discord"`
	DiscordWebhook string `json:"discord_webhook"`
	Email          bool   `json:"email"`
	EmailHost      string `json:"email_host"`
	EmailPort      string `json:"email_port"`
	EmailUser      string `json:"email_user"`
	EmailPass      string `json:"email_pass"`
	EmailFrom      string `json:"email_from"`
	EmailTo        string `json:"email_to"`
	Webhook        bool   `json:"webhook"`
	WebhookURL     string `json:"webhook_url"`
	WebhookSecret  string `json:"webhook_secret"`
}

func hasNoticeSettings(raw map[string]json.RawMessage) bool {
	for _, key := range []string{
		"feishu", "feishu_webhook", "feishu_secret",
		"telegram", "tg_token", "tg_chat",
		"dingtalk", "ding_webhook", "ding_secret",
		"wecom", "wecom_webhook",
		"bark", "bark_key", "bark_endpoint",
		"discord", "discord_webhook",
		"email", "email_host", "email_port", "email_user", "email_pass", "email_from", "email_to",
		"webhook", "webhook_url", "webhook_secret",
	} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

// ReloadFromJSON rebuilds notification providers from admin settings JSON.
func (s *NoticeService) ReloadFromJSON(settingsJSON string) {
	if settingsJSON == "" {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &raw); err != nil {
		s.warn("parse notice settings failed", zap.Error(err))
		return
	}
	if !hasNoticeSettings(raw) {
		return
	}
	var ns noticeSettings
	if err := json.Unmarshal([]byte(settingsJSON), &ns); err != nil {
		s.warn("parse notice settings failed", zap.Error(err))
		return
	}

	providers := s.providersFromSettings(ns)
	s.mu.Lock()
	s.providers = providers
	s.mu.Unlock()

	s.info("notice providers reloaded", zap.Int("count", len(providers)), zap.Strings("providers", providerNames(providers)))
}

func (s *NoticeService) Config() config.NotificationConfig {
	return s.currentConfig()
}

func normalizeNotificationConfig(cfg *config.NotificationConfig) config.NotificationConfig {
	var out config.NotificationConfig
	if cfg != nil {
		out = *cfg
	}
	return out
}

func (s *NoticeService) providersFromSettings(ns noticeSettings) []notice.Notifier {
	providers := make([]notice.Notifier, 0, 8)
	s.addProvider(&providers, ns.Feishu, "feishu", map[string]string{"webhook": ns.FeishuWebhook}, func() notice.Notifier {
		return notice.NewFeishuNotifier(ns.FeishuWebhook, ns.FeishuSecret)
	})
	s.addProvider(&providers, ns.Telegram, "telegram", map[string]string{"bot_token": ns.TgToken, "chat_id": ns.TgChat}, func() notice.Notifier {
		return notice.NewTelegramNotifier(ns.TgToken, ns.TgChat)
	})
	s.addProvider(&providers, ns.Dingtalk, "dingtalk", map[string]string{"webhook": ns.DingWebhook}, func() notice.Notifier {
		return notice.NewDingtalkNotifier(ns.DingWebhook, ns.DingSecret)
	})
	s.addProvider(&providers, ns.WeCom, "wecom", map[string]string{"webhook": ns.WeComWebhook}, func() notice.Notifier {
		return notice.NewWeComNotifier(ns.WeComWebhook)
	})
	s.addProvider(&providers, ns.Bark, "bark", map[string]string{"key": ns.BarkKey}, func() notice.Notifier {
		return notice.NewBarkNotifier(ns.BarkKey, ns.BarkEndpoint)
	})
	s.addProvider(&providers, ns.Discord, "discord", map[string]string{"webhook": ns.DiscordWebhook}, func() notice.Notifier {
		return notice.NewDiscordNotifier(ns.DiscordWebhook)
	})
	s.addProvider(&providers, ns.Email, "email", map[string]string{"host": ns.EmailHost, "to": ns.EmailTo}, func() notice.Notifier {
		return notice.NewEmailNotifier(ns.EmailHost, ns.EmailPort, ns.EmailUser, ns.EmailPass, ns.EmailFrom, ns.EmailTo)
	})
	s.addProvider(&providers, ns.Webhook, "webhook", map[string]string{"url": ns.WebhookURL}, func() notice.Notifier {
		return notice.NewWebhookNotifier(ns.WebhookURL, ns.WebhookSecret)
	})
	return providers
}

func (s *NoticeService) addProvider(providers *[]notice.Notifier, enabled bool, name string, required map[string]string, create func() notice.Notifier) {
	if !enabled {
		return
	}
	missing := make([]string, 0, len(required))
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		s.warn("notice provider enabled but incomplete", zap.String("provider", name), zap.Strings("missing", missing))
		return
	}
	*providers = append(*providers, create())
}

func (s *NoticeService) SendLoginEvent(ctx context.Context, success bool, ip string) {
	status := "❌ 失败"
	if success {
		status = "✅ 成功"
	}
	msg := fmt.Sprintf("【短链系统】管理员登录%s\nIP: %s\n时间: %s", status, ip, nowStr())
	s.broadcast(ctx, msg)
}

func (s *NoticeService) SendSuffixChanged(ctx context.Context, newSuffix, baseURL string) {
	entry := suffixEntry(baseURL, newSuffix)
	msg := fmt.Sprintf("【短链系统】管理后缀已更换\n新入口: %s\n时间: %s", entry, nowStr())
	s.broadcast(ctx, msg)
}

func (s *NoticeService) SendSuffixInfo(ctx context.Context, suffix, baseURL string) {
	entry := suffixEntry(baseURL, suffix)
	msg := fmt.Sprintf("【短链系统】当前管理后缀\n入口: %s\n时间: %s", entry, nowStr())
	s.broadcast(ctx, msg)
}

func (s *NoticeService) broadcast(ctx context.Context, msg string) {
	s.mu.RLock()
	providers := append([]notice.Notifier(nil), s.providers...)
	s.mu.RUnlock()

	if len(providers) == 0 {
		s.info("notice skipped: no providers enabled")
		return
	}

	var wg sync.WaitGroup
	for _, provider := range providers {
		p := provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			sendCtx, cancel := context.WithTimeout(ctx, noticeSendTimeout)
			defer cancel()
			if err := p.Send(sendCtx, msg); err != nil {
				s.warn("send notice failed", zap.String("provider", p.Name()), zap.Error(err))
				return
			}
			s.info("notice sent", zap.String("provider", p.Name()))
		}()
	}
	wg.Wait()
}

func suffixEntry(baseURL, suffix string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	suffix = strings.Trim(strings.TrimSpace(suffix), "/")
	if baseURL == "" {
		return "/" + suffix + "/"
	}
	return baseURL + "/" + suffix + "/"
}

func providerNames(providers []notice.Notifier) []string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name())
	}
	return names
}

func (s *NoticeService) warn(msg string, fields ...zap.Field) {
	if s != nil && s.log != nil {
		s.log.Warn(msg, fields...)
	}
}

func (s *NoticeService) info(msg string, fields ...zap.Field) {
	if s != nil && s.log != nil {
		s.log.Info(msg, fields...)
	}
}

func nowStr() string {
	// Display timestamps in the container's local timezone (typically
	// Asia/Shanghai, per Dockerfile TZ). Storage is UTC (see DatabaseConfig.DSN)
	// — only presentation strings should carry a local time.
	return time.Now().Local().Format("2006-01-02 15:04:05")
}

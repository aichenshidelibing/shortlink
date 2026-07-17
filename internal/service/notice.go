package service

import (
	"context"
	"encoding/json"
	"fmt"
	"shortlink/internal/config"
	"shortlink/internal/notice"
	"sync"
	"time"

	"go.uber.org/zap"
)

type NoticeService struct {
	mu        sync.RWMutex
	providers []notice.Notifier
	log       *zap.Logger
}

func NewNoticeService(cfg *config.NotificationConfig, log *zap.Logger) *NoticeService {
	s := &NoticeService{log: log}
	s.initFromConfig(cfg)
	return s
}

func (s *NoticeService) initFromConfig(cfg *config.NotificationConfig) {
	s.providers = nil
	if cfg.Feishu.Enabled && cfg.Feishu.Webhook != "" {
		s.providers = append(s.providers, notice.NewFeishuNotifier(cfg.Feishu.Webhook, cfg.Feishu.Secret))
	}
	if cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
		s.providers = append(s.providers, notice.NewTelegramNotifier(cfg.Telegram.BotToken, cfg.Telegram.ChatID))
	}
	if cfg.Dingtalk.Enabled && cfg.Dingtalk.Webhook != "" {
		s.providers = append(s.providers, notice.NewDingtalkNotifier(cfg.Dingtalk.Webhook, cfg.Dingtalk.Secret))
	}
	if cfg.WeCom.Enabled && cfg.WeCom.Webhook != "" {
		s.providers = append(s.providers, notice.NewWeComNotifier(cfg.WeCom.Webhook))
	}
	if cfg.Bark.Enabled && cfg.Bark.Key != "" {
		s.providers = append(s.providers, notice.NewBarkNotifier(cfg.Bark.Key, cfg.Bark.Endpoint))
	}
	if cfg.Discord.Enabled && cfg.Discord.Webhook != "" {
		s.providers = append(s.providers, notice.NewDiscordNotifier(cfg.Discord.Webhook))
	}
	if cfg.Email.Enabled && cfg.Email.Host != "" && cfg.Email.To != "" {
		s.providers = append(s.providers, notice.NewEmailNotifier(cfg.Email.Host, cfg.Email.Port, cfg.Email.User, cfg.Email.Pass, cfg.Email.From, cfg.Email.To))
	}
	if cfg.Webhook.Enabled && cfg.Webhook.URL != "" {
		s.providers = append(s.providers, notice.NewWebhookNotifier(cfg.Webhook.URL, cfg.Webhook.Secret))
	}
}

type noticeSettings struct {
	Feishu        bool   `json:"feishu"`
	FeishuWebhook string `json:"feishu_webhook"`
	FeishuSecret  string `json:"feishu_secret"`
	Telegram      bool   `json:"telegram"`
	TgToken       string `json:"tg_token"`
	TgChat        string `json:"tg_chat"`
	Dingtalk      bool   `json:"dingtalk"`
	DingWebhook   string `json:"ding_webhook"`
	DingSecret    string `json:"ding_secret"`
	// New channels
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

// ReloadFromJSON rebuilds notification providers from admin settings JSON.
func (s *NoticeService) ReloadFromJSON(settingsJSON string) {
	if settingsJSON == "" {
		return
	}
	var ns noticeSettings
	if err := json.Unmarshal([]byte(settingsJSON), &ns); err != nil {
		s.log.Warn("parse notice settings failed", zap.Error(err))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.providers = nil
	if ns.Feishu && ns.FeishuWebhook != "" {
		s.providers = append(s.providers, notice.NewFeishuNotifier(ns.FeishuWebhook, ns.FeishuSecret))
	}
	if ns.Telegram && ns.TgToken != "" && ns.TgChat != "" {
		s.providers = append(s.providers, notice.NewTelegramNotifier(ns.TgToken, ns.TgChat))
	}
	if ns.Dingtalk && ns.DingWebhook != "" {
		s.providers = append(s.providers, notice.NewDingtalkNotifier(ns.DingWebhook, ns.DingSecret))
	}
	if ns.WeCom && ns.WeComWebhook != "" {
		s.providers = append(s.providers, notice.NewWeComNotifier(ns.WeComWebhook))
	}
	if ns.Bark && ns.BarkKey != "" {
		s.providers = append(s.providers, notice.NewBarkNotifier(ns.BarkKey, ns.BarkEndpoint))
	}
	if ns.Discord && ns.DiscordWebhook != "" {
		s.providers = append(s.providers, notice.NewDiscordNotifier(ns.DiscordWebhook))
	}
	if ns.Email && ns.EmailHost != "" && ns.EmailTo != "" {
		s.providers = append(s.providers, notice.NewEmailNotifier(ns.EmailHost, ns.EmailPort, ns.EmailUser, ns.EmailPass, ns.EmailFrom, ns.EmailTo))
	}
	if ns.Webhook && ns.WebhookURL != "" {
		s.providers = append(s.providers, notice.NewWebhookNotifier(ns.WebhookURL, ns.WebhookSecret))
	}
	s.log.Info("notice providers reloaded", zap.Int("count", len(s.providers)))
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
	msg := fmt.Sprintf("【短链系统】管理后缀已更换\n新入口: %s/%s/\n时间: %s", baseURL, newSuffix, nowStr())
	s.broadcast(ctx, msg)
}

func (s *NoticeService) SendSuffixInfo(ctx context.Context, suffix, baseURL string) {
	msg := fmt.Sprintf("【短链系统】当前管理后缀\n入口: %s/%s/\n时间: %s", baseURL, suffix, nowStr())
	s.broadcast(ctx, msg)
}

func (s *NoticeService) broadcast(ctx context.Context, msg string) {
	s.mu.RLock()
	providers := s.providers
	s.mu.RUnlock()

	for _, p := range providers {
		if err := p.Send(ctx, msg); err != nil {
			s.log.Warn("send notice failed", zap.String("provider", p.Name()), zap.Error(err))
		}
	}
}

func nowStr() string {
	// Display timestamps in the container's local timezone (typically
	// Asia/Shanghai, per Dockerfile TZ). Storage is UTC (see DatabaseConfig.DSN)
	// — only presentation strings should carry a local time.
	return time.Now().Local().Format("2006-01-02 15:04:05")
}

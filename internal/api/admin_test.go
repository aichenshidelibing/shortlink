package api

import (
	"encoding/json"
	"testing"

	"shortlink/internal/config"
	"shortlink/internal/service"
)

func TestSettingsWithRuntimeDefaultsIncludesNotificationConfig(t *testing.T) {
	noticeSvc := service.NewNoticeService(&config.NotificationConfig{
		Feishu: config.FeishuConfig{
			Enabled: true,
			Webhook: "https://example.com/feishu",
			Secret:  "feishu-secret",
		},
		Telegram: config.TelegramConfig{
			Enabled:  true,
			BotToken: "bot-token",
			ChatID:   "chat-id",
		},
		Webhook: config.WebhookConfig{
			Enabled: true,
			URL:     "https://example.com/webhook",
			Secret:  "hook-secret",
		},
	}, nil)

	h := &AdminHandler{noticeSvc: noticeSvc}
	got := h.settingsWithRuntimeDefaults(`{}`)

	var raw map[string]any
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("unmarshal settings json: %v", err)
	}

	checks := map[string]any{
		"feishu":         true,
		"feishu_webhook": "https://example.com/feishu",
		"feishu_secret":  "feishu-secret",
		"telegram":       true,
		"tg_token":       "bot-token",
		"tg_chat":        "chat-id",
		"webhook":        true,
		"webhook_url":    "https://example.com/webhook",
		"webhook_secret": "hook-secret",
	}
	for key, want := range checks {
		if got := raw[key]; got != want {
			t.Fatalf("%s mismatch: got %#v want %#v", key, got, want)
		}
	}
}

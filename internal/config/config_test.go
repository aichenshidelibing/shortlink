package config

import "testing"

func TestRejectsKnownDefaultAdminPassword(t *testing.T) {
	cfg := &Config{Admin: AdminConfig{Password: "admin123"}}
	if err := cfg.ValidateSecurity(); err == nil {
		t.Fatal("expected default admin password to be rejected")
	}
}

func TestAcceptsNonDefaultAdminPassword(t *testing.T) {
	cfg := &Config{Admin: AdminConfig{Password: "not-the-default-password"}}
	if err := cfg.ValidateSecurity(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestLoadAllowsMissingExplicitConfigFileWhenEnvIsSet(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("ADMIN_PASSWORD", "not-the-default-password")
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")

	cfg, err := Load("/tmp/shortlink-config-test-does-not-exist.yaml")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("server port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Admin.Password != "not-the-default-password" {
		t.Fatalf("admin password was not loaded from env")
	}
}

func TestLoadNotificationFromEnvWithoutConfigFile(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "not-the-default-password")
	t.Setenv("FEISHU_ENABLED", "true")
	t.Setenv("FEISHU_WEBHOOK", "https://example.invalid/feishu")
	t.Setenv("FEISHU_SECRET", "secret-value")
	t.Setenv("TELEGRAM_ENABLED", "true")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("TELEGRAM_CHAT_ID", "chat-id")
	t.Setenv("WEBHOOK_ENABLED", "true")
	t.Setenv("WEBHOOK_URL", "https://example.invalid/webhook")
	t.Setenv("WEBHOOK_SECRET", "webhook-secret")

	cfg, err := Load("/tmp/shortlink-config-test-does-not-exist.yaml")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if !cfg.Notification.Feishu.Enabled || cfg.Notification.Feishu.Webhook != "https://example.invalid/feishu" || cfg.Notification.Feishu.Secret != "secret-value" {
		t.Fatalf("feishu notification was not loaded from env: %+v", cfg.Notification.Feishu)
	}
	if !cfg.Notification.Telegram.Enabled || cfg.Notification.Telegram.BotToken != "bot-token" || cfg.Notification.Telegram.ChatID != "chat-id" {
		t.Fatalf("telegram notification was not loaded from env: %+v", cfg.Notification.Telegram)
	}
	if !cfg.Notification.Webhook.Enabled || cfg.Notification.Webhook.URL != "https://example.invalid/webhook" || cfg.Notification.Webhook.Secret != "webhook-secret" {
		t.Fatalf("webhook notification was not loaded from env: %+v", cfg.Notification.Webhook)
	}
}

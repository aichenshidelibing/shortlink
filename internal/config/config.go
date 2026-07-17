package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server       ServerConfig       `mapstructure:"server"`
	Database     DatabaseConfig     `mapstructure:"database"`
	Redis        RedisConfig        `mapstructure:"redis"`
	Admin        AdminConfig        `mapstructure:"admin"`
	Encryption   EncryptionConfig   `mapstructure:"encryption"`
	Shortlink    ShortlinkConfig    `mapstructure:"shortlink"`
	RateLimit    RateLimitConfig    `mapstructure:"rate_limit"`
	Cloudflare   CloudflareConfig   `mapstructure:"cloudflare"`
	Captcha      CaptchaConfig      `mapstructure:"captcha"`
	Notification NotificationConfig `mapstructure:"notification"`
	Features     FeaturesConfig     `mapstructure:"features"`
	Background   BackgroundConfig   `mapstructure:"background"`
}

type ServerConfig struct {
	Port         int    `mapstructure:"port"`
	Mode         string `mapstructure:"mode"`
	TrustedProxy bool   `mapstructure:"trusted_proxy"`
}

type DatabaseConfig struct {
	Driver   string `mapstructure:"driver"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	Charset  string `mapstructure:"charset"`
	MaxOpen  int    `mapstructure:"max_open"`
	MaxIdle  int    `mapstructure:"max_idle"`
}

func (d DatabaseConfig) DSN() string {
	// loc=UTC keeps the mysql driver's time.Time <-> datetime conversions
	// aligned with the MySQL container's server-wide UTC. If we used
	// loc=Local, the driver would encode Go times as local-clock string
	// literals; MySQL then stores those naive datetimes as UTC, so any
	// "ban_until > NOW()" comparison silently drifts by the offset.
	// parseTime=True + loc=UTC + time_zone='+00:00' pins both sides.
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=UTC&time_zone=%%27%%2B00%%3A00%%27",
		d.User, d.Password, d.Host, d.Port, d.DBName, d.Charset)
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type AdminConfig struct {
	Username      string `mapstructure:"username"`
	Password      string `mapstructure:"password"`
	SessionSecret string `mapstructure:"session_secret"`
}

type EncryptionConfig struct {
	Key string `mapstructure:"key"`
}

type ShortlinkConfig struct {
	DefaultLength   int    `mapstructure:"default_length"`
	MaxCustomLength int    `mapstructure:"max_custom_length"`
	MinCustomLength int    `mapstructure:"min_custom_length"`
	HashidsSalt     string `mapstructure:"hashids_salt"`
	SnowflakeWorker int64  `mapstructure:"snowflake_worker"`
	SnowflakeDC     int64  `mapstructure:"snowflake_datacenter"`
}

type RateLimitConfig struct {
	CreatePerMinute   int `mapstructure:"create_per_minute"`
	RedirectPerMinute int `mapstructure:"redirect_per_minute"`
}

type CloudflareConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	SiteKey   string `mapstructure:"site_key"`
	SecretKey string `mapstructure:"secret_key"`
}

type CaptchaConfig struct {
	Enabled            bool              `mapstructure:"enabled"`
	Provider           string            `mapstructure:"provider"`
	Mode               string            `mapstructure:"mode"`
	NormalProvider     string            `mapstructure:"normal_provider"`
	EscalationProvider string            `mapstructure:"escalation_provider"`
	FailureThreshold   int               `mapstructure:"failure_threshold"`
	RiskWindowSeconds  int               `mapstructure:"risk_window_seconds"`
	Cap                CapConfig         `mapstructure:"cap"`
	PlayCaptcha        PlayCaptchaConfig `mapstructure:"playcaptcha"`
}

type CapConfig struct {
	SiteKey     string `mapstructure:"site_key"`
	SecretKey   string `mapstructure:"secret_key"`
	VerifyURL   string `mapstructure:"verify_url"`
	APIEndpoint string `mapstructure:"api_endpoint"`
}

type PlayCaptchaConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	SiteKey   string `mapstructure:"site_key"`
	SecretKey string `mapstructure:"secret_key"`
	Endpoint  string `mapstructure:"endpoint"`
}

type NotificationConfig struct {
	Feishu   FeishuConfig   `mapstructure:"feishu"`
	Telegram TelegramConfig `mapstructure:"telegram"`
	Dingtalk DingtalkConfig `mapstructure:"dingtalk"`
	WeCom    WeComConfig    `mapstructure:"wecom"`
	Bark     BarkConfig     `mapstructure:"bark"`
	Discord  DiscordConfig  `mapstructure:"discord"`
	Email    EmailConfig    `mapstructure:"email"`
	Webhook  WebhookConfig  `mapstructure:"webhook"`
}

type FeishuConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Webhook string `mapstructure:"webhook"`
	Secret  string `mapstructure:"secret"`
}

type TelegramConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"`
	ChatID   string `mapstructure:"chat_id"`
}

type DingtalkConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Webhook string `mapstructure:"webhook"`
	Secret  string `mapstructure:"secret"`
}

type WeComConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Webhook string `mapstructure:"webhook"`
}

type BarkConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Key      string `mapstructure:"key"`
	Endpoint string `mapstructure:"endpoint"`
}

type DiscordConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Webhook string `mapstructure:"webhook"`
}

type EmailConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	Port    string `mapstructure:"port"`
	User    string `mapstructure:"user"`
	Pass    string `mapstructure:"pass"`
	From    string `mapstructure:"from"`
	To      string `mapstructure:"to"`
}

type WebhookConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
	Secret  string `mapstructure:"secret"`
}

type FeaturesConfig struct {
	AllowCustomCode     bool `mapstructure:"allow_custom_code"`
	RequirePrivacyAgree bool `mapstructure:"require_privacy_agree"`
}

type BackgroundConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
	Type    string `mapstructure:"type"`
}

func (c *Config) ValidateSecurity() error {
	password := strings.TrimSpace(c.Admin.Password)
	if password == "" {
		return nil
	}
	knownDefaults := map[string]bool{
		"admin":     true,
		"admin123":  true,
		"password":  true,
		"123456":    true,
		"changeme":  true,
		"shortlink": true,
	}
	if knownDefaults[strings.ToLower(password)] {
		return fmt.Errorf("admin password uses a known default value")
	}
	return nil
}

func setDefaults() {
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.mode", "release")
	viper.SetDefault("server.trusted_proxy", true)
	viper.SetDefault("database.driver", "mysql")
	viper.SetDefault("database.host", "mysql")
	viper.SetDefault("database.port", 3306)
	viper.SetDefault("database.user", "shortlink")
	viper.SetDefault("database.dbname", "shortlink")
	viper.SetDefault("database.charset", "utf8mb4")
	viper.SetDefault("database.max_open", 50)
	viper.SetDefault("database.max_idle", 10)
	viper.SetDefault("redis.addr", "redis:6379")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("admin.username", "admin")
	viper.SetDefault("shortlink.default_length", 6)
	viper.SetDefault("shortlink.max_custom_length", 32)
	viper.SetDefault("shortlink.min_custom_length", 4)
	viper.SetDefault("shortlink.snowflake_worker", 1)
	viper.SetDefault("shortlink.snowflake_datacenter", 1)
	viper.SetDefault("rate_limit.create_per_minute", 10)
	viper.SetDefault("rate_limit.redirect_per_minute", 60)
	viper.SetDefault("captcha.provider", "cap")
	viper.SetDefault("captcha.mode", "adaptive")
	viper.SetDefault("captcha.normal_provider", "cap")
	viper.SetDefault("captcha.escalation_provider", "turnstile")
	viper.SetDefault("captcha.failure_threshold", 3)
	viper.SetDefault("captcha.risk_window_seconds", 600)
	viper.SetDefault("features.allow_custom_code", true)
	viper.SetDefault("features.require_privacy_agree", true)
	viper.SetDefault("background.type", "image")
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetEnvPrefix("")
	viper.AutomaticEnv()
	setDefaults()

	_ = viper.BindEnv("server.port", "SERVER_PORT")
	_ = viper.BindEnv("server.mode", "SERVER_MODE")
	_ = viper.BindEnv("database.host", "DB_HOST")
	_ = viper.BindEnv("database.port", "DB_PORT")
	_ = viper.BindEnv("database.user", "DB_USER")
	_ = viper.BindEnv("database.password", "DB_PASSWORD")
	_ = viper.BindEnv("database.dbname", "DB_NAME")
	_ = viper.BindEnv("redis.addr", "REDIS_ADDR")
	_ = viper.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = viper.BindEnv("admin.username", "ADMIN_USERNAME")
	_ = viper.BindEnv("admin.password", "ADMIN_PASSWORD")
	_ = viper.BindEnv("admin.session_secret", "ADMIN_SESSION_SECRET")
	_ = viper.BindEnv("encryption.key", "ENCRYPTION_KEY")
	_ = viper.BindEnv("shortlink.hashids_salt", "HASHIDS_SALT")
	_ = viper.BindEnv("cloudflare.enabled", "CF_ENABLED")
	_ = viper.BindEnv("cloudflare.site_key", "CF_SITE_KEY")
	_ = viper.BindEnv("cloudflare.secret_key", "CF_SECRET_KEY")
	_ = viper.BindEnv("captcha.enabled", "CAPTCHA_ENABLED")
	_ = viper.BindEnv("captcha.provider", "CAPTCHA_PROVIDER")
	_ = viper.BindEnv("captcha.mode", "CAPTCHA_MODE")
	_ = viper.BindEnv("captcha.normal_provider", "CAPTCHA_NORMAL_PROVIDER")
	_ = viper.BindEnv("captcha.escalation_provider", "CAPTCHA_ESCALATION_PROVIDER")
	_ = viper.BindEnv("captcha.failure_threshold", "CAPTCHA_FAILURE_THRESHOLD")
	_ = viper.BindEnv("captcha.risk_window_seconds", "CAPTCHA_RISK_WINDOW_SECONDS")
	_ = viper.BindEnv("captcha.cap.site_key", "CAP_SITE_KEY")
	_ = viper.BindEnv("captcha.cap.secret_key", "CAP_SECRET_KEY")
	_ = viper.BindEnv("captcha.cap.verify_url", "CAP_VERIFY_URL")
	_ = viper.BindEnv("captcha.cap.api_endpoint", "CAP_API_ENDPOINT")
	_ = viper.BindEnv("captcha.playcaptcha.enabled", "PLAYCAPTCHA_ENABLED")
	_ = viper.BindEnv("captcha.playcaptcha.site_key", "PLAYCAPTCHA_SITE_KEY")
	_ = viper.BindEnv("captcha.playcaptcha.secret_key", "PLAYCAPTCHA_SECRET_KEY")
	_ = viper.BindEnv("captcha.playcaptcha.endpoint", "PLAYCAPTCHA_ENDPOINT")
	_ = viper.BindEnv("notification.feishu.enabled", "FEISHU_ENABLED")
	_ = viper.BindEnv("notification.feishu.webhook", "FEISHU_WEBHOOK")
	_ = viper.BindEnv("notification.feishu.secret", "FEISHU_SECRET")
	_ = viper.BindEnv("notification.telegram.enabled", "TELEGRAM_ENABLED")
	_ = viper.BindEnv("notification.telegram.bot_token", "TELEGRAM_BOT_TOKEN")
	_ = viper.BindEnv("notification.telegram.chat_id", "TELEGRAM_CHAT_ID")
	_ = viper.BindEnv("notification.dingtalk.enabled", "DINGTALK_ENABLED")
	_ = viper.BindEnv("notification.dingtalk.webhook", "DINGTALK_WEBHOOK")
	_ = viper.BindEnv("notification.dingtalk.secret", "DINGTALK_SECRET")
	_ = viper.BindEnv("notification.wecom.enabled", "WECOM_ENABLED")
	_ = viper.BindEnv("notification.wecom.webhook", "WECOM_WEBHOOK")
	_ = viper.BindEnv("notification.bark.enabled", "BARK_ENABLED")
	_ = viper.BindEnv("notification.bark.key", "BARK_KEY")
	_ = viper.BindEnv("notification.bark.endpoint", "BARK_ENDPOINT")
	_ = viper.BindEnv("notification.discord.enabled", "DISCORD_ENABLED")
	_ = viper.BindEnv("notification.discord.webhook", "DISCORD_WEBHOOK")
	_ = viper.BindEnv("notification.email.enabled", "EMAIL_ENABLED")
	_ = viper.BindEnv("notification.email.host", "EMAIL_HOST")
	_ = viper.BindEnv("notification.email.port", "EMAIL_PORT")
	_ = viper.BindEnv("notification.email.user", "EMAIL_USER")
	_ = viper.BindEnv("notification.email.pass", "EMAIL_PASS")
	_ = viper.BindEnv("notification.email.from", "EMAIL_FROM")
	_ = viper.BindEnv("notification.email.to", "EMAIL_TO")
	_ = viper.BindEnv("notification.webhook.enabled", "WEBHOOK_ENABLED")
	_ = viper.BindEnv("notification.webhook.url", "WEBHOOK_URL")
	_ = viper.BindEnv("notification.webhook.secret", "WEBHOOK_SECRET")
	_ = viper.BindEnv("shortlink.snowflake_worker", "SNOWFLAKE_WORKER_ID")
	_ = viper.BindEnv("shortlink.snowflake_datacenter", "SNOWFLAKE_DATACENTER_ID")

	if err := viper.ReadInConfig(); err != nil {
		// config file is optional when env vars are set. With SetConfigFile,
		// viper returns an underlying filesystem error for a missing explicit path.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.ValidateSecurity(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

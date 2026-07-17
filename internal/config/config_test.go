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

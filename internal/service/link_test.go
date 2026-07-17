package service

import (
	"context"
	"strings"
	"testing"

	"shortlink/internal/config"
)

type codeExistsRepo struct{}

func TestSafeShortCodeCharacters(t *testing.T) {
	valid := []string{"safe-code-123", "safe_code_123", "AbC123"}
	for _, code := range valid {
		if !isSafeShortCode(code) {
			t.Fatalf("expected %q to be safe", code)
		}
	}

	invalid := []string{"a/b", "a.b", "abc%2F", " abc", "abc ", "abc def", "中文码", "abc?x", "abc#x", ""}
	for _, code := range invalid {
		if isSafeShortCode(code) {
			t.Fatalf("expected %q to be unsafe", code)
		}
	}
}

func TestReservedShortCodes(t *testing.T) {
	reserved := []string{"api", "API", "manage", "help", "__admin", "index.html", "dashboard", "favicon.ico", "robots.txt", "assets", "static"}
	for _, code := range reserved {
		if !isReservedShortCode(code) {
			t.Fatalf("expected %q to be reserved", code)
		}
	}
	if isReservedShortCode("safe-code-123") {
		t.Fatal("safe-code-123 should not be reserved")
	}
}

func TestValidateCustomCodeFormatBeforeRepository(t *testing.T) {
	svc := &LinkService{cfg: &config.ShortlinkConfig{MinCustomLength: 4, MaxCustomLength: 32}}

	cases := []string{"a/bc", "a.bc", "abc%2F", "中文码", "abc def", "api", "manage", "help", "__admin", "index.html"}
	for _, code := range cases {
		err := svc.validateCustomCode(context.Background(), code)
		if err == nil {
			t.Fatalf("expected %q to be rejected", code)
		}
	}
}

func TestValidateCustomCodeLength(t *testing.T) {
	svc := &LinkService{cfg: &config.ShortlinkConfig{MinCustomLength: 4, MaxCustomLength: 8}}

	if err := svc.validateCustomCode(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("expected short length error, got %v", err)
	}
	if err := svc.validateCustomCode(context.Background(), "abcdefghi"); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("expected long length error, got %v", err)
	}
}

package service

import (
	"strings"
	"testing"

	"shortlink/internal/model"
)

func TestMatchDomainList(t *testing.T) {
	list := "# comment\nexample.com, *.trusted.test\n.blocked.test"

	cases := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"trusted.test", true},
		{"a.trusted.test", true},
		{"blocked.test", true},
		{"x.blocked.test", true},
		{"other.test", false},
	}
	for _, tc := range cases {
		if got := MatchDomainList(tc.host, list); got != tc.want {
			t.Fatalf("MatchDomainList(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestAPIKeyCheckDomainAllowed(t *testing.T) {
	svc := &APIKeyService{}
	key := &model.APIKey{
		AllowedDomains: "example.com, *.trusted.test",
		DeniedDomains:  "bad.example.com",
	}

	allowed := []string{"example.com", "sub.example.com", "trusted.test", "a.trusted.test"}
	for _, host := range allowed {
		if err := svc.CheckDomainAllowed(key, host); err != nil {
			t.Fatalf("expected %s to be allowed: %v", host, err)
		}
	}

	denied := []string{"bad.example.com", "other.test"}
	for _, host := range denied {
		if err := svc.CheckDomainAllowed(key, host); err == nil {
			t.Fatalf("expected %s to be denied", host)
		}
	}
}

func TestAPIKeyDeniedWins(t *testing.T) {
	svc := &APIKeyService{}
	key := &model.APIKey{AllowedDomains: "example.com", DeniedDomains: "bad.example.com"}

	err := svc.CheckDomainAllowed(key, "bad.example.com")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected denied domain error, got %v", err)
	}
}

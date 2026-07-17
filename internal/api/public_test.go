package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNormalizeTTLPolicyDisallowsNever(t *testing.T) {
	ds := &dynamicSettings{
		SettingsLoaded:   true,
		TTLAllowNeverSet: true,
		TTLAllowNever:    false,
		TTLMax:           86400,
		TTLOptions:       []int{0, 3600, 3600, -1, 172800, 86400},
	}
	policy := normalizeTTLPolicy(ds)
	if policy.AllowNever {
		t.Fatal("expected never to be disabled")
	}
	if policy.Default != 3600 {
		t.Fatalf("expected default to use smallest positive option, got %d", policy.Default)
	}
	want := []int{3600, 86400}
	if len(policy.Options) != len(want) {
		t.Fatalf("options=%v want %v", policy.Options, want)
	}
	for i := range want {
		if policy.Options[i] != want[i] {
			t.Fatalf("options=%v want %v", policy.Options, want)
		}
	}
}

func TestParseExpiryWithPolicyAppliesDefaultWhenNeverDisabled(t *testing.T) {
	h := &PublicHandler{}
	ds := &dynamicSettings{SettingsLoaded: true, TTLAllowNeverSet: true, TTLAllowNever: false, TTLDefault: 3600, TTLMax: 86400}
	exp, set, err := h.parseExpiryWithPolicy(ds, "", 0, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !set || exp == nil {
		t.Fatalf("expected default expiration to be set")
	}
	if time.Until(*exp) < 3500*time.Second || time.Until(*exp) > 3700*time.Second {
		t.Fatalf("expiration not near default ttl: %v", time.Until(*exp))
	}
}

func TestParseExpiryWithPolicyRejectsNeverWhenDisabled(t *testing.T) {
	h := &PublicHandler{}
	ds := &dynamicSettings{SettingsLoaded: true, TTLAllowNeverSet: true, TTLAllowNever: false, TTLDefault: 3600, TTLMax: 86400}
	_, _, err := h.parseExpiryWithPolicy(ds, "never", 0, true)
	if err == nil || err.message != "permanent links are not allowed" {
		t.Fatalf("expected permanent link rejection, got %v", err)
	}
}

func TestResolveQRPolicy(t *testing.T) {
	h := &PublicHandler{}
	ds := &dynamicSettings{SettingsLoaded: true, QRAllowUser: false, QRDefaultText: "默认文字", QRDefaultTpl: "card"}
	if _, _, err := h.resolveQRPolicy(ds, "用户文字", "", false); err == nil {
		t.Fatal("expected qr customization to be rejected")
	}
	text, tpl, err := h.resolveQRPolicy(ds, "", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "默认文字" || tpl != "card" {
		t.Fatalf("resolved qr=(%q,%q), want defaults", text, tpl)
	}
}

func TestQRShowDirectExplicitFalse(t *testing.T) {
	ds := &dynamicSettings{SettingsLoaded: true, QRShowDirectSet: true, QRShowDirect: false}
	if qrShowDirectEnabled(ds) {
		t.Fatal("explicit qr_show_direct=false should be preserved")
	}
	if !qrShowDirectEnabled(&dynamicSettings{}) {
		t.Fatal("missing qr_show_direct should default to true")
	}
}

func TestPublicRequestBaseURLTrustedForwardedHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/links", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "s1.example.test, proxy.local")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := publicRequestBaseURL(c, true); got != "https://s1.example.test" {
		t.Fatalf("trusted forwarded base URL = %q", got)
	}
	if got := publicRequestBaseURL(c, false); got != "http://127.0.0.1:8080" {
		t.Fatalf("untrusted forwarded base URL = %q", got)
	}
}

func TestPublicRequestBaseURLSanitizesForwardedHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/links", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.test/path")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := publicRequestBaseURL(c, true); got != "https://127.0.0.1" {
		t.Fatalf("sanitized forwarded base URL = %q", got)
	}
}

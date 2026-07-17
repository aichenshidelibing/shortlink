package api

import (
	"os"
	"strings"
	"testing"
)

func readStatic(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestStaticHTMLSourceAndDistStaySynced(t *testing.T) {
	pairs := [][2]string{
		{"../../web/public/index.html", "../../web/public/dist/index.html"},
		{"../../web/admin/index.html", "../../web/admin/dist/index.html"},
	}
	for _, pair := range pairs {
		src := readStatic(t, pair[0])
		dist := readStatic(t, pair[1])
		if src != dist {
			t.Fatalf("%s and %s are not synchronized", pair[0], pair[1])
		}
	}
}

func TestPublicHTMLUsesDynamicTTLQRAndAllActionLabels(t *testing.T) {
	html := readStatic(t, "../../web/public/index.html")
	checks := []string{
		"manageCopy",
		"qrAllowUser",
		"qrShowDirect",
		"renderTTL()",
		"ttl_options",
		"qr_show_direct",
		"qr_allow_user_customize",
		"captcha_enabled",
		"cap_token",
		"captcha_escalation_required",
	}
	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Fatalf("public html missing %q", check)
		}
	}
	if strings.Contains(html, "acts[1].textContent=t('report')") {
		t.Fatal("public action labels still use old shifted indexes")
	}
}

func TestAdminHTMLDoesNotUseSelectClassOnTOTPInput(t *testing.T) {
	html := readStatic(t, "../../web/admin/index.html")
	if strings.Contains(html, "v-model=\"totpCode\" maxlength=\"6\" inputmode=\"numeric\" :placeholder=\"tt('totpInput')\"") && strings.Contains(html, "class=\"tool-select\" style=\"flex:1;border-radius:10px\"") {
		t.Fatal("TOTP input still uses select styling")
	}
	if !strings.Contains(html, "class=\"tool-input\"") {
		t.Fatal("admin html missing tool-input class for non-select controls")
	}
	if !strings.Contains(html, "ttl_options_selected") || !strings.Contains(html, "ttlSummary") {
		t.Fatal("admin html missing friendly TTL controls")
	}
	for _, check := range []string{"captcha_enabled", "cap_verify_url", "playcaptcha_enabled"} {
		if !strings.Contains(html, check) {
			t.Fatalf("admin html missing %q", check)
		}
	}
}

package service

import "testing"

func TestNormalizeDestinationURLRejectsLocalHostnames(t *testing.T) {
	cases := []string{
		"localhost:8080/path",
		"http://localhost/path",
		"http://app.local/path",
		"http://service.internal/path",
		"http://router.lan/path",
	}
	for _, raw := range cases {
		if _, err := NormalizeDestinationURL(raw, false); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestNormalizeDestinationURLRejectsNonStandardIPLiteral(t *testing.T) {
	cases := []string{
		"http://2130706433/",
		"http://0177.0.0.1/",
		"http://0x7f.0.0.1/",
	}
	for _, raw := range cases {
		if _, err := NormalizeDestinationURL(raw, false); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestNormalizeDestinationURLAllowsPublicHost(t *testing.T) {
	normalized, err := NormalizeDestinationURL("example.com/path", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.URL != "https://example.com/path" || normalized.Host != "example.com" {
		t.Fatalf("normalized=%+v", normalized)
	}
}

package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestProvisioningURIRoundTrip(t *testing.T) {
	svc := NewTOTP()

	secret, _, err := svc.GenerateSecret("admin")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}

	uri := svc.ProvisioningURI("admin", secret)
	if uri == "" {
		t.Fatal("expected provisioning uri")
	}

	key, err := otp.NewKeyFromURL(uri)
	if err != nil {
		t.Fatalf("parse provisioning uri: %v", err)
	}
	if got := key.Secret(); got != secret {
		t.Fatalf("uri secret mismatch: got %q want %q", got, secret)
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if !svc.Validate(code, secret) {
		t.Fatal("expected generated code to validate")
	}
}

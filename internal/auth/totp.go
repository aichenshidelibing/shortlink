package auth

import (
	"bytes"
	"encoding/base32"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"
)

type TOTP struct{}

func NewTOTP() *TOTP {
	return &TOTP{}
}

func (t *TOTP) GenerateSecret(account string) (string, []byte, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Shortlink",
		AccountName: account,
	})
	if err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	img, err := key.Image(200, 200)
	if err != nil {
		return key.Secret(), nil, err
	}
	if err := png.Encode(&buf, img); err != nil {
		return key.Secret(), nil, err
	}

	return key.Secret(), buf.Bytes(), nil
}

func (t *TOTP) Validate(passcode, secret string) bool {
	// Try with skew=1 first, then skew=2 for better clock tolerance
	if totp.Validate(passcode, secret) {
		return true
	}
	// Some authenticators or servers have clock drift — allow ±2 periods (60s)
	ok, err := totp.ValidateCustom(passcode, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      2,
		Digits:    6,
		Algorithm: 0,
	})
	return err == nil && ok
}

func (t *TOTP) ProvisioningURI(account, secret string) string {
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Shortlink",
		AccountName: account,
		Secret:      secretBytes,
	})
	if err != nil || key == nil {
		return ""
	}
	return key.URL()
}

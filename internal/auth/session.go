package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "sl_session"

type SessionManager struct {
	secret []byte
}

func NewSessionManager(secret string) *SessionManager {
	return &SessionManager{secret: []byte(secret)}
}

func (s *SessionManager) Create(w http.ResponseWriter, r *http.Request, adminID int) error {
	expires := time.Now().Add(2 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%d", adminID, expires)
	sig := s.sign(payload)
	token := base64.URLEncoding.EncodeToString([]byte(payload)) + "." + base64.URLEncoding.EncodeToString(sig)

	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   7200,
	})
	return nil
}

func (s *SessionManager) Validate(r *http.Request) (int, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return 0, err
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid session")
	}

	payloadBytes, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, err
	}

	expectedSig, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}

	if !hmac.Equal(s.sign(string(payloadBytes)), expectedSig) {
		return 0, fmt.Errorf("invalid signature")
	}

	payload := string(payloadBytes)
	fields := strings.Split(payload, ":")
	if len(fields) != 2 {
		return 0, fmt.Errorf("invalid payload")
	}

	adminID, _ := strconv.Atoi(fields[0])
	expires, _ := strconv.ParseInt(fields[1], 10, 64)

	if time.Now().Unix() > expires {
		return 0, fmt.Errorf("session expired")
	}

	return adminID, nil
}

// Clear expires the session cookie. We intentionally emit two Set-Cookie
// headers — one with Secure=true, one with Secure=false — because Create()
// may have set either variant depending on the request's TLS state, and
// browsers treat those as distinct cookies. Emitting both guarantees the
// browser drops whichever one it currently holds.
func (s *SessionManager) Clear(w http.ResponseWriter) {
	for _, secure := range []bool{true, false} {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

func (s *SessionManager) sign(payload string) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(payload))
	return h.Sum(nil)
}

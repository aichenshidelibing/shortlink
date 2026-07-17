package crypto

import (
	"encoding/base64"
	"strings"
)

// CryptoManager provides versioned encryption with legacy fallback.
// v1: weak XOR obfuscation (legacy)
// v2: AES-256-GCM (current, always used for new encrypts)
type CryptoManager struct {
	strong *StrongCrypto
	weak   *WeakCrypto
}

func NewCryptoManager(strong *StrongCrypto, weak *WeakCrypto) *CryptoManager {
	return &CryptoManager{strong: strong, weak: weak}
}

const strongPrefix = "v2:"

// Encrypt encrypts plaintext using strong AES-256-GCM.
// Returns versioned string: "v2:<nonce_b64>:<ciphertext_b64>"
func (m *CryptoManager) Encrypt(plain string) string {
	ct, nonce, err := m.strong.EncryptString(plain)
	if err != nil {
		// Fallback to weak on error (should never happen)
		return m.weak.Encrypt(plain)
	}
	return strongPrefix + nonce + ":" + ct
}

// Decrypt auto-detects encryption version and decrypts.
// v2 (strong) if starts with "v2:", otherwise tries v1 (weak/legacy).
func (m *CryptoManager) Decrypt(cipher string) (string, error) {
	if cipher == "" {
		return "", nil
	}

	// v2: strong AES-256-GCM
	if strings.HasPrefix(cipher, strongPrefix) {
		payload := cipher[len(strongPrefix):]
		parts := strings.SplitN(payload, ":", 2)
		if len(parts) != 2 {
			return "", nil
		}
		return m.strong.DecryptString(parts[1], parts[0])
	}

	// v1: legacy weak XOR (no prefix, or "v1:" prefix)
	legacy := cipher
	if strings.HasPrefix(legacy, "v1:") {
		legacy = legacy[3:]
	}
	return m.weak.Decrypt(legacy)
}

// TryDecrypt tries to decrypt, returns original string on failure.
func (m *CryptoManager) TryDecrypt(cipher string) string {
	plain, err := m.Decrypt(cipher)
	if err != nil || plain == "" {
		// If base64 decode fails, it might be plaintext already
		// Try weak directly
		if p, e := m.weak.Decrypt(cipher); e == nil {
			return p
		}
		return cipher
	}
	return plain
}

// EncryptBytes encrypts raw bytes with strong AES-256-GCM.
// Used for link URLs. Returns (ciphertext, nonce).
// No version prefix needed — stored as binary in DB.
func (m *CryptoManager) EncryptBytes(plain []byte) ([]byte, []byte, error) {
	return m.strong.Encrypt(plain)
}

// DecryptBytes decrypts raw bytes. No versioning needed — all link data
// uses strong AES-GCM (migrated from old weak at DB level if needed).
func (m *CryptoManager) DecryptBytes(ct, nonce []byte) ([]byte, error) {
	return m.strong.Decrypt(ct, nonce)
}

// HashBase64 is a helper to decode base64 (used for checking if data is encrypted).
func HashBase64(s string) bool {
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

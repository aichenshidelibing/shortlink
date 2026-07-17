package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func GenerateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "sl_" + hex.EncodeToString(b)
}

func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func VerifyAPIKey(key, hash string) bool {
	return HashAPIKey(key) == hash
}

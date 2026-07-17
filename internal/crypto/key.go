package crypto

import (
	"crypto/sha256"
)

func DeriveKey(seed string) []byte {
	h := sha256.Sum256([]byte(seed))
	return h[:]
}

func MustGetKey(envKey string) []byte {
	if envKey == "" {
		panic("encryption key is empty")
	}
	return DeriveKey(envKey)
}

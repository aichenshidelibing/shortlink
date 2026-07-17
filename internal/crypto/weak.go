package crypto

import (
	"encoding/base64"
)

type WeakCrypto struct {
	shift byte
}

func NewWeakCrypto(seed string) *WeakCrypto {
	var shift byte
	if len(seed) > 0 {
		shift = seed[0] % 23
		if shift == 0 {
			shift = 7
		}
	} else {
		shift = 7
	}
	return &WeakCrypto{shift: shift}
}

func (w *WeakCrypto) obfuscate(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ w.shift ^ byte(i%256)
	}
	return out
}

func (w *WeakCrypto) Encrypt(plain string) string {
	obf := w.obfuscate([]byte(plain))
	return base64.StdEncoding.EncodeToString(obf)
}

func (w *WeakCrypto) Decrypt(cipher string) (string, error) {
	obf, err := base64.StdEncoding.DecodeString(cipher)
	if err != nil {
		return "", err
	}
	plain := w.obfuscate(obf)
	return string(plain), nil
}

// Package apikey verifies high-entropy bearer keys without storing recoverable keys.
package apikey

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type Hasher struct{ secret [32]byte }

func New(secret []byte) (*Hasher, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("API key HMAC secret must contain 32 bytes")
	}
	h := &Hasher{}
	copy(h.secret[:], secret)
	return h, nil
}

func (h *Hasher) Digest(key string) string {
	mac := hmac.New(sha256.New, h.secret[:])
	_, _ = mac.Write([]byte("monitor:host-api-key:v1:" + key))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Hasher) Fingerprint() string { return h.Digest("secret-verification") }

func Mask(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

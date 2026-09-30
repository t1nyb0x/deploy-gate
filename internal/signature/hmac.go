package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const prefix = "sha256="

func Validate(body []byte, sigHeader, secret string) bool {
	if secret == "" {
		return false
	}

	if !strings.HasPrefix(sigHeader, prefix) {
		return false
	}

	got, err := hex.DecodeString(strings.TrimPrefix(sigHeader, prefix))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)

	expected := mac.Sum(nil)

	return hmac.Equal(got, expected)
}

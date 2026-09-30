package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return prefix + hex.EncodeToString(mac.Sum(nil))
}

func TestValidate(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)

	tests := []struct {
		name   string
		body   []byte
		sig    string
		secret string
		want   bool
	}{
		{"valid", body, sign(body, "s"), "s", true},
		{"wrong secret", body, sign(body, "other"), "s", false},
		{"tampered body", []byte(`{"ref":"refs/heads/evil"}`), sign(body, "s"), "s", false},
		{"empty secret", body, sign(body, ""), "", false},
		{"missing prefix", body, sign(body, "s")[len(prefix):], "s", false},
		{"sha1 prefix", body, "sha1=" + sign(body, "s")[len(prefix):], "s", false},
		{"non-hex", body, prefix + "zzzz", "s", false},
		{"empty header", body, "", "s", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Validate(tt.body, tt.sig, tt.secret); got != tt.want {
				t.Errorf("Validate() = %v, want %v", got, tt.want)
			}
		})
	}
}

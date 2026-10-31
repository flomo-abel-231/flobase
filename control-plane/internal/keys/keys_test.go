package keys

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestAPIKeyIsVerifiableJWT(t *testing.T) {
	secret := JWTSecret()
	token := APIKey(secret, "abcdefghijklmnop", "anon")

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("want 3 parts, got %d", len(parts))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if got := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); got != parts[2] {
		t.Fatal("signature does not verify")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["role"] != "anon" || claims["ref"] != "abcdefghijklmnop" {
		t.Fatalf("unexpected claims %v", claims)
	}
}

func TestRefAndPasswordShape(t *testing.T) {
	if r := Ref(); len(r) != 16 || strings.Trim(r, refAlphabet) != "" {
		t.Fatalf("bad ref %q", r)
	}
	if p := Password(); len(p) != 32 || strings.Trim(p, secretAlphabet) != "" {
		t.Fatalf("bad password %q", p)
	}
}

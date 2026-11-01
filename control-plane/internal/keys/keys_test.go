package keys

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
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

func TestSignVerify(t *testing.T) {
	tok := Sign("secret", map[string]any{"sub": "u1", "exp": time.Now().Add(time.Hour).Unix()})
	claims, err := Verify("secret", tok)
	if err != nil || claims["sub"] != "u1" {
		t.Fatalf("Verify = %v, %v", claims, err)
	}
	if _, err := Verify("other", tok); err == nil {
		t.Fatal("accepted a token signed with another secret")
	}
	expired := Sign("secret", map[string]any{"sub": "u1", "exp": time.Now().Add(-time.Second).Unix()})
	if _, err := Verify("secret", expired); err == nil {
		t.Fatal("accepted an expired token")
	}
	if _, err := Verify("secret", tok[:len(tok)-2]+"xx"); err == nil {
		t.Fatal("accepted a tampered token")
	}
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + strings.Split(tok, ".")[1] + "."
	if _, err := Verify("secret", none); err == nil {
		t.Fatal("accepted alg none")
	}
}

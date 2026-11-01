// Package keys makes project refs, passwords, JWT secrets and the two API
// keys (anon, service_role) every project gets.
package keys

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"
)

const refAlphabet = "abcdefghijklmnopqrstuvwxyz"
const secretAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func random(alphabet string, n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

// Ref is the project's public id, used in hostnames and container names.
func Ref() string { return random(refAlphabet, 16) }

// Password is URL-safe so it can sit in a connection string unescaped.
func Password() string { return random(secretAlphabet, 32) }

func JWTSecret() string { return random(secretAlphabet, 48) }

// APIKey signs a long-lived HS256 JWT for a role, the same shape as
// Supabase's anon and service_role keys.
func APIKey(secret, ref, role string) string {
	now := time.Now().Unix()
	return Sign(secret, map[string]any{
		"iss":  "flobase",
		"ref":  ref,
		"role": role,
		"iat":  now,
		"exp":  now + 10*365*24*3600,
	})
}

// Sign makes an HS256 JWT with the given claims.
func Sign(secret string, claims map[string]any) string {
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	return unsigned + "." + enc.EncodeToString(mac(secret, unsigned))
}

var ErrInvalidToken = errors.New("invalid token")

// Verify checks an HS256 JWT's signature and expiry and returns its claims.
// Only HS256 is accepted, so a token cannot pick a weaker algorithm.
func Verify(secret, token string) (map[string]any, error) {
	enc := base64.RawURLEncoding
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac(secret, parts[0]+"."+parts[1])) {
		return nil, ErrInvalidToken
	}
	var header struct{ Alg string }
	if raw, err := enc.DecodeString(parts[0]); err != nil || json.Unmarshal(raw, &header) != nil || header.Alg != "HS256" {
		return nil, ErrInvalidToken
	}
	var claims map[string]any
	raw, err := enc.DecodeString(parts[1])
	if err != nil || json.Unmarshal(raw, &claims) != nil {
		return nil, ErrInvalidToken
	}
	if exp, ok := claims["exp"].(float64); !ok || time.Now().Unix() >= int64(exp) {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func mac(secret, data string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(data))
	return m.Sum(nil)
}

// Token is a random opaque token, for refresh tokens and invite links.
func Token() string { return random(secretAlphabet, 43) }

// Hash is how opaque tokens are stored: a leaked table gives no usable token.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

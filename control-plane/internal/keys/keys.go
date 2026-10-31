// Package keys makes project refs, passwords, JWT secrets and the two API
// keys (anon, service_role) every project gets.
package keys

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
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
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	now := time.Now().Unix()
	payload, _ := json.Marshal(map[string]any{
		"iss":  "flobase",
		"ref":  ref,
		"role": role,
		"iat":  now,
		"exp":  now + 10*365*24*3600,
	})
	unsigned := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + enc.EncodeToString(mac.Sum(nil))
}

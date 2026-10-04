package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

// MinTokenLen is the minimum access-token length (base64url characters; 43 ≈ 256 bits).
const MinTokenLen = 32

// Auth checks the access token. Only the token's SHA-256 digest is kept in memory.
type Auth struct {
	digest [sha256.Size]byte
}

// NewAuth returns an Auth for token, rejecting short tokens.
func NewAuth(token string) (*Auth, error) {
	if len(token) < MinTokenLen {
		return nil, errors.New("access token must be at least 32 characters (use `gymrouter new-token`)")
	}
	return &Auth{digest: sha256.Sum256([]byte(token))}, nil
}

// NewToken returns a random 256-bit token, base64url-encoded.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Valid reports whether the request carries the token as "Authorization: Bearer <token>".
// Comparing fixed-size digests keeps the check constant-time regardless of input length.
func (a *Auth) Valid(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || tok == "" {
		return false
	}
	d := sha256.Sum256([]byte(tok))
	return subtle.ConstantTimeCompare(d[:], a.digest[:]) == 1
}

// Require wraps h so that requests without a valid token get Go's plain "404 page not found", the same as any path
// that doesn't exist (and as Traefik's own): to anyone without a setup link there is nothing here. The app tells it
// apart from its own 404s, which are JSON.
func (a *Auth) Require(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Valid(r) {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

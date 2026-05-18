package ledger

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AuthClaims struct {
	Sub  string `json:"sub"`
	Org  string `json:"org"`
	Role string `json:"role"`
	Jti  string `json:"jti"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

type contextKey string

const authClaimsKey contextKey = "auth"

func signJWT(claims AuthClaims, secret []byte) (string, error) {
	return signJWTWithTime(claims, secret, time.Now())
}

func signJWTWithTime(claims AuthClaims, secret []byte, now time.Time) (string, error) {
	claims.Iat = now.Unix()
	claims.Exp = now.Add(1 * time.Hour).Unix()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: generate jti: %w", err)
	}
	claims.Jti = hex.EncodeToString(b[:])

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

func verifyJWT(tokenString string, secret []byte) (*AuthClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("auth: malformed token")
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("auth: invalid signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("auth: malformed payload")
	}

	var claims AuthClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("auth: malformed claims")
	}

	now := time.Now().Unix()
	if claims.Exp != 0 && now > claims.Exp {
		return nil, fmt.Errorf("auth: token expired")
	}

	return &claims, nil
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func AuthFromContext(ctx context.Context) *AuthClaims {
	claims, _ := ctx.Value(authClaimsKey).(*AuthClaims)
	return claims
}

func authMiddleware(next http.Handler, secret []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(secret) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		token := extractBearer(r)
		if token == "" {
			http.Error(w, `{"error":"auth_required"}`, http.StatusUnauthorized)
			return
		}
		claims, err := verifyJWT(token, secret)
		if err != nil {
			http.Error(w, `{"error":"auth_required"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), authClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) resolveAuth(r *http.Request) (*AuthClaims, error) {
	if len(s.jwtSecret) == 0 {
		return &AuthClaims{Sub: "open", Org: "", Role: "owner"}, nil
	}
	token := extractBearer(r)
	if token == "" {
		return nil, fmt.Errorf("auth: missing token")
	}
	return verifyJWT(token, s.jwtSecret)
}

func roleAtLeast(have, min string) bool {
	rank := map[string]int{"viewer": 0, "member": 1, "admin": 2, "owner": 3}
	return rank[have] >= rank[min]
}

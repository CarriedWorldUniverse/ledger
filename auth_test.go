package ledger

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJWT_SignAndVerify(t *testing.T) {
	secret := []byte("test-secret")
	claims := AuthClaims{
		Sub:  "jacinta",
		Org:  "nexus",
		Role: "owner",
	}

	token, err := signJWT(claims, secret)
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	got, err := verifyJWT(token, secret)
	if err != nil {
		t.Fatalf("verifyJWT: %v", err)
	}
	if got.Sub != "jacinta" {
		t.Errorf("sub = %q, want jacinta", got.Sub)
	}
	if got.Org != "nexus" {
		t.Errorf("org = %q, want nexus", got.Org)
	}
	if got.Role != "owner" {
		t.Errorf("role = %q, want owner", got.Role)
	}
	if got.Iat == 0 || got.Exp == 0 {
		t.Error("expected iat and exp to be set")
	}
	if got.Exp-got.Iat != 3600 {
		t.Errorf("exp-iat = %d, want 3600 (1 hour)", got.Exp-got.Iat)
	}
}

func TestJWT_VerifyRejectsExpired(t *testing.T) {
	secret := []byte("test-secret")
	claims := AuthClaims{
		Sub:  "jacinta",
		Org:  "nexus",
		Role: "owner",
	}
	now := time.Now()
	claims.Iat = now.Add(-2 * time.Hour).Unix()
	claims.Exp = now.Add(-1 * time.Hour).Unix()

	token, err := signJWTWithTime(claims, secret, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("signJWTWithTime: %v", err)
	}

	_, err = verifyJWT(token, secret)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWT_VerifyRejectsWrongSecret(t *testing.T) {
	claims := AuthClaims{Sub: "jacinta", Org: "nexus", Role: "owner"}
	token, err := signJWT(claims, []byte("secret-a"))
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}

	_, err = verifyJWT(token, []byte("secret-b"))
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestJWT_VerifyRejectsMalformed(t *testing.T) {
	secret := []byte("test-secret")

	if _, err := verifyJWT("not.a.jwt", secret); err == nil {
		t.Fatal("expected error for malformed token")
	}
	if _, err := verifyJWT("", secret); err == nil {
		t.Fatal("expected error for empty token")
	}
	if _, err := verifyJWT("eyJ.eyJ.!!!", secret); err == nil {
		t.Fatal("expected error for bad base64")
	}
}

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		name      string
		auth      string
		wantEmpty bool
	}{
		{"valid", "Bearer abc123", false},
		{"no prefix", "abc123", true},
		{"empty header", "", true},
		{"basic auth", "Basic dXNlcjpwYXNz", true},
		{"extra spaces", "Bearer  abc123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			if tt.auth != "" {
				r.Header.Set("Authorization", tt.auth)
			}
			got := extractBearer(r)
			if tt.wantEmpty && got != "" {
				t.Errorf("expected empty token, got %q", got)
			}
			if !tt.wantEmpty && got == "" {
				t.Error("expected non-empty token")
			}
		})
	}
}

func TestAuthMiddleware_PopulatesContext(t *testing.T) {
	secret := []byte("test-secret")
	claims := AuthClaims{Sub: "jacinta", Org: "nexus", Role: "owner"}
	token, _ := signJWT(claims, secret)

	var captured *AuthClaims
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = AuthFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	wrapped := authMiddleware(next, secret)
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if captured == nil {
		t.Fatal("expected claims in context")
	}
	if captured.Sub != "jacinta" || captured.Role != "owner" {
		t.Errorf("claims = %+v", captured)
	}
}

func TestAuthMiddleware_Returns401OnBadToken(t *testing.T) {
	secret := []byte("test-secret")
	badToken, _ := signJWT(AuthClaims{Sub: "x", Org: "x", Role: "viewer"}, []byte("other-secret"))

	tests := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"malformed", "Bearer not.jwt.at.all"},
		{"wrong secret", "Bearer " + badToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			})
			wrapped := authMiddleware(next, secret)
			req, _ := http.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_SkipsWhenNoSecret(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := authMiddleware(next, nil)
	req, _ := http.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (open mode), got %d", rec.Code)
	}
}

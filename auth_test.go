package ledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestContextWithAuth_RoundTrips(t *testing.T) {
	want := &AuthClaims{Sub: "agent.anvil", Org: "carried-world", Role: "member"}
	ctx := ContextWithAuth(context.Background(), want)
	got := AuthFromContext(ctx)
	if got == nil || got.Sub != want.Sub || got.Org != want.Org {
		t.Fatalf("round-trip = %+v, want %+v", got, want)
	}
}

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

func TestRequireAdmin(t *testing.T) {
	secret := []byte("test-secret")

	tests := []struct {
		name       string
		role       string
		wantStatus int
	}{
		{"owner allowed", "owner", http.StatusOK},
		{"admin allowed", "admin", http.StatusOK},
		{"member denied", "member", http.StatusForbidden},
		{"viewer denied", "viewer", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{jwtSecret: secret}
			claims := AuthClaims{Sub: "test", Org: "nexus", Role: tt.role}
			token, _ := signJWT(claims, secret)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !svc.requireAdmin(w, r) {
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			wrapped := svc.authMiddleware(handler)

			req, _ := http.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestRequireAdmin_NoSecretOpenMode(t *testing.T) {
	svc := &Service{jwtSecret: nil}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !svc.requireAdmin(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	wrapped := svc.authMiddleware(handler)

	req, _ := http.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (open mode)", rec.Code)
	}
}

func TestRequireRole(t *testing.T) {
	secret := []byte("test-secret")

	tests := []struct {
		name      string
		tokenOrg  string
		tokenRole string
		checkOrg  string
		minRole   string
		wantOK    bool
	}{
		{"same org, owner meets admin", "nexus", "owner", "nexus", "admin", true},
		{"same org, admin meets member", "nexus", "admin", "nexus", "member", true},
		{"same org, member meets viewer", "nexus", "member", "nexus", "viewer", true},
		{"same org, viewer meets viewer", "nexus", "viewer", "nexus", "viewer", true},
		{"same org, member fails admin", "nexus", "member", "nexus", "admin", false},
		{"different org, owner fails", "nexus", "owner", "acme", "admin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{jwtSecret: secret}
			claims := AuthClaims{Sub: "test", Org: tt.tokenOrg, Role: tt.tokenRole}
			token, _ := signJWT(claims, secret)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !svc.requireRole(w, r, tt.checkOrg, tt.minRole) {
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			wrapped := svc.authMiddleware(handler)

			req, _ := http.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			if tt.wantOK && rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
			if !tt.wantOK && rec.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rec.Code)
			}
		})
	}
}

func TestAuthRefresh(t *testing.T) {
	secret := []byte("test-secret")
	claims := AuthClaims{Sub: "jacinta", Org: "nexus", Role: "owner"}
	token, _ := signJWT(claims, secret)
	origClaims, _ := verifyJWT(token, secret)

	svc := &Service{jwtSecret: secret}
	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Token == "" {
		t.Fatal("expected new token in response")
	}
	if body.Token == token {
		t.Error("expected fresh token, got same token")
	}

	newClaims, err := verifyJWT(body.Token, secret)
	if err != nil {
		t.Fatalf("verifyJWT of refreshed token: %v", err)
	}
	if newClaims.Sub != "jacinta" || newClaims.Org != "nexus" || newClaims.Role != "owner" {
		t.Errorf("refreshed claims = %+v", newClaims)
	}
	if newClaims.Exp < origClaims.Exp {
		t.Errorf("expected refreshed expiry %d >= original expiry %d", newClaims.Exp, origClaims.Exp)
	}
}

func TestAuthRefresh_RejectsExpiredToken(t *testing.T) {
	secret := []byte("test-secret")
	claims := AuthClaims{Sub: "jacinta", Org: "nexus", Role: "owner"}
	now := time.Now()
	claims.Iat = now.Add(-2 * time.Hour).Unix()
	claims.Exp = now.Add(-1 * time.Hour).Unix()
	token, _ := signJWTWithTime(claims, secret, now.Add(-2*time.Hour))

	svc := &Service{jwtSecret: secret}
	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired token; got %d", resp.StatusCode)
	}
}

func TestAuthRefresh_RejectsGET(t *testing.T) {
	secret := []byte("test-secret")

	svc := &Service{jwtSecret: secret}
	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/auth/refresh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

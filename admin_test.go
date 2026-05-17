package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestAdminService(t *testing.T) (*Service, string) {
	t.Helper()
	svc, err := New(context.Background(), Config{
		DBPath:     filepath.Join(t.TempDir(), "ledger.db"),
		AdminToken: "test-admin-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, "test-admin-token"
}

func adminReq(method, url, token string, body any) (*http.Request, error) {
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func doAdmin(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	return resp
}

func TestAdmin_OrgCRUD(t *testing.T) {
	svc, token := newTestAdminService(t)
	defer svc.Close()

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// POST /api/admin/orgs — create
	req, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/orgs", token, map[string]string{
		"slug": "acme", "name": "Acme Corp",
	})
	resp := doAdmin(t, req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var org Organisation
	json.NewDecoder(resp.Body).Decode(&org)
	resp.Body.Close()
	if org.Slug != "acme" || org.Name != "Acme Corp" {
		t.Fatalf("got %+v", org)
	}

	// GET /api/admin/orgs/acme — read
	req2, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs/acme", token, nil)
	resp2 := doAdmin(t, req2)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp2.StatusCode)
	}
	var got Organisation
	json.NewDecoder(resp2.Body).Decode(&got)
	if got.Name != "Acme Corp" {
		t.Errorf("name = %q", got.Name)
	}

	// GET /api/admin/orgs — list
	req3, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs", token, nil)
	resp3 := doAdmin(t, req3)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp3.StatusCode)
	}
	var list []Organisation
	json.NewDecoder(resp3.Body).Decode(&list)
	if len(list) < 1 {
		t.Error("expected at least one org in list")
	}

	// PUT /api/admin/orgs/acme — update
	req4, _ := adminReq(http.MethodPut, srv.URL+"/api/admin/orgs/acme", token, map[string]string{
		"name": "Acme Updated",
	})
	resp4 := doAdmin(t, req4)
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp4.StatusCode)
	}

	// DELETE /api/admin/orgs/acme — delete
	req5, _ := adminReq(http.MethodDelete, srv.URL+"/api/admin/orgs/acme", token, nil)
	resp5 := doAdmin(t, req5)
	defer resp5.Body.Close()
	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp5.StatusCode)
	}

	// GET /api/admin/orgs/acme — 404 after delete
	req6, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs/acme", token, nil)
	resp6 := doAdmin(t, req6)
	defer resp6.Body.Close()
	if resp6.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete; got %d", resp6.StatusCode)
	}
}

func TestAdmin_NonAdminRejected(t *testing.T) {
	svc, _ := newTestAdminService(t)
	defer svc.Close()

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Without token
	req, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs", "", nil)
	resp := doAdmin(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 without token; got %d", resp.StatusCode)
	}

	// With wrong token
	req2, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs", "wrong-token", nil)
	resp2 := doAdmin(t, req2)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 with wrong token; got %d", resp2.StatusCode)
	}
}

func TestAdmin_UserCRUD(t *testing.T) {
	svc, token := newTestAdminService(t)
	defer svc.Close()

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Create user
	req, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/users", token, map[string]string{
		"id": "alice", "kind": "human",
	})
	resp := doAdmin(t, req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Get user
	req2, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/users/alice", token, nil)
	resp2 := doAdmin(t, req2)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp2.StatusCode)
	}

	// Update user kind
	req3, _ := adminReq(http.MethodPut, srv.URL+"/api/admin/users/alice", token, map[string]string{
		"kind": "ai",
	})
	resp3 := doAdmin(t, req3)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp3.StatusCode)
	}

	// Verify update
	req4, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/users/alice", token, nil)
	resp4 := doAdmin(t, req4)
	defer resp4.Body.Close()
	var u User
	json.NewDecoder(resp4.Body).Decode(&u)
	if u.Kind != "ai" {
		t.Errorf("kind = %q after update", u.Kind)
	}

	// Delete user
	req5, _ := adminReq(http.MethodDelete, srv.URL+"/api/admin/users/alice", token, nil)
	resp5 := doAdmin(t, req5)
	defer resp5.Body.Close()
	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp5.StatusCode)
	}

	// 404 after delete
	req6, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/users/alice", token, nil)
	resp6 := doAdmin(t, req6)
	defer resp6.Body.Close()
	if resp6.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete; got %d", resp6.StatusCode)
	}
}

func TestAdmin_MemberLifecycle(t *testing.T) {
	svc, token := newTestAdminService(t)
	defer svc.Close()

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Create org and user
	req0, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/orgs", token, map[string]string{"slug": "acme", "name": "Acme"})
	resp0 := doAdmin(t, req0)
	resp0.Body.Close()
	req1, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/users", token, map[string]string{"id": "alice", "kind": "human"})
	resp1 := doAdmin(t, req1)
	resp1.Body.Close()

	// Add member
	req, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/orgs/acme/members", token, map[string]string{
		"user_id": "alice", "role": "admin",
	})
	resp := doAdmin(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status = %d", resp.StatusCode)
	}

	// List members
	req2, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs/acme/members", token, nil)
	resp2 := doAdmin(t, req2)
	defer resp2.Body.Close()
	var members []OrgMember
	json.NewDecoder(resp2.Body).Decode(&members)
	if len(members) != 1 || members[0].UserID != "alice" || members[0].Role != "admin" {
		t.Errorf("members = %+v", members)
	}

	// Change role
	req3, _ := adminReq(http.MethodPut, srv.URL+"/api/admin/orgs/acme/members/alice", token, map[string]string{
		"role": "member",
	})
	resp3 := doAdmin(t, req3)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("role change status = %d", resp3.StatusCode)
	}

	// Verify role
	req4, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs/acme/members", token, nil)
	resp4 := doAdmin(t, req4)
	defer resp4.Body.Close()
	json.NewDecoder(resp4.Body).Decode(&members)
	if members[0].Role != "member" {
		t.Errorf("role = %q after change", members[0].Role)
	}

	// Remove member
	req5, _ := adminReq(http.MethodDelete, srv.URL+"/api/admin/orgs/acme/members", token, map[string]string{
		"user_id": "alice",
	})
	resp5 := doAdmin(t, req5)
	defer resp5.Body.Close()
	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("remove member status = %d", resp5.StatusCode)
	}

	// Verify empty
	req6, _ := adminReq(http.MethodGet, srv.URL+"/api/admin/orgs/acme/members", token, nil)
	resp6 := doAdmin(t, req6)
	defer resp6.Body.Close()
	json.NewDecoder(resp6.Body).Decode(&members)
	if len(members) != 0 {
		t.Errorf("expected empty members; got %d", len(members))
	}
}

func TestAdmin_RejectsInvalidRole(t *testing.T) {
	svc, token := newTestAdminService(t)
	defer svc.Close()

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	req0, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/orgs", token, map[string]string{"slug": "acme", "name": "Acme"})
	resp0 := doAdmin(t, req0)
	resp0.Body.Close()
	req1, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/users", token, map[string]string{"id": "alice", "kind": "human"})
	resp1 := doAdmin(t, req1)
	resp1.Body.Close()

	req, _ := adminReq(http.MethodPost, srv.URL+"/api/admin/orgs/acme/members", token, map[string]string{
		"user_id": "alice", "role": "superuser",
	})
	resp := doAdmin(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid role; got %d", resp.StatusCode)
	}
}

func TestAdmin_MalformedJSON(t *testing.T) {
	svc, token := newTestAdminService(t)
	defer svc.Close()

	h := svc.Handler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/orgs", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := doAdmin(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON; got %d", resp.StatusCode)
	}
}

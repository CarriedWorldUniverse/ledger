package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// linksRESTRig spins up the full Service + HTTP handler in a temp dir
// with two issues seeded under a single project, returning the test
// server URL + issue keys for the call-side tests below to use.
func linksRESTRig(t *testing.T) (string, string, string, func()) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	svc, err := New(ctx, Config{DBPath: filepath.Join(dir, "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	a, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "blocker",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "blocked",
		DefinitionOfDone: "- [x] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(svc.Handler())
	cleanup := func() { srv.Close(); svc.Close() }
	return srv.URL, a.Key, b.Key, cleanup
}

func TestREST_LinkCreateAndList(t *testing.T) {
	url, a, b, cleanup := linksRESTRig(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{"to_key": b, "type": "blocks", "actor": "shadow"})
	resp, err := http.Post(url+"/api/issues/"+a+"/links", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create link status = %d, want 201", resp.StatusCode)
	}

	// List from the from-side: should show one outgoing link to b.
	resp2, err := http.Get(url + "/api/issues/" + a + "/links")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp2.StatusCode)
	}
	var got struct {
		Links []linkRESTRow `json:"links"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 1 {
		t.Fatalf("got %d links, want 1: %+v", len(got.Links), got.Links)
	}
	l := got.Links[0]
	if l.FromKey != a || l.ToKey != b || l.Type != "blocks" || l.Direction != "outgoing" {
		t.Errorf("link wrong: %+v", l)
	}

	// List from the to-side: same link, direction incoming.
	resp3, err := http.Get(url + "/api/issues/" + b + "/links")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var got3 struct {
		Links []linkRESTRow `json:"links"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&got3)
	if len(got3.Links) != 1 || got3.Links[0].Direction != "incoming" {
		t.Errorf("incoming view wrong: %+v", got3.Links)
	}
}

func TestREST_LinkDelete(t *testing.T) {
	url, a, b, cleanup := linksRESTRig(t)
	defer cleanup()

	mkBody := func() []byte {
		body, _ := json.Marshal(map[string]any{"to_key": b, "type": "relates-to", "actor": "shadow"})
		return body
	}

	// Create then delete.
	resp, _ := http.Post(url+"/api/issues/"+a+"/links", "application/json", bytes.NewReader(mkBody()))
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, url+"/api/issues/"+a+"/links", bytes.NewReader(mkBody()))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", resp2.StatusCode)
	}

	// Idempotent: delete again should still succeed.
	req2, _ := http.NewRequest(http.MethodDelete, url+"/api/issues/"+a+"/links", bytes.NewReader(mkBody()))
	req2.Header.Set("Content-Type", "application/json")
	resp3, _ := http.DefaultClient.Do(req2)
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("idempotent delete status = %d, want 200", resp3.StatusCode)
	}
}

func TestREST_LinkRejectsBadInput(t *testing.T) {
	url, a, _, cleanup := linksRESTRig(t)
	defer cleanup()

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty body fields", `{}`, http.StatusBadRequest},
		{"invalid link type", `{"to_key":"NEX-2","type":"frobnicates","actor":"shadow"}`, http.StatusBadRequest},
		{"self-link", `{"to_key":"` + a + `","type":"blocks","actor":"shadow"}`, http.StatusBadRequest},
		{"malformed json", `{not json`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Post(url+"/api/issues/"+a+"/links", "application/json", bytes.NewReader([]byte(c.body)))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("status = %d, want %d (body: %s)", resp.StatusCode, c.want, string(body))
			}
		})
	}
}

func TestREST_LinkMethodNotAllowed(t *testing.T) {
	url, a, _, cleanup := linksRESTRig(t)
	defer cleanup()

	req, _ := http.NewRequest(http.MethodPatch, url+"/api/issues/"+a+"/links", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PATCH status = %d, want 405", resp.StatusCode)
	}
}

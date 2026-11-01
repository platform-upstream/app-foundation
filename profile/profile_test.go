package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/identity"
	"github.com/platform-upstream/app-foundation/sqlite"
)

type photos map[string][]byte

func (p photos) Photo(_ context.Context, person identity.Person) ([]byte, error) {
	if b, ok := p[person.ID]; ok {
		return b, nil
	}
	return nil, identity.ErrUnknown
}

var (
	alice = identity.Person{ID: "alice", Name: "Alice", Group: "owners", Emails: []string{"alice@example.org"}, Role: "owner",
		PictureURL: "/internal/v1/people/alice/photo?v=7", ActsFor: []string{"emma"},
		Spaces: []identity.Space{{Kind: identity.SpacePerson, ID: "alice"}, {Kind: identity.SpaceFamily, ID: "home"}}}
	emma = identity.Person{ID: "emma", Name: "Emma", Group: "kids", Kid: true, Role: "member", Spaces: []identity.Space{{Kind: identity.SpacePerson, ID: "emma"}}}
)

func setup(t *testing.T) *Handler {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "app.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, photos{"alice": []byte("jpeg")}, "/api/me")
}

func do(h http.Handler, actor, person identity.Person, method, path string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(identity.NewContext(req.Context(), actor, person))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMeAndPhoto(t *testing.T) {
	h := setup(t)
	rec := do(h, alice, alice, http.MethodGet, "/api/me", nil)
	var m Me
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if m.Photo != "/api/me/photo?v=7" || m.Email != "alice@example.org" || len(m.Spaces) != 2 || m.Settings.Language != "" || m.Actor != nil {
		t.Fatalf("me %+v", m)
	}
	if rec := do(h, alice, alice, http.MethodGet, "/api/me/photo?v=7", nil); rec.Code != 200 || rec.Body.String() != "jpeg" || rec.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatalf("photo %d %v", rec.Code, rec.Header())
	}
	// Acting for a kid: the kid's profile, the adult as actor, no photo of the kid.
	rec = do(h, alice, emma, http.MethodGet, "/api/me", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m.ID != "emma" || m.Actor == nil || m.Actor.ID != "alice" || m.Photo != "" {
		t.Fatalf("acting %+v", m)
	}
	if rec := do(h, alice, emma, http.MethodGet, "/api/me/photo", nil); rec.Code != 404 {
		t.Fatalf("kid photo %d", rec.Code)
	}
}

func TestSettingsArePerPerson(t *testing.T) {
	h := setup(t)
	for _, c := range []struct {
		body any
		code int
	}{
		{map[string]any{"language": "de", "extra": map[string]any{"theme": "dark", "startPage": "/list"}}, 200},
		{map[string]any{"language": "fr"}, 400},
		{map[string]any{"language": "en", "extra": map[string]any{"Bad Key": 1}}, 400},
		{map[string]any{"language": "en", "unknown": true}, 400},
	} {
		if rec := do(h, alice, alice, http.MethodPut, "/api/me/settings", c.body); rec.Code != c.code {
			t.Errorf("%v: %d %s", c.body, rec.Code, rec.Body.String())
		}
	}
	s, _ := h.Get(context.Background(), "alice")
	if s.Language != "de" || string(s.Extra["theme"]) != `"dark"` {
		t.Fatalf("alice %+v", s)
	}
	// Acting for emma writes emma's settings, never alice's.
	do(h, alice, emma, http.MethodPut, "/api/me/settings", map[string]any{"language": "en"})
	if s, _ := h.Get(context.Background(), "alice"); s.Language != "de" {
		t.Fatalf("alice changed: %+v", s)
	}
	if s, _ := h.Get(context.Background(), "emma"); s.Language != "en" {
		t.Fatalf("emma %+v", s)
	}
	// Without a person (no identity middleware): refused.
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("no person %d", rec.Code)
	}
}

type dir map[string]identity.Person

func (d dir) ByEmail(_ context.Context, e string) (identity.Person, error) {
	if p, ok := d[e]; ok {
		return p, nil
	}
	return identity.Person{}, identity.ErrUnknown
}
func (d dir) ByID(context.Context, string) (identity.Person, error) {
	return identity.Person{}, identity.ErrUnknown
}

// Apps without identity.Middleware: the directory person by the Access e-mail, else one named
// after the e-mail; nobody without an Access caller.
func TestLookupByAccessEmail(t *testing.T) {
	h := setup(t).Lookup(dir{"alice@example.org": alice}).FromAccess()
	get := func(c *access.Caller) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		if c != nil {
			req = req.WithContext(access.NewContext(req.Context(), *c))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	var m Me
	_ = json.Unmarshal(get(&access.Caller{Subject: "s", Email: "Alice@Example.org"}).Body.Bytes(), &m)
	if m.ID != "alice" || m.Photo == "" {
		t.Fatalf("alice %+v", m)
	}
	_ = json.Unmarshal(get(&access.Caller{Subject: "s", Email: "bob@example.org"}).Body.Bytes(), &m)
	if m.ID != "bob@example.org" || m.Name != "bob" || m.Photo != "" {
		t.Fatalf("bob %+v", m)
	}
	if rec := get(nil); rec.Code != 401 {
		t.Fatalf("no caller %d", rec.Code)
	}
	// Without FromAccess and Lookup, only identity.Middleware's person counts.
	if rec := httptest.NewRecorder(); true {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req = req.WithContext(access.NewContext(req.Context(), access.Caller{Subject: "s", Email: "bob@example.org"}))
		setup(t).ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("plain handler served an Access caller: %d", rec.Code)
		}
	}
}

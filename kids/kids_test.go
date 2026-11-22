package kids

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/platform-upstream/app-foundation/identity"
)

const key = "test-key-0123456789abcdef0123456789"

// fake is a small directory: two families (one run by a self-registered adult), an owner who acts
// for her kid, and the kid routes with their refusals.
type fake struct {
	t        *testing.T
	people   atomic.Int32
	lastBody string
	lastType string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+key {
		http.Error(w, `{"error":"no key"}`, http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	f.lastBody, f.lastType = string(body), r.Header.Get("Content-Type")
	acting := r.URL.Query().Get("actingPerson")
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/people":
		if r.URL.Query().Get("app") != "game" {
			writeJSON(http.StatusForbidden, map[string]string{"error": "other app"})
			return
		}
		writeJSON(http.StatusOK, []Person{
			{ID: "olga", Name: "Olga", Group: "owners", ActsFor: []string{"ben", "mia"}},
			{ID: "sam", Name: "Sam", Group: "guests", SelfRegistered: true, ActsFor: []string{"mia"}},
			{ID: "ben", Name: "Ben", Kid: true, Families: []string{"home"}, PictureURL: "/internal/v1/people/ben/photo?v=42"},
			{ID: "mia", Name: "Mia", Kid: true, Families: []string{"sams"}},
			{ID: "", Name: "nobody", Kid: true},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/people/sam":
		f.people.Add(1)
		writeJSON(http.StatusOK, Person{ID: "sam", Name: "Sam", SelfRegistered: true})
	case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/people/ben/photo":
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/photo"):
		writeJSON(http.StatusNotFound, map[string]string{"error": "no photo"})
	case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/families":
		writeJSON(http.StatusOK, []Family{{ID: "home", Admins: []string{"olga"}}, {ID: "sams", Admins: []string{"sam"}}})
	case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/families/sams":
		writeJSON(http.StatusOK, Family{ID: "sams", Admins: []string{"sam"}})
	case r.Method == http.MethodGet && r.URL.Path == "/internal/v1/families/broken":
		writeJSON(http.StatusInternalServerError, map[string]string{"error": "down"})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/internal/v1/families/"):
		writeJSON(http.StatusNotFound, map[string]string{"error": "no family"})
	case r.URL.Path == "/internal/v1/families/sams/kids" && r.Method == http.MethodPost:
		if acting != "sam" {
			writeJSON(http.StatusForbidden, map[string]string{"error": "not an admin"})
			return
		}
		var b struct{ Name string }
		_ = json.Unmarshal(body, &b)
		if b.Name == "full" {
			writeJSON(http.StatusConflict, map[string]string{"error": "kid limit"})
			return
		}
		writeJSON(http.StatusCreated, Person{ID: "new-kid", Name: b.Name, Kid: true})
	case r.URL.Path == "/internal/v1/families/sams/kids/mia" && r.Method == http.MethodPut:
		var b struct{ Name string }
		_ = json.Unmarshal(body, &b)
		if b.Name == "" {
			writeJSON(http.StatusBadRequest, map[string]string{"error": "name"})
			return
		}
		writeJSON(http.StatusOK, Person{ID: "mia", Name: b.Name, Kid: true, Families: []string{"sams"}})
	case r.URL.Path == "/internal/v1/families/sams/kids/mia" && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/internal/v1/families/sams/kids/mia/photo" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		writeJSON(http.StatusOK, Person{ID: "mia", Kid: true})
	case r.URL.Path == "/internal/v1/people/ben/photo" && r.Method == http.MethodPut:
		writeJSON(http.StatusOK, Person{ID: "ben", Kid: true})
	case r.URL.Path == "/redirect":
		http.Redirect(w, r, "http://elsewhere.example.org/", http.StatusFound)
	default:
		writeJSON(http.StatusNotFound, map[string]string{"error": "no route " + r.Method + " " + r.URL.Path})
	}
}

func newClient(t *testing.T) (*Client, *fake) {
	t.Helper()
	f := &fake{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := New(identity.Config{URL: srv.URL + "/", Key: key}, "game")
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func TestNewRefusesBadConfig(t *testing.T) {
	for _, cfg := range []identity.Config{{URL: "", Key: key}, {URL: "ftp://x", Key: key}, {URL: "http://d", Key: ""}} {
		if _, err := New(cfg, "game"); err == nil {
			t.Errorf("New(%+v) accepted", cfg)
		}
	}
	if _, err := New(identity.Config{URL: "http://d", Key: key}, ""); err == nil {
		t.Error("New without app accepted")
	}
}

func TestKidChanges(t *testing.T) {
	c, f := newClient(t)
	ctx := context.Background()

	k, err := c.CreateKid(ctx, "sams", "sam", "Lou")
	if err != nil || k.ID != "new-kid" || k.Name != "Lou" || !k.Kid || len(k.Families) != 1 || k.Families[0] != "sams" {
		t.Fatalf("CreateKid = %+v, %v", k, err)
	}
	if f.lastType != "application/json" || f.lastBody != `{"name":"Lou"}` {
		t.Fatalf("body %q %q", f.lastType, f.lastBody)
	}
	if _, err := c.CreateKid(ctx, "sams", "olga", "Lou"); !errors.Is(err, ErrForbidden) || Status(err) != http.StatusForbidden {
		t.Fatalf("CreateKid by a non-admin: %v", err)
	}
	if _, err := c.CreateKid(ctx, "sams", "sam", "full"); !errors.Is(err, ErrConflict) {
		t.Fatalf("CreateKid over the limit: %v", err)
	}
	var e *Error
	if _, err := c.CreateKid(ctx, "sams", "sam", "full"); !errors.As(err, &e) || e.Message != "kid limit" {
		t.Fatalf("message lost: %v", err)
	}

	r, err := c.RenameKid(ctx, "sams", "mia", "sam", "Mira")
	if err != nil || r.Name != "Mira" {
		t.Fatalf("RenameKid = %+v, %v", r, err)
	}
	if _, err := c.RenameKid(ctx, "sams", "mia", "sam", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("RenameKid empty: %v", err)
	}
	if err := c.DeleteKid(ctx, "sams", "mia", "sam"); err != nil {
		t.Fatal(err)
	}
	if err := c.PutKidPhoto(ctx, "sams", "mia", "sam", []byte{0xFF, 0xD8}); err != nil || f.lastType != "image/jpeg" {
		t.Fatalf("PutKidPhoto: %v (%s)", err, f.lastType)
	}
	if err := c.DeleteKidPhoto(ctx, "sams", "mia", "sam"); err != nil {
		t.Fatal(err)
	}
	if err := c.PutPersonPhoto(ctx, "ben", "olga", []byte{0xFF, 0xD8}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteKid(ctx, "sams", "ghost", "sam"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteKid unknown: %v", err)
	}
}

func TestPhotoAndSelfRegistered(t *testing.T) {
	c, f := newClient(t)
	ctx := context.Background()
	jpeg, err := c.Photo(ctx, "ben")
	if err != nil || len(jpeg) != 3 {
		t.Fatalf("Photo = %v, %v", jpeg, err)
	}
	if _, err := c.Photo(ctx, "mia"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Photo without one: %v", err)
	}
	for range 3 {
		self, err := c.SelfRegistered(ctx, "sam")
		if err != nil || !self {
			t.Fatalf("SelfRegistered = %v, %v", self, err)
		}
	}
	if n := f.people.Load(); n != 1 {
		t.Fatalf("SelfRegistered asked directory %d times, want 1 (cached)", n)
	}
}

func TestAdminFamily(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()
	sam := identity.Person{ID: "sam", Spaces: []identity.Space{{Kind: identity.SpacePerson, ID: "sam"}, {Kind: identity.SpaceFamily, ID: "gone"}, {Kind: identity.SpaceFamily, ID: "sams"}}}
	if fam, err := c.AdminFamily(ctx, sam); err != nil || fam != "sams" {
		t.Fatalf("AdminFamily = %q, %v", fam, err)
	}
	member := identity.Person{ID: "olga", Spaces: []identity.Space{{Kind: identity.SpaceFamily, ID: "sams"}}}
	if fam, err := c.AdminFamily(ctx, member); err != nil || fam != "" {
		t.Fatalf("a member is no admin: %q, %v", fam, err)
	}
	broken := identity.Person{ID: "sam", Spaces: []identity.Space{{Kind: identity.SpaceFamily, ID: "broken"}}}
	if fam, err := c.AdminFamily(ctx, broken); err == nil || fam != "" || Status(err) != 0 {
		t.Fatalf("directory down: %q, %v", fam, err)
	}
}

func TestNoRedirectAndServerErrors(t *testing.T) {
	c, _ := newClient(t)
	if _, err := c.send(context.Background(), http.MethodGet, "/redirect", "", nil, 1024); err == nil || Status(err) != 0 {
		t.Fatalf("redirect followed or taken as a refusal: %v", err)
	}
}

func TestPhotoVersion(t *testing.T) {
	for in, want := range map[string]string{"": "", "/internal/v1/people/x/photo?v=12": "12", "/internal/v1/people/x/photo": "1", "%zz": ""} {
		if got := PhotoVersion(in); got != want {
			t.Errorf("PhotoVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoster(t *testing.T) {
	c, _ := newClient(t)
	r := NewRoster(c)
	if _, ok := r.Kids(); ok {
		t.Fatal("kids known before the first sync")
	}
	var nilRoster *Roster
	if nilRoster.Enabled() || !r.Enabled() {
		t.Fatal("Enabled")
	}
	var linked []Kid
	if err := r.Sync(context.Background(), func(_ context.Context, k []Kid) error { linked = k; return nil }); err != nil {
		t.Fatal(err)
	}
	ids, ok := r.Kids()
	if !ok || len(ids) != 2 || !ids["ben"] || !ids["mia"] || len(linked) != 2 {
		t.Fatalf("ids=%v linked=%+v", ids, linked)
	}
	byID := map[string]Kid{}
	for _, k := range linked {
		byID[k.ID] = k
	}
	// Ben: an owner acts for him, his family is the platform's. Mia: her family's admin signed
	// themselves up, so she never takes over a kid by name, even though an owner acts for her.
	if !byID["ben"].Trusted || byID["mia"].Trusted {
		t.Fatalf("trust: %+v", byID)
	}
	if byID["ben"].PhotoVersion != "42" || r.PhotoVersion("ben") != "42" || r.PhotoVersion("mia") != "" {
		t.Fatalf("photo versions: %+v", byID)
	}
	if !r.IsAdmin("sams", "sam") || r.IsAdmin("sams", "olga") || len(r.Admins("home")) != 1 {
		t.Fatal("admins")
	}
	if fams := r.KidFamilies("mia"); len(fams) != 1 || fams[0] != "sams" {
		t.Fatalf("families %v", fams)
	}
	if o := r.Owners(); len(o) != 1 || o[0] != "olga" {
		t.Fatalf("owners %v", o)
	}
	// The sync told the client who signed themselves up: no extra lookup.
	if self, err := c.SelfRegistered(context.Background(), "sam"); err != nil || !self {
		t.Fatalf("self %v %v", self, err)
	}

	r.Learn(Person{ID: "new-kid", Families: []string{"sams"}, PictureURL: "/p?v=7"})
	if ids, _ := r.Kids(); !ids["new-kid"] || r.PhotoVersion("new-kid") != "7" {
		t.Fatal("Learn")
	}
	r.PhotoChanged("mia", false)
	if r.PhotoVersion("mia") == "" {
		t.Fatal("PhotoChanged")
	}
	r.PhotoChanged("mia", true)
	if r.PhotoVersion("mia") != "" {
		t.Fatal("PhotoChanged removed")
	}
	r.Forget("new-kid")
	if ids, _ := r.Kids(); ids["new-kid"] {
		t.Fatal("Forget")
	}

	// A failed link keeps the last list.
	if err := r.Sync(context.Background(), func(context.Context, []Kid) error { return errors.New("db") }); err == nil {
		t.Fatal("link error lost")
	}
	if ids, ok := r.Kids(); !ok || !ids["ben"] {
		t.Fatal("last list lost")
	}
}

func TestRosterKnowFamily(t *testing.T) {
	c, _ := newClient(t)
	r := NewRoster(c)
	if err := r.KnowFamily(context.Background(), "sams"); err != nil || !r.IsAdmin("sams", "sam") {
		t.Fatalf("KnowFamily: %v", err)
	}
	if err := r.KnowFamily(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("KnowFamily unknown: %v", err)
	}
}

func TestRosterRunCallsAfterOnce(t *testing.T) {
	c, _ := newClient(t)
	r := NewRoster(c)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		r.Run(ctx, 2*time.Millisecond, nil, func(context.Context) { calls.Add(1) })
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	if n := calls.Load(); n != 1 {
		t.Fatalf("after ran %d times", n)
	}
}

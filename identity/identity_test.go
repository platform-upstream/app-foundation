package identity_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/identity"
	"github.com/platform-upstream/app-foundation/identity/identitytest"
)

var (
	alice = identity.Person{ID: "alice", Name: "Alice", Group: "owners", Emails: []string{"alice@example.org"}, Role: "owner", ActsFor: []string{"emma"}}
	bob   = identity.Person{ID: "bob", Name: "Bob", Group: "family", Emails: []string{"bob@example.org"}, Role: "viewer"}
	emma  = identity.Person{ID: "emma", Name: "Emma", Group: "kids", Kid: true, Role: "member"}
	olga  = identity.Person{ID: "olga", Name: "Olga", Group: "guests", Emails: []string{"olga@example.org"}} // no grant
)

// serve runs one request through identity.Middleware with caller set as access.Middleware would.
func serve(t *testing.T, dir identity.Resolver, opts identity.Options, caller *access.Caller, headers ...string) (*httptest.ResponseRecorder, identity.Person, identity.Person) {
	t.Helper()
	var person, actor identity.Person
	h := identity.Middleware(dir, opts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		person, _ = identity.From(r.Context())
		actor, _ = identity.ActorFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Add(headers[i], headers[i+1])
	}
	if caller != nil {
		req = req.WithContext(access.NewContext(req.Context(), *caller))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, person, actor
}

func signedIn(email string) *access.Caller {
	return &access.Caller{Subject: "sub-" + email, Email: email}
}

func TestMiddlewareResolvesThePerson(t *testing.T) {
	dir := identitytest.New(alice, bob, emma, olga)
	rec, person, actor := serve(t, dir, identity.Options{}, signedIn("Alice@Example.org"))
	if rec.Code != http.StatusNoContent || person.ID != "alice" || actor.ID != "alice" || person.Role != "owner" {
		t.Fatalf("%d %+v %+v", rec.Code, person, actor)
	}
}

func TestMiddlewareFailsClosed(t *testing.T) {
	dir := identitytest.New(alice, bob, emma, olga)
	down := identitytest.New(alice)
	down.SetUnavailable(true)
	for name, c := range map[string]struct {
		dir    identity.Resolver
		opts   identity.Options
		caller *access.Caller
		want   int
	}{
		"no caller":            {dir, identity.Options{}, nil, http.StatusUnauthorized},
		"unknown e-mail":       {dir, identity.Options{}, signedIn("mallory@example.org"), http.StatusForbidden},
		"no grant":             {dir, identity.Options{}, signedIn("olga@example.org"), http.StatusForbidden},
		"person without email": {dir, identity.Options{}, &access.Caller{Subject: "x"}, http.StatusUnauthorized},
		"local without person": {dir, identity.Options{}, &access.Caller{Local: true}, http.StatusUnauthorized},
		"local without role":   {dir, identity.Options{Local: &identity.Person{ID: "local"}}, &access.Caller{Local: true}, http.StatusForbidden},
		"unlisted token":       {dir, identity.Options{ServiceTokens: map[string]string{"other": "alice"}}, &access.Caller{ServiceToken: "agent"}, http.StatusUnauthorized},
		"token for no grant":   {dir, identity.Options{ServiceTokens: map[string]string{"agent": "olga"}}, &access.Caller{ServiceToken: "agent"}, http.StatusForbidden},
		"directory down":       {down, identity.Options{}, signedIn("alice@example.org"), http.StatusServiceUnavailable},
		"no resolver":          {nil, identity.Options{}, signedIn("alice@example.org"), http.StatusServiceUnavailable},
	} {
		rec, _, _ := serve(t, c.dir, c.opts, c.caller)
		if rec.Code != c.want || !strings.Contains(rec.Body.String(), `"error"`) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestMiddlewareLocalAndServiceTokens(t *testing.T) {
	dir := identitytest.New(alice, bob, emma)
	local := identity.Person{ID: "local", Name: "Local", Role: "owner"}
	rec, person, _ := serve(t, dir, identity.Options{Local: &local}, &access.Caller{Local: true})
	if rec.Code != http.StatusNoContent || person.ID != "local" {
		t.Fatalf("local: %d %+v", rec.Code, person)
	}
	rec, person, _ = serve(t, dir, identity.Options{ServiceTokens: map[string]string{"agent.access": "bob"}}, &access.Caller{ServiceToken: "agent.access"})
	if rec.Code != http.StatusNoContent || person.ID != "bob" || person.Role != "viewer" {
		t.Fatalf("service token: %d %+v", rec.Code, person)
	}
}

func TestActingForAKid(t *testing.T) {
	notKid := identity.Person{ID: "nils", Name: "Nils", Role: "member"}
	sneaky := alice
	sneaky.ID, sneaky.Emails, sneaky.ActsFor = "sneaky", []string{"sneaky@example.org"}, []string{"nils", "ghost"}
	dir := identitytest.New(alice, bob, emma, notKid, sneaky)

	rec, person, actor := serve(t, dir, identity.Options{}, signedIn("alice@example.org"), identity.ActForHeader, "emma")
	if rec.Code != http.StatusNoContent || person.ID != "emma" || actor.ID != "alice" {
		t.Fatalf("alice for emma: %d person %+v actor %+v", rec.Code, person, actor)
	}
	for name, c := range map[string]struct {
		email   string
		headers []string
	}{
		"not in actsFor":      {"bob@example.org", []string{identity.ActForHeader, "emma"}},
		"two headers":         {"alice@example.org", []string{identity.ActForHeader, "emma", identity.ActForHeader, "emma"}},
		"someone not granted": {"sneaky@example.org", []string{identity.ActForHeader, "ghost"}},
		"not a kid":           {"sneaky@example.org", []string{identity.ActForHeader, "nils"}},
	} {
		if rec, _, _ := serve(t, dir, identity.Options{}, signedIn(c.email), c.headers...); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

func TestRejectIsTheApps(t *testing.T) {
	opts := identity.Options{Reject: func(w http.ResponseWriter, _ *http.Request, err error) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(err.Error()))
	}}
	rec, _, _ := serve(t, identitytest.New(), opts, signedIn("x@example.org"))
	if rec.Code != http.StatusTeapot || !strings.Contains(rec.Body.String(), "no such person") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

var testKey = strings.Repeat("k", 32)

// fakeDirectory answers like directory's identity API for the app "paper".
func fakeDirectory(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/v1/people":
			_, _ = w.Write([]byte(`[{"id":"alice","name":"Alice","group":"owners","emails":["a@example.org"],"roles":{"paper":"owner"},"actsFor":["emma"]},{"id":"emma","name":"Emma","group":"kids","kid":true,"roles":{"paper":"member"}},{"id":"odd","name":"Odd","roles":{}}]`))
		case "/internal/v1/people/by-email":
			if strings.EqualFold(r.URL.Query().Get("email"), "alice+x@example.org") {
				_, _ = w.Write([]byte(`{"id":"alice","name":"Alice","group":"owners","emails":["alice+x@example.org"],"pictureUrl":"/internal/v1/people/alice/photo?v=7","kid":false,"roles":{"paper":"owner"},"actsFor":["emma"],"via":"family","spaces":[{"kind":"person","id":"alice"},{"kind":"family","id":"home"},{"kind":"family","id":"Bad Id"},{"kind":"team","id":"x"}]}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case "/internal/v1/people/emma":
			_, _ = w.Write([]byte(`{"id":"emma","name":"Emma","group":"kids","emails":null,"pictureUrl":"","kid":true,"roles":{"paper":"member"}}`))
		case "/internal/v1/people/norole":
			_, _ = w.Write([]byte(`{"id":"norole","name":"N","group":"family","emails":[],"roles":{}}`))
		case "/internal/v1/people/tworoles":
			_, _ = w.Write([]byte(`{"id":"tworoles","name":"T","group":"family","emails":[],"roles":{"paper":"owner","cook":"owner"}}`))
		case "/internal/v1/people/broken":
			_, _ = w.Write([]byte(`{"id":`))
		case "/internal/v1/people/huge":
			_, _ = w.Write([]byte(`{"id":"huge","name":"` + strings.Repeat("x", maxPersonBytes) + `"}`))
		case "/internal/v1/people/fail":
			w.WriteHeader(http.StatusBadGateway)
		case "/internal/v1/people/moved":
			http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
		case "/internal/v1/people/alice/photo":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestClientReadsDirectory(t *testing.T) {
	srv, seen := fakeDirectory(t)
	c, err := NewClient(Config{URL: srv.URL + "/", Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := c.ByEmail(ctx, " Alice+X@example.org ")
	if err != nil || p.ID != "alice" || p.Role != "owner" || !p.CanActFor("emma") || p.CanActFor("") {
		t.Fatalf("by e-mail: %+v %v", p, err)
	}
	if (*seen)[0] != "/internal/v1/people/by-email?email=Alice%2BX%40example.org" {
		t.Fatalf("e-mail not escaped: %s", (*seen)[0])
	}
	kid, err := c.ByID(ctx, "emma")
	if err != nil || !kid.Kid || kid.Role != "member" || kid.Emails == nil || kid.ActsFor == nil {
		t.Fatalf("kid: %+v %v", kid, err)
	}
	photo, err := c.Photo(ctx, p)
	if err != nil || string(photo) != "jpeg" {
		t.Fatalf("photo: %q %v", photo, err)
	}
	if _, err := c.Photo(ctx, kid); !errors.Is(err, ErrUnknown) {
		t.Fatalf("no photo: %v", err)
	}
	if _, err := c.Photo(ctx, Person{PictureURL: "https://evil.example/internal/v1/people/x"}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("absolute photo URL: %v", err)
	}
}

func TestClientListsTheAppsPeople(t *testing.T) {
	srv, _ := fakeDirectory(t)
	c, _ := NewClient(Config{URL: srv.URL, Key: testKey})
	people, err := c.People(context.Background())
	if err != nil || len(people) != 2 || people[0].ID != "alice" || !people[1].Kid || people[1].ActsFor == nil {
		t.Fatalf("%+v %v", people, err)
	}
	srv.Close()
	if _, err := c.People(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed: %v", err)
	}
}

func TestClientFailsClosed(t *testing.T) {
	srv, _ := fakeDirectory(t)
	c, _ := NewClient(Config{URL: srv.URL, Key: testKey})
	wrongKey, _ := NewClient(Config{URL: srv.URL, Key: strings.Repeat("w", 32)})
	ctx := context.Background()
	for name, c := range map[string]struct {
		lookup func() (Person, error)
		want   error
	}{
		"unknown e-mail":   {func() (Person, error) { return c.ByEmail(ctx, "x@example.org") }, ErrUnknown},
		"empty e-mail":     {func() (Person, error) { return c.ByEmail(ctx, " ") }, ErrUnknown},
		"invalid id":       {func() (Person, error) { return c.ByID(ctx, "../admin") }, ErrUnknown},
		"no role":          {func() (Person, error) { return c.ByID(ctx, "norole") }, ErrUnknown},
		"two roles":        {func() (Person, error) { return c.ByID(ctx, "tworoles") }, ErrUnknown},
		"broken JSON":      {func() (Person, error) { return c.ByID(ctx, "broken") }, ErrUnavailable},
		"too large":        {func() (Person, error) { return c.ByID(ctx, "huge") }, ErrUnavailable},
		"5xx":              {func() (Person, error) { return c.ByID(ctx, "fail") }, ErrUnavailable},
		"redirect":         {func() (Person, error) { return c.ByID(ctx, "moved") }, ErrUnavailable},
		"key not accepted": {func() (Person, error) { return wrongKey.ByID(ctx, "emma") }, ErrUnavailable},
	} {
		if _, err := c.lookup(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	srv.Close()
	if _, err := c.ByID(ctx, "emma"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed server: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv(EnvURL, "")
	t.Setenv(EnvKey, "short")
	_, err := ConfigFromEnv()
	if err == nil || !strings.Contains(err.Error(), EnvURL) || !strings.Contains(err.Error(), EnvKey) {
		t.Fatalf("missing values: %v", err)
	}
	t.Setenv(EnvURL, "http://directory.directory-prod.svc:8080/internal")
	t.Setenv(EnvKey, testKey)
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), EnvURL) {
		t.Fatalf("URL with path: %v", err)
	}
	t.Setenv(EnvURL, "http://directory.directory-prod.svc:8080")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.Key != testKey {
		t.Fatalf("valid: %+v %v", cfg, err)
	}
	for _, bad := range []string{"directory:8080", "ftp://d", "http://user:pw@d", "http://d?x=1", ""} {
		if _, err := NewClient(Config{URL: bad, Key: testKey}); err == nil {
			t.Errorf("accepted URL %q", bad)
		}
	}
	if _, err := NewClient(Config{URL: "http://d", Key: "short"}); err == nil {
		t.Error("accepted short key")
	}
}

func TestClientReadsSpaces(t *testing.T) {
	srv, _ := fakeDirectory(t)
	c, err := NewClient(Config{URL: srv.URL, Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	alice, err := c.ByEmail(ctx, "alice+x@example.org")
	if err != nil {
		t.Fatal(err)
	}
	// Own space first, family spaces kept, malformed entries dropped.
	if want := []Space{{SpacePerson, "alice"}, {SpaceFamily, "home"}}; !reflect.DeepEqual(alice.Spaces, want) || alice.Via != "family" {
		t.Fatalf("alice %+v %q", alice.Spaces, alice.Via)
	}
	if !alice.InSpace(Space{SpaceFamily, "home"}) || alice.InSpace(Space{SpaceFamily, "other"}) || !reflect.DeepEqual(alice.FamilySpaces(), []string{"home"}) {
		t.Fatal("InSpace/FamilySpaces")
	}
	// An older directory without spaces: the own space only.
	emma, _ := c.ByID(ctx, "emma")
	if !reflect.DeepEqual(emma.Spaces, []Space{{SpacePerson, "emma"}}) {
		t.Fatalf("emma %+v", emma.Spaces)
	}
}

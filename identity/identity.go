// Package identity turns the verified Access caller into a person of the platform's directory:
// who is calling (id, name, photo), their role in this app, and the kid profiles they may act for.
// Apps keep no user lists; they key their data by Person.ID.
//
// Compose it right after access.Middleware: access proves the login, Middleware resolves the
// person and refuses anyone directory does not grant this app (fail closed). The Client reads
// directory's identity API inside the cluster; wrap it in a Cache so a short directory outage
// does not sign everyone out. identitytest has an in-memory directory for tests.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/platform-upstream/app-foundation/access"
)

var (
	// ErrUnknown: directory knows no such person for this app (no grant counts as unknown).
	ErrUnknown = errors.New("identity: no such person in this app")
	// ErrUnavailable: directory did not answer usably (network, timeout, 5xx, rejected key).
	ErrUnavailable = errors.New("identity: directory unavailable")
	// ErrNoCaller: the request carries no caller from access.Middleware, or one Options does not
	// map to a person (a local request without Options.Local, an unlisted service token).
	ErrNoCaller = errors.New("identity: request has no person")
	// ErrActFor: the act-for header names someone the caller may not act for.
	ErrActFor = errors.New("identity: not allowed to act for that person")
)

// ActForHeader names the kid profile an adult's request acts for. The app's frontend sends it;
// Middleware accepts only ids in the caller's Person.ActsFor.
const ActForHeader = "X-Act-For"

// Person is one person of the platform as this app sees them.
type Person struct {
	// ID is a stable slug; key the app's data by it.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Group is owners, family, guests or kids.
	Group  string   `json:"group"`
	Emails []string `json:"emails"`
	// PictureURL is the uploaded photo, relative to directory and readable only with the app's
	// key (Client.Photo); "" without one (show initials).
	PictureURL string `json:"pictureUrl"`
	// Kid is a profile without login; an adult's request acts for it (ActForHeader).
	Kid bool `json:"kid"`
	// Role is the person's role in this app: owner, member or viewer. Never empty for a person a
	// Resolver returns.
	Role string `json:"role"`
	// ActsFor lists the kid profiles this person may act for in this app.
	ActsFor []string `json:"actsFor"`
}

// CanActFor reports whether p may act for the person with id.
func (p Person) CanActFor(id string) bool { return id != "" && slices.Contains(p.ActsFor, id) }

// Resolver finds the people of this app. Both methods return ErrUnknown for anyone directory
// does not grant this app and an error wrapping ErrUnavailable when it cannot tell.
type Resolver interface {
	ByEmail(ctx context.Context, email string) (Person, error)
	ByID(ctx context.Context, id string) (Person, error)
}

type contextKey struct{}

type subjects struct{ actor, person Person }

// NewContext returns ctx carrying actor (who signed in) and person (whom the request acts for;
// the same as actor unless an adult acts for a kid). Middleware does this; tests of handlers use
// it directly.
func NewContext(ctx context.Context, actor, person Person) context.Context {
	return context.WithValue(ctx, contextKey{}, subjects{actor, person})
}

// From returns the person the request acts for: the owner of the data it reads and writes.
func From(ctx context.Context) (Person, bool) {
	s, ok := ctx.Value(contextKey{}).(subjects)
	return s.person, ok
}

// ActorFrom returns the person who signed in. It differs from From only while acting for a kid;
// use it for audit fields ("added by").
func ActorFrom(ctx context.Context) (Person, bool) {
	s, ok := ctx.Value(contextKey{}).(subjects)
	return s.actor, ok
}

// Options say how Middleware treats callers that are not a signed-in person.
type Options struct {
	// Local is the person a local-mode request (access.Caller.Local, loopback only) acts as, for
	// the local preview. nil refuses local requests.
	Local *Person
	// ServiceTokens maps an accepted Access service token's client ID to the id of the person it
	// acts for (resolved through directory like anyone else). Unlisted tokens are refused; a
	// read-only service route belongs outside this middleware.
	ServiceTokens map[string]string
	// Reject writes the failure response. nil writes JSON: 401 for ErrNoCaller, 403 for
	// ErrUnknown and ErrActFor, 503 for anything else (ErrUnavailable).
	Reject access.RejectFunc
}

// Middleware resolves the caller access.Middleware verified into a Person and puts it into the
// request context (From, ActorFrom). It fails closed: no caller, a person directory does not
// grant this app, a disallowed act-for header or an unavailable directory all end in Reject.
// A nil resolver (the local preview, where only Options.Local passes) answers ErrUnavailable.
func Middleware(resolver Resolver, opts Options) func(http.Handler) http.Handler {
	if resolver == nil {
		resolver = noDirectory{}
	}
	reject := opts.Reject
	if reject == nil {
		reject = defaultReject
	}
	tokens := make(map[string]string, len(opts.ServiceTokens))
	for client, id := range opts.ServiceTokens {
		tokens[client] = id
	}
	var local *Person
	if opts.Local != nil {
		p := *opts.Local
		local = &p
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, err := resolveCaller(r, resolver, local, tokens)
			if err == nil && actor.Role == "" {
				err = ErrUnknown
			}
			if err != nil {
				reject(w, r, err)
				return
			}
			person, err := actFor(r, resolver, actor)
			if err != nil {
				reject(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), actor, person)))
		})
	}
}

func resolveCaller(r *http.Request, resolver Resolver, local *Person, tokens map[string]string) (Person, error) {
	caller, ok := access.CallerFrom(r.Context())
	switch {
	case !ok:
		return Person{}, ErrNoCaller
	case caller.Local:
		if local == nil {
			return Person{}, ErrNoCaller
		}
		return *local, nil
	case caller.ServiceToken != "":
		id, listed := tokens[caller.ServiceToken]
		if !listed || id == "" {
			return Person{}, ErrNoCaller
		}
		return resolver.ByID(r.Context(), id)
	case caller.Person() && caller.Email != "":
		return resolver.ByEmail(r.Context(), caller.Email)
	default:
		return Person{}, ErrNoCaller
	}
}

// actFor returns the person named by ActForHeader when actor may act for them, else actor.
func actFor(r *http.Request, resolver Resolver, actor Person) (Person, error) {
	values := r.Header.Values(ActForHeader)
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		return actor, nil
	}
	if len(values) != 1 || !actor.CanActFor(values[0]) {
		return Person{}, ErrActFor
	}
	kid, err := resolver.ByID(r.Context(), values[0])
	switch {
	case errors.Is(err, ErrUnknown):
		return Person{}, ErrActFor
	case err != nil:
		return Person{}, err
	case !kid.Kid:
		return Person{}, ErrActFor
	}
	return kid, nil
}

type noDirectory struct{}

func (noDirectory) ByEmail(context.Context, string) (Person, error) { return Person{}, ErrUnavailable }
func (noDirectory) ByID(context.Context, string) (Person, error)    { return Person{}, ErrUnavailable }

func defaultReject(w http.ResponseWriter, _ *http.Request, err error) {
	status, message := http.StatusServiceUnavailable, "Sign-in is unavailable right now. Try again shortly."
	switch {
	case errors.Is(err, ErrNoCaller):
		status, message = http.StatusUnauthorized, "Sign-in required."
	case errors.Is(err, ErrUnknown), errors.Is(err, ErrActFor):
		status, message = http.StatusForbidden, "No access."
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// Package profile serves the signed-in person to the app's frontend: who they are (from
// identity), their photo from directory, and their settings in this app (language and anything the
// app adds). The frontend module of the same name (frontend registry "profile") shows it bottom left
// with a settings dialog.
//
// Mount Handler under /api/me behind identity.Middleware. Settings live in the app's own SQLite
// (table person_settings, created on first use), keyed by the person the request acts for.
package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/httpx"
	"github.com/platform-upstream/app-foundation/identity"
)

// Languages the frontend module ships. "" follows the browser.
var Languages = []string{"de", "en"}

// MaxSettingsBytes bounds one person's settings document.
const MaxSettingsBytes = 16 << 10

// Photos reads a person's photo from directory (identity.Client.Photo). nil: no photos.
type Photos interface {
	Photo(ctx context.Context, p identity.Person) ([]byte, error)
}

// AppLister lists a person's other apps (identity.Client.Apps). nil: no list.
type AppLister interface {
	Apps(ctx context.Context, id string) ([]identity.App, error)
}

// Me is what GET /api/me answers.
type Me struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
	Email string `json:"email"`
	Role  string `json:"role"`
	Kid   bool   `json:"kid"`
	// Photo is the app-relative photo URL ("" without one: show initials).
	Photo string `json:"photo"`
	// Actor is who signed in when the request acts for a kid; nil otherwise.
	Actor *Me `json:"actor,omitempty"`
	// Spaces says where this person's data lives in this app.
	Spaces   []identity.Space `json:"spaces"`
	Settings Settings         `json:"settings"`
}

// Settings is one person's settings in this app: the language, plus app keys in Extra.
type Settings struct {
	// Language is "de", "en" or "" (the browser's).
	Language string `json:"language"`
	// Extra holds the app's own settings (a flat object of small values).
	Extra map[string]json.RawMessage `json:"extra"`
}

// Handler serves GET /api/me, GET /api/me/photo and GET/PUT /api/me/settings. prefix is where it
// is mounted ("/api/me"); db holds the settings.
type Handler struct {
	db     *sql.DB
	photos Photos
	prefix string
	lookup identity.Resolver
	apps   AppLister
	// anyCaller: serve a profile from the Access e-mail even without a lookup.
	anyCaller bool
	// mu guards ready: the settings table exists.
	mu    sync.Mutex
	ready bool
}

// FromAccess serves a profile to any caller access.Middleware verified (named after the e-mail),
// for apps without directory; Lookup adds the directory person.
func (h *Handler) FromAccess() *Handler {
	h.anyCaller = true
	return h
}

// New returns the handler; photos may be nil.
func New(db *sql.DB, photos Photos, prefix string) *Handler {
	return &Handler{db: db, photos: photos, prefix: strings.TrimRight(prefix, "/")}
}

var keyPattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9_.-]{0,39}$`)

// init creates the settings table on first use. Only a success is remembered: a first request
// that fails (its client went away, the database was busy) leaves the next one to try again.
func (h *Handler) init(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ready {
		return nil
	}
	if _, err := h.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS person_settings (
			person_id  TEXT    PRIMARY KEY,
			settings   TEXT    NOT NULL,
			updated_at INTEGER NOT NULL
		)`); err != nil {
		return err
	}
	h.ready = true
	return nil
}

// Lookup lets the handler serve apps that do not run identity.Middleware yet (they gate by the
// Access e-mail themselves): the person is looked up by the signed-in e-mail for display only. A
// person directory does not know (or no directory) still gets a profile, named after the e-mail.
// The lookup never admits anyone: the app's own gate stays in front.
func (h *Handler) Lookup(r identity.Resolver) *Handler {
	h.lookup = r
	return h
}

// Apps serves GET {prefix}/apps: the person's other apps for the menu, from directory.
func (h *Handler) Apps(l AppLister) *Handler {
	h.apps = l
	return h
}

func (h *Handler) caller(r *http.Request) (identity.Person, bool) {
	if p, ok := identity.From(r.Context()); ok {
		return p, true
	}
	if h.lookup == nil && !h.anyCaller {
		return identity.Person{}, false
	}
	c, ok := access.CallerFrom(r.Context())
	switch {
	case !ok:
		return identity.Person{}, false
	case c.Local:
		return identity.Person{ID: "local", Name: "Local", Group: "owners", Role: "owner", Spaces: []identity.Space{{Kind: identity.SpacePerson, ID: "local"}}}, true
	case c.Email == "":
		return identity.Person{}, false
	}
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if h.lookup != nil {
		if p, err := h.lookup.ByEmail(r.Context(), email); err == nil {
			return p, true
		}
	}
	name, _, _ := strings.Cut(email, "@")
	return identity.Person{ID: email, Name: name, Emails: []string{email}, Role: "member", Spaces: []identity.Space{{Kind: identity.SpacePerson, ID: email}}}, true
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	person, ok := h.caller(r)
	if !ok {
		_ = httpx.WriteError(w, http.StatusUnauthorized, "Please sign in again.")
		return
	}
	if err := h.init(r.Context()); err != nil {
		_ = httpx.WriteError(w, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	switch rest := strings.TrimPrefix(r.URL.Path, h.prefix); {
	case (rest == "" || rest == "/") && r.Method == http.MethodGet:
		h.me(w, r, person)
	case rest == "/apps" && r.Method == http.MethodGet:
		h.otherApps(w, r, person)
	case rest == "/photo" && r.Method == http.MethodGet:
		h.photo(w, r, person)
	case rest == "/settings" && r.Method == http.MethodGet:
		s, err := h.Get(r.Context(), person.ID)
		h.write(w, s, err)
	case rest == "/settings" && r.Method == http.MethodPut:
		h.put(w, r, person)
	default:
		_ = httpx.WriteError(w, http.StatusNotFound, "Not found.")
	}
}

func view(p identity.Person, prefix string) Me {
	m := Me{ID: p.ID, Name: p.Name, Group: p.Group, Role: p.Role, Kid: p.Kid, Spaces: p.Spaces}
	if len(p.Emails) > 0 {
		m.Email = p.Emails[0]
	}
	if p.PictureURL != "" {
		// The directory URL carries a version (?v=…); keep it so a new photo is a new URL.
		v := ""
		if i := strings.Index(p.PictureURL, "?"); i >= 0 {
			v = p.PictureURL[i:]
		}
		m.Photo = prefix + "/photo" + v
	}
	if m.Spaces == nil {
		m.Spaces = []identity.Space{{Kind: identity.SpacePerson, ID: p.ID}}
	}
	return m
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request, person identity.Person) {
	s, err := h.Get(r.Context(), person.ID)
	if err != nil {
		h.write(w, nil, err)
		return
	}
	m := view(person, h.prefix)
	m.Settings = s
	if actor, ok := identity.ActorFrom(r.Context()); ok && actor.ID != person.ID {
		a := view(actor, h.prefix)
		a.Photo = ""
		a.Spaces = nil
		m.Actor = &a
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = httpx.WriteJSON(w, http.StatusOK, m)
}

func (h *Handler) photo(w http.ResponseWriter, r *http.Request, person identity.Person) {
	if h.photos == nil || person.PictureURL == "" {
		_ = httpx.WriteError(w, http.StatusNotFound, "No photo.")
		return
	}
	data, err := h.photos.Photo(r.Context(), person)
	switch {
	case errors.Is(err, identity.ErrUnknown):
		_ = httpx.WriteError(w, http.StatusNotFound, "No photo.")
		return
	case err != nil:
		_ = httpx.WriteError(w, http.StatusServiceUnavailable, "The photo is not available right now.")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// otherApps answers [] when there is no list (no lister, a kid acting through an adult, directory
// unreachable): the menu then just shows none.
func (h *Handler) otherApps(w http.ResponseWriter, r *http.Request, person identity.Person) {
	list := []identity.App{}
	id := person.ID
	if actor, ok := identity.ActorFrom(r.Context()); ok {
		id = actor.ID
	}
	if h.apps != nil {
		if got, err := h.apps.Apps(r.Context(), id); err == nil {
			list = got
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	_ = httpx.WriteJSON(w, http.StatusOK, list)
}

// Get returns a person's settings in this app (defaults when none are stored).
func (h *Handler) Get(ctx context.Context, personID string) (Settings, error) {
	if err := h.init(ctx); err != nil {
		return Settings{}, err
	}
	var raw string
	err := h.db.QueryRowContext(ctx, `SELECT settings FROM person_settings WHERE person_id = ?`, personID).Scan(&raw)
	s := Settings{Extra: map[string]json.RawMessage{}}
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Extra == nil {
		s = Settings{Extra: map[string]json.RawMessage{}, Language: s.Language}
	}
	return s, nil
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request, person identity.Person) {
	var s Settings
	if err := httpx.DecodeJSON(w, r, &s, MaxSettingsBytes); err != nil {
		_ = httpx.WriteError(w, httpx.StatusCode(err), "Invalid settings.")
		return
	}
	if s.Language != "" && s.Language != "de" && s.Language != "en" {
		_ = httpx.WriteError(w, http.StatusBadRequest, "Language is de, en or empty (the browser's).")
		return
	}
	if s.Extra == nil {
		s.Extra = map[string]json.RawMessage{}
	}
	if len(s.Extra) > 50 {
		_ = httpx.WriteError(w, http.StatusBadRequest, "At most 50 settings.")
		return
	}
	for k, v := range s.Extra {
		if !keyPattern.MatchString(k) || len(v) > 2048 {
			_ = httpx.WriteError(w, http.StatusBadRequest, "Invalid setting: "+k)
			return
		}
	}
	raw, _ := json.Marshal(s)
	if _, err := h.db.ExecContext(r.Context(), `INSERT INTO person_settings (person_id, settings, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (person_id) DO UPDATE SET settings = excluded.settings, updated_at = excluded.updated_at`,
		person.ID, string(raw), time.Now().UnixMilli()); err != nil {
		h.write(w, nil, err)
		return
	}
	h.write(w, s, nil)
}

func (h *Handler) write(w http.ResponseWriter, v any, err error) {
	if err != nil {
		_ = httpx.WriteError(w, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = httpx.WriteJSON(w, http.StatusOK, v)
}

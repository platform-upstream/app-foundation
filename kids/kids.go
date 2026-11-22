// Package kids is the client for the kid profiles an app keeps in the platform's directory: the
// kids granted the app and their families (Roster), and the changes a family admin makes from a
// signup app (create, rename, delete a kid profile, set or remove its photo). Directory is the one
// source of kid photos; the app proxies them, because directory's /internal routes answer pods only.
//
// The client reads the same DIRECTORY_URL and DIRECTORY_KEY as identity (identity.Config). The
// app keeps its own per-kid settings and links them to Person.ID; copy and HTTP answers stay in the
// app (map refusals with Status or errors.Is against ErrForbidden, ErrNotFound, ErrConflict and
// ErrInvalid).
package kids

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/platform-upstream/app-foundation/identity"
)

// Person is a person of directory's identity API, as far as kid handling needs one.
type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kid   bool   `json:"kid"`
	Group string `json:"group"`
	// Families are the families a kid profile belongs to.
	Families []string `json:"families"`
	// ActsFor are the kid profiles an adult may act for in the app.
	ActsFor []string `json:"actsFor"`
	// PictureURL is directory's photo path with its version (?v=); "" without a photo.
	PictureURL string `json:"pictureUrl"`
	// SelfRegistered: the person signed themselves up through a signup app.
	SelfRegistered bool `json:"selfRegistered"`
}

// PhotoVersion is the version of the person's photo, "" without one.
func (p Person) PhotoVersion() string { return PhotoVersion(p.PictureURL) }

// PhotoVersion takes the v of a pictureUrl ("/internal/v1/people/x/photo?v=123"): "" without a
// photo, "1" for a photo without a version. Apps put it in their photo URLs, so a new photo has a
// new address and the old one can be cached for good.
func PhotoVersion(pictureURL string) string {
	if pictureURL == "" {
		return ""
	}
	u, err := url.Parse(pictureURL)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("v"); v != "" {
		return v
	}
	return "1"
}

// Family is a family that keeps a space in the app.
type Family struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Admins  []string `json:"admins"`
	Members []string `json:"members"`
}

// Refusals by directory's status; an *Error matches them with errors.Is.
var (
	// ErrForbidden: not a signup app, or the acting person is no admin of the family (403).
	ErrForbidden = errors.New("kids: not allowed")
	// ErrNotFound: no such person, photo or family for the app, or the kid is not in it (404).
	ErrNotFound = errors.New("kids: not found")
	// ErrConflict: the family's kid limit, or a kid that also belongs to another family (409).
	ErrConflict = errors.New("kids: conflict")
	// ErrInvalid: directory refused the name or the picture (400, 413, 415).
	ErrInvalid = errors.New("kids: invalid")
)

// Error is directory's refusal (a 4xx) with its status and message. Other failures (network, 5xx)
// are plain errors: the app answers them as "directory unavailable".
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("kids: directory %d %s", e.Status, e.Message) }

// Is matches the refusal sentinels.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrConflict:
		return e.Status == http.StatusConflict
	case ErrInvalid:
		return e.Status == http.StatusBadRequest || e.Status == http.StatusRequestEntityTooLarge ||
			e.Status == http.StatusUnsupportedMediaType
	}
	return false
}

// Status is directory's status when err is a refusal, else 0.
func Status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// selfTTL is how long a selfRegistered answer is reused; the flag does not change for a person.
const selfTTL = 10 * time.Minute

type selfEntry struct {
	self bool
	at   time.Time
}

// Client calls directory with the app's identity API key. Safe for concurrent use.
type Client struct {
	base string
	key  string
	app  string
	http *http.Client

	mu   sync.Mutex
	self map[string]selfEntry
}

// New returns a client for directory at cfg.URL with cfg.Key; app is the app's name in directory
// (the people granted it are listed with ?app=). The key never follows a redirect.
func New(cfg identity.Config, app string) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("kids: bad directory URL %q", cfg.URL)
	}
	if cfg.Key == "" {
		return nil, errors.New("kids: directory key is required")
	}
	if app == "" {
		return nil, errors.New("kids: app name is required")
	}
	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: identity.Timeout}
	}
	noRedirect := *c
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base.String(), key: cfg.Key, app: app, http: &noRedirect, self: map[string]selfEntry{}}, nil
}

// send makes one call and returns the body of a 2xx answer; a 4xx is an *Error.
func (c *Client) send(ctx context.Context, method, path, contentType string, body []byte, limit int64) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Error
		if msg == "" {
			msg = e.Message
		}
		return nil, &Error{Status: resp.StatusCode, Message: msg}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kids: directory %s", resp.Status)
	}
	return data, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	data, err := c.send(ctx, http.MethodGet, path, "", nil, 4<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// People returns the people granted the app, kid profiles included.
func (c *Client) People(ctx context.Context) ([]Person, error) {
	var people []Person
	err := c.getJSON(ctx, "/internal/v1/people?app="+url.QueryEscape(c.app), &people)
	return people, err
}

// Person returns one person (ErrNotFound for someone directory does not know).
func (c *Client) Person(ctx context.Context, id string) (Person, error) {
	var p Person
	err := c.getJSON(ctx, "/internal/v1/people/"+url.PathEscape(id), &p)
	if err == nil {
		c.remember(p.ID, p.SelfRegistered)
	}
	return p, err
}

// Families returns the families that keep a space in the app.
func (c *Client) Families(ctx context.Context) ([]Family, error) {
	var families []Family
	err := c.getJSON(ctx, "/internal/v1/families", &families)
	return families, err
}

// Family returns one family that keeps a space in the app (ErrNotFound otherwise).
func (c *Client) Family(ctx context.Context, id string) (Family, error) {
	var f Family
	err := c.getJSON(ctx, "/internal/v1/families/"+url.PathEscape(id), &f)
	return f, err
}

// Photo returns a person's photo (JPEG, at most 512 px); ErrNotFound without one.
func (c *Client) Photo(ctx context.Context, person string) ([]byte, error) {
	return c.send(ctx, http.MethodGet, "/internal/v1/people/"+url.PathEscape(person)+"/photo", "", nil, 4<<20)
}

// SelfRegistered reports whether the person signed themselves up through a signup app. The
// platform's own people, the guests they added and kid profiles are false. Answers are cached.
func (c *Client) SelfRegistered(ctx context.Context, person string) (bool, error) {
	c.mu.Lock()
	e, ok := c.self[person]
	c.mu.Unlock()
	if ok && time.Since(e.at) < selfTTL {
		return e.self, nil
	}
	p, err := c.Person(ctx, person)
	if err != nil {
		return false, err
	}
	return p.SelfRegistered, nil
}

func (c *Client) remember(person string, self bool) {
	if person == "" {
		return
	}
	c.mu.Lock()
	c.self[person] = selfEntry{self: self, at: time.Now()}
	c.mu.Unlock()
}

// AdminFamily is the first family space of actor (identity.Person.FamilySpaces) that actor
// administers: where new kid profiles go. "" when there is none. A family directory no longer
// knows is skipped; another failure is returned when no family was found.
func (c *Client) AdminFamily(ctx context.Context, actor identity.Person) (string, error) {
	var firstErr error
	for _, id := range actor.FamilySpaces() {
		f, err := c.Family(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if slices.Contains(f.Admins, actor.ID) {
			return f.ID, nil
		}
	}
	return "", firstErr
}

func familyKids(family, kid, suffix, acting string) string {
	p := "/internal/v1/families/" + url.PathEscape(family) + "/kids"
	if kid != "" {
		p += "/" + url.PathEscape(kid)
	}
	return p + suffix + "?actingPerson=" + url.QueryEscape(acting)
}

func (c *Client) kidCall(ctx context.Context, method, path, contentType string, body []byte) (Person, error) {
	data, err := c.send(ctx, method, path, contentType, body, 1<<20)
	if err != nil {
		return Person{}, err
	}
	var p Person
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &p); err != nil {
			return Person{}, fmt.Errorf("kids: directory answer: %w", err)
		}
	}
	return p, nil
}

func nameBody(name string) []byte {
	b, _ := json.Marshal(map[string]string{"name": name})
	return b
}

// CreateKid adds a kid profile to family on behalf of acting (a family admin, signup apps only)
// and returns its identity.
func (c *Client) CreateKid(ctx context.Context, family, acting, name string) (Person, error) {
	p, err := c.kidCall(ctx, http.MethodPost, familyKids(family, "", "", acting), "application/json", nameBody(name))
	if err != nil {
		return Person{}, err
	}
	if p.ID == "" {
		return Person{}, errors.New("kids: directory answer without id")
	}
	p.Kid = true
	if len(p.Families) == 0 {
		p.Families = []string{family}
	}
	return p, nil
}

// RenameKid renames one of the family's kid profiles and returns its identity.
func (c *Client) RenameKid(ctx context.Context, family, kid, acting, name string) (Person, error) {
	p, err := c.kidCall(ctx, http.MethodPut, familyKids(family, kid, "", acting), "application/json", nameBody(name))
	if err != nil {
		return Person{}, err
	}
	if p.ID == "" {
		p.ID, p.Name = kid, name
	}
	p.Kid = true
	return p, nil
}

// DeleteKid deletes one of the family's kid profiles (from every app).
func (c *Client) DeleteKid(ctx context.Context, family, kid, acting string) error {
	_, err := c.send(ctx, http.MethodDelete, familyKids(family, kid, "", acting), "", nil, 1<<20)
	return err
}

// PutKidPhoto sets the photo (a JPEG) of one of the family's kid profiles.
func (c *Client) PutKidPhoto(ctx context.Context, family, kid, acting string, jpeg []byte) error {
	_, err := c.send(ctx, http.MethodPut, familyKids(family, kid, "/photo", acting), "image/jpeg", jpeg, 1<<20)
	return err
}

// DeleteKidPhoto removes the photo of one of the family's kid profiles.
func (c *Client) DeleteKidPhoto(ctx context.Context, family, kid, acting string) error {
	_, err := c.send(ctx, http.MethodDelete, familyKids(family, kid, "/photo", acting), "", nil, 1<<20)
	return err
}

// PutPersonPhoto sets the photo of a kid in no family, on behalf of acting (someone who acts for
// the kid).
func (c *Client) PutPersonPhoto(ctx context.Context, kid, acting string, jpeg []byte) error {
	path := "/internal/v1/people/" + url.PathEscape(kid) + "/photo?actingPerson=" + url.QueryEscape(acting)
	_, err := c.send(ctx, http.MethodPut, path, "image/jpeg", jpeg, 1<<20)
	return err
}

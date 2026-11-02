package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/platform-upstream/app-foundation/access"
)

// Environment variables read by ConfigFromEnv.
const (
	EnvURL = "DIRECTORY_URL"
	EnvKey = "DIRECTORY_KEY"
)

// Timeout bounds each request of the Client's default HTTP client.
const Timeout = 5 * time.Second

const (
	maxPersonBytes = 64 << 10
	maxPhotoBytes  = 2 << 20
	maxListBytes   = 1 << 20
)

var idPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// Config locates directory's identity API.
type Config struct {
	// URL is directory's in-cluster base URL, such as http://directory-prod.directory-prod.svc:8080.
	URL string
	// Key is this app's identity API key (at least access.MinInternalKeyLength characters); the
	// same value is directory's DIRECTORY_CALLER_KEY_<APP>.
	Key string
	// HTTPClient defaults to one with Timeout. It must not follow redirects off directory.
	HTTPClient *http.Client
}

// ConfigFromEnv reads DIRECTORY_URL and DIRECTORY_KEY. It fails closed: a missing or malformed
// value is an error naming the variable.
func ConfigFromEnv() (Config, error) {
	cfg := Config{URL: strings.TrimSpace(os.Getenv(EnvURL)), Key: os.Getenv(EnvKey)}
	var problems []error
	if cfg.URL == "" {
		problems = append(problems, fmt.Errorf("%s is required (directory's in-cluster URL)", EnvURL))
	} else if _, err := baseURL(cfg.URL); err != nil {
		problems = append(problems, fmt.Errorf("%s: %w", EnvURL, err))
	}
	if len(cfg.Key) < access.MinInternalKeyLength {
		problems = append(problems, fmt.Errorf("%s is required (at least %d characters)", EnvKey, access.MinInternalKeyLength))
	}
	if err := errors.Join(problems...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Client reads people from directory's identity API (/internal/v1). It does not cache; wrap it
// in a Cache.
type Client struct {
	base   *url.URL
	key    string
	client *http.Client
}

// NewClient validates cfg and returns a Client.
func NewClient(cfg Config) (*Client, error) {
	base, err := baseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	if len(cfg.Key) < access.MinInternalKeyLength {
		return nil, fmt.Errorf("identity: key must have at least %d characters", access.MinInternalKeyLength)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	// The key never follows a redirect.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, key: cfg.Key, client: &noRedirect}, nil
}

func baseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, fmt.Errorf("identity: %q must be http(s)://host[:port] without path", raw)
	}
	u.Path = ""
	return u, nil
}

// ByEmail returns the person with that login e-mail (directory compares case-insensitively).
func (c *Client) ByEmail(ctx context.Context, email string) (Person, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 {
		return Person{}, ErrUnknown
	}
	return c.person(ctx, "/internal/v1/people/by-email?email="+url.QueryEscape(email))
}

// ByID returns the person with that id.
func (c *Client) ByID(ctx context.Context, id string) (Person, error) {
	if !idPattern.MatchString(id) {
		return Person{}, ErrUnknown
	}
	return c.person(ctx, "/internal/v1/people/"+url.PathEscape(id))
}

// People returns everyone directory grants this app, kid profiles included, for apps that
// need the whole list (a service whose callers name people, a picker). Each has a Role.
func (c *Client) People(ctx context.Context) ([]Person, error) {
	body, err := c.get(ctx, "/internal/v1/people", maxListBytes)
	if err != nil {
		return nil, err
	}
	var list []wire
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	people := make([]Person, 0, len(list))
	for _, w := range list {
		p, err := w.person()
		if errors.Is(err, ErrUnknown) {
			continue
		}
		if err != nil {
			return nil, err
		}
		people = append(people, p)
	}
	return people, nil
}

// wire is directory's answer: roles holds the calling app's role only.
type wire struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Group      string            `json:"group"`
	Emails     []string          `json:"emails"`
	PictureURL string            `json:"pictureUrl"`
	Kid        bool              `json:"kid"`
	Roles      map[string]string `json:"roles"`
	ActsFor    []string          `json:"actsFor"`
	Spaces     []Space           `json:"spaces"`
	Via        string            `json:"via"`
}

func (c *Client) person(ctx context.Context, path string) (Person, error) {
	body, err := c.get(ctx, path, maxPersonBytes)
	if err != nil {
		return Person{}, err
	}
	var w wire
	if err := json.Unmarshal(body, &w); err != nil {
		return Person{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return w.person()
}

func (w wire) person() (Person, error) {
	if !idPattern.MatchString(w.ID) {
		return Person{}, fmt.Errorf("%w: answer without a valid id", ErrUnavailable)
	}
	// Exactly one role, the calling app's; anything else is no grant (fail closed).
	var role string
	if len(w.Roles) == 1 {
		for _, r := range w.Roles {
			role = r
		}
	}
	if role == "" {
		return Person{}, ErrUnknown
	}
	p := Person{ID: w.ID, Name: w.Name, Group: w.Group, Emails: w.Emails, PictureURL: w.PictureURL, Kid: w.Kid, Role: role, ActsFor: w.ActsFor}
	if p.Emails == nil {
		p.Emails = []string{}
	}
	if p.ActsFor == nil {
		p.ActsFor = []string{}
	}
	p.Via = w.Via
	p.Spaces = normalizeSpaces(p.ID, w.Spaces)
	return p, nil
}

// App is one of a person's other apps, for the app menu.
type App struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color,omitempty"`
}

// Apps returns the other apps the person with id may open (not this one). An older directory
// without the list answers ErrUnknown.
func (c *Client) Apps(ctx context.Context, id string) ([]App, error) {
	if !idPattern.MatchString(id) {
		return nil, ErrUnknown
	}
	body, err := c.get(ctx, "/internal/v1/people/"+id+"/apps", maxListBytes)
	if err != nil {
		return nil, err
	}
	var apps []App
	if err := json.Unmarshal(body, &apps); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	out := make([]App, 0, len(apps))
	for _, a := range apps {
		if u, err := url.Parse(a.URL); err == nil && u.Scheme == "https" && u.Host != "" && a.Name != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

// Photo returns p's uploaded photo (JPEG); ErrUnknown when p has none. Serve it to the browser
// from an app route with "Cache-Control: private".
func (c *Client) Photo(ctx context.Context, p Person) ([]byte, error) {
	if p.PictureURL == "" {
		return nil, ErrUnknown
	}
	ref, err := url.Parse(p.PictureURL)
	if err != nil || ref.IsAbs() || ref.Host != "" || !strings.HasPrefix(ref.Path, "/internal/v1/people/") {
		return nil, ErrUnknown
	}
	return c.get(ctx, ref.RequestURI(), maxPhotoBytes)
}

func (c *Client) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+path, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, ErrUnknown
	case resp.StatusCode != http.StatusOK:
		// 401 means directory does not accept this app's key: a deployment fault, not a person.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("%w: directory answered %d", ErrUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: answer larger than %d bytes", ErrUnavailable, limit)
	}
	return body, nil
}

// normalizeSpaces keeps the own space first and only well-formed family spaces; an older
// directory without spaces gives the own space only.
func normalizeSpaces(id string, in []Space) []Space {
	out := []Space{{Kind: SpacePerson, ID: id}}
	for _, s := range in {
		if s.Kind == SpaceFamily && idPattern.MatchString(s.ID) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

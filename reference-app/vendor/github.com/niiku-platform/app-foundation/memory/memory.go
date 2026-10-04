// Package memory provides the proven shared Memory service client used by first-party apps.
package memory

import (
	"bytes"
	"context"
	"database/sql"
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
)

const (
	DefaultTimeout   = 5 * time.Second
	MaxResponseBytes = int64(4 << 20)
)

var (
	ErrNotConfigured = errors.New("memory_not_configured")
	ErrUnavailable   = errors.New("memory_unavailable")
)

// HTTPError reports a non-success response without including private response content.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("memory_http_%d", e.StatusCode) }

// Note is the Memory service note wire shape.
type Note struct {
	ID          string   `json:"id"`
	Person      string   `json:"person"`
	Domain      string   `json:"domain"`
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Markdown    string   `json:"markdown"`
	Source      string   `json:"source"`
	Owner       string   `json:"owner"`
	Visibility  []string `json:"visibility"`
	Revision    int64    `json:"revision"`
	UpdatedAt   string   `json:"updatedAt"`
	FilePath    string   `json:"filePath,omitempty"`
	Frontmatter string   `json:"frontmatter,omitempty"`
}

// Person is the Memory service person wire shape.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Status summarizes local Memory configuration and outbox state.
type Status struct {
	Configured bool   `json:"configured"`
	Pending    int    `json:"pending"`
	Conflicts  int    `json:"conflicts"`
	LastSync   string `json:"lastSync"`
}

// Config contains transport configuration and the app identity attached to queued writes.
type Config struct {
	BaseURL    string
	APIKey     string
	App        string
	HTTPClient *http.Client
}

// Client keeps a local cache and durable outbox in the application-owned SQLite database.
type Client struct {
	db             *sql.DB
	base, key, app string
	http           *http.Client
	mu             sync.Mutex
}

// New initializes the shared Memory cache/outbox tables. An empty base URL or key creates a
// disabled client so ordinary application behavior can remain available without Memory.
func New(db *sql.DB, config Config) (*Client, error) {
	if db == nil {
		return nil, errors.New("memory database is nil")
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS memory_cache(key TEXT PRIMARY KEY,data TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS memory_pending(key TEXT PRIMARY KEY,method TEXT NOT NULL,path TEXT NOT NULL,data TEXT NOT NULL,conflict INTEGER NOT NULL DEFAULT 0);`); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: DefaultTimeout}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		if copy.Timeout <= 0 || copy.Timeout > DefaultTimeout {
			copy.Timeout = DefaultTimeout
		}
		client = &copy
	}
	return &Client{
		db: db, base: strings.TrimRight(config.BaseURL, "/"), key: config.APIKey,
		app: config.App, http: client,
	}, nil
}

// Enabled reports whether remote Memory calls are configured.
func (c *Client) Enabled() bool {
	return c != nil && c.base != "" && c.key != ""
}

func (c *Client) call(ctx context.Context, method, path string, body, target any) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return &HTTPError{StatusCode: response.StatusCode}
	}
	if target == nil {
		return nil
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return ErrUnavailable
	}
	if int64(len(bodyBytes)) > MaxResponseBytes {
		return errors.New("memory_response_too_large")
	}
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("memory_response_invalid")
	}
	return nil
}

func (c *Client) cached(key string, target any) bool {
	var encoded string
	return c.db.QueryRow("SELECT data FROM memory_cache WHERE key=?", key).Scan(&encoded) == nil && json.Unmarshal([]byte(encoded), target) == nil
}

func (c *Client) cache(key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = c.db.Exec("INSERT INTO memory_cache VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET data=excluded.data,updated_at=excluded.updated_at", key, string(encoded), time.Now().UTC().Format(time.RFC3339))
	return err
}

// Notes returns notes for a domain, falling back to cache on transport and 5xx failures while
// overlaying the local outbox. Authorization failures never use cached private data.
func (c *Client) Notes(ctx context.Context, domain string) ([]Note, error) {
	out := struct {
		Notes []Note `json:"notes"`
	}{Notes: []Note{}}
	requestPath := "/v1/notes?domain=" + url.QueryEscape(domain)
	err := c.call(ctx, http.MethodGet, requestPath, nil, &out)
	if err == nil {
		_ = c.cache(requestPath, out)
	} else if isAuthorizationError(err) || !c.cached(requestPath, &out) {
		return nil, err
	}
	rows, err := c.db.Query("SELECT method,data FROM memory_pending WHERE path LIKE '/v1/notes/%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var method, encoded string
		if err := rows.Scan(&method, &encoded); err != nil {
			return nil, err
		}
		var note Note
		if json.Unmarshal([]byte(encoded), &note) != nil || note.Domain != domain {
			continue
		}
		index := -1
		for i, old := range out.Notes {
			if old.ID == note.ID {
				index = i
				break
			}
		}
		if method == http.MethodDelete {
			if index >= 0 {
				out.Notes = append(out.Notes[:index], out.Notes[index+1:]...)
			}
			continue
		}
		if index >= 0 {
			out.Notes[index] = note
		} else {
			out.Notes = append(out.Notes, note)
		}
	}
	return out.Notes, rows.Err()
}

// People returns configured people, falling back to cache except on authorization failures.
func (c *Client) People(ctx context.Context) ([]Person, error) {
	out := struct {
		People []Person `json:"people"`
	}{People: []Person{}}
	err := c.call(ctx, http.MethodGet, "/v1/people", nil, &out)
	if err == nil {
		_ = c.cache("people", out)
	} else if isAuthorizationError(err) || !c.cached("people", &out) {
		return nil, err
	}
	return out.People, nil
}

// QueueNote adds or replaces one pending note write.
func (c *Client) QueueNote(note Note) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	note.Owner = c.app
	if len(note.Visibility) == 0 {
		note.Visibility = []string{c.app}
	}
	encoded, err := json.Marshal(note)
	if err != nil {
		return err
	}
	_, err = c.db.Exec("INSERT INTO memory_pending(key,method,path,data,conflict) VALUES(?,'PUT',?,?,0) ON CONFLICT(key) DO UPDATE SET method=excluded.method,path=excluded.path,data=excluded.data,conflict=0", note.ID, "/v1/notes/"+url.PathEscape(note.ID), string(encoded))
	return err
}

// QueueDelete adds or replaces one pending note deletion.
func (c *Client) QueueDelete(note Note) error {
	encoded, err := json.Marshal(note)
	if err != nil {
		return err
	}
	_, err = c.db.Exec("INSERT INTO memory_pending(key,method,path,data,conflict) VALUES(?,'DELETE',?,?,0) ON CONFLICT(key) DO UPDATE SET method=excluded.method,path=excluded.path,data=excluded.data,conflict=0", note.ID, "/v1/notes/"+url.PathEscape(note.ID), string(encoded))
	return err
}

// RenamePerson queues a person rename.
func (c *Client) RenamePerson(id, name string) error {
	encoded, err := json.Marshal(Person{ID: id, Name: name})
	if err != nil {
		return err
	}
	_, err = c.db.Exec("INSERT INTO memory_pending(key,method,path,data,conflict) VALUES(?,'PUT',?,?,0) ON CONFLICT(key) DO UPDATE SET method=excluded.method,path=excluded.path,data=excluded.data,conflict=0", "person:"+id, "/v1/people/"+url.PathEscape(id), string(encoded))
	return err
}

// Flush serially sends up to 50 pending writes. Conflict rows remain durable for explicit review.
func (c *Client) Flush(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	type pending struct{ key, method, path, data string }
	rows, err := c.db.Query("SELECT key,method,path,data FROM memory_pending WHERE conflict=0 ORDER BY key LIMIT 50")
	if err != nil {
		return err
	}
	list := []pending{}
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.key, &item.method, &item.path, &item.data); err != nil {
			rows.Close()
			return err
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range list {
		var value any
		if err := json.Unmarshal([]byte(item.data), &value); err != nil {
			return err
		}
		var saved Note
		var target any
		if item.method == http.MethodPut && strings.HasPrefix(item.path, "/v1/notes/") {
			target = &saved
		}
		err = c.call(ctx, item.method, item.path, value, target)
		if err != nil {
			var httpError *HTTPError
			if errors.As(err, &httpError) && httpError.StatusCode == http.StatusConflict {
				var wanted, current Note
				_ = json.Unmarshal([]byte(item.data), &wanted)
				if item.method == http.MethodPut && strings.HasPrefix(item.path, "/v1/notes/") && c.call(ctx, http.MethodGet, item.path, nil, &current) == nil && sameNote(wanted, current) {
					saved = current
					err = nil
				} else {
					_, _ = c.db.Exec("UPDATE memory_pending SET conflict=1 WHERE key=? AND data=?", item.key, item.data)
					continue
				}
			}
			if err != nil {
				return err
			}
		}
		if saved.ID != "" {
			var newer string
			if c.db.QueryRow("SELECT data FROM memory_pending WHERE key=?", item.key).Scan(&newer) == nil && newer != item.data {
				var note Note
				if json.Unmarshal([]byte(newer), &note) == nil && note.Revision == saved.Revision-1 {
					note.Revision = saved.Revision
					encoded, _ := json.Marshal(note)
					_, _ = c.db.Exec("UPDATE memory_pending SET data=? WHERE key=? AND data=?", string(encoded), item.key, newer)
				}
			}
		}
		if _, err = c.db.Exec("DELETE FROM memory_pending WHERE key=? AND data=?", item.key, item.data); err != nil {
			return err
		}
	}
	return c.cache("lastSync", time.Now().UTC().Format(time.RFC3339))
}

// Status reports local configuration, queue, conflict, and last-sync state.
func (c *Client) Status() Status {
	value := Status{Configured: c.Enabled()}
	if c == nil {
		return value
	}
	_ = c.db.QueryRow("SELECT COUNT(*) FROM memory_pending").Scan(&value.Pending)
	_ = c.db.QueryRow("SELECT COUNT(*) FROM memory_pending WHERE conflict=1").Scan(&value.Conflicts)
	_ = c.cached("lastSync", &value.LastSync)
	return value
}

// DiscardConflicts removes pending writes already marked as conflicts.
func (c *Client) DiscardConflicts() error {
	if c == nil {
		return nil
	}
	_, err := c.db.Exec("DELETE FROM memory_pending WHERE conflict=1")
	return err
}

// Find returns the note matching a person/key pair.
func Find(notes []Note, person, key string) (Note, bool) {
	for _, note := range notes {
		if note.Person == person && note.Key == key {
			return note, true
		}
	}
	return Note{}, false
}

// Summary bounds a generated summary to the most recent 20 lines and about 12 KiB.
func Summary(header string, lines []string) string {
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	for len(lines) > 0 && len(header)+len(strings.Join(lines, "\n")) > 12000 {
		lines = lines[1:]
	}
	return header + strings.Join(lines, "\n")
}

func sameNote(left, right Note) bool {
	leftVisibility := append([]string(nil), left.Visibility...)
	rightVisibility := append([]string(nil), right.Visibility...)
	slices.Sort(leftVisibility)
	slices.Sort(rightVisibility)
	return left.ID == right.ID && left.Person == right.Person && left.Domain == right.Domain && left.Key == right.Key && left.Title == right.Title && left.Markdown == right.Markdown && left.Source == right.Source && slices.Equal(leftVisibility, rightVisibility)
}

func isAuthorizationError(err error) bool {
	var httpError *HTTPError
	return errors.As(err, &httpError) && (httpError.StatusCode == http.StatusUnauthorized || httpError.StatusCode == http.StatusForbidden)
}

// Package identitytest is an in-memory directory for tests of apps that use identity.
package identitytest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/platform-upstream/app-foundation/identity"
)

// Directory is an identity.Resolver over a fixed set of people. A person without Role counts as
// not granted (ErrUnknown), as in directory.
type Directory struct {
	mu          sync.Mutex
	people      map[string]identity.Person
	unavailable bool
	calls       int
}

// New returns a Directory holding people.
func New(people ...identity.Person) *Directory {
	d := &Directory{people: map[string]identity.Person{}}
	for _, p := range people {
		d.Set(p)
	}
	return d
}

// Set adds or replaces a person.
func (d *Directory) Set(p identity.Person) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p.Emails == nil {
		p.Emails = []string{}
	}
	if p.ActsFor == nil {
		p.ActsFor = []string{}
	}
	if len(p.Spaces) == 0 || p.Spaces[0] != (identity.Space{Kind: identity.SpacePerson, ID: p.ID}) {
		p.Spaces = append([]identity.Space{{Kind: identity.SpacePerson, ID: p.ID}}, p.Spaces...)
	}
	d.people[p.ID] = p
}

// Remove drops the person with id.
func (d *Directory) Remove(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.people, id)
}

// SetUnavailable makes every lookup fail with identity.ErrUnavailable (or work again).
func (d *Directory) SetUnavailable(down bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.unavailable = down
}

// Calls returns how many lookups reached the Directory (to test a cache in front of it).
func (d *Directory) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// ByEmail implements identity.Resolver, case-insensitively like directory.
func (d *Directory) ByEmail(_ context.Context, email string) (identity.Person, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.unavailable {
		return identity.Person{}, fmt.Errorf("%w: identitytest", identity.ErrUnavailable)
	}
	for _, p := range d.people {
		for _, e := range p.Emails {
			if strings.EqualFold(e, strings.TrimSpace(email)) {
				return granted(p)
			}
		}
	}
	return identity.Person{}, identity.ErrUnknown
}

// ByID implements identity.Resolver.
func (d *Directory) ByID(_ context.Context, id string) (identity.Person, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.unavailable {
		return identity.Person{}, fmt.Errorf("%w: identitytest", identity.ErrUnavailable)
	}
	p, ok := d.people[id]
	if !ok {
		return identity.Person{}, identity.ErrUnknown
	}
	return granted(p)
}

// People returns everyone with a Role, sorted by id, like identity.Client.People.
func (d *Directory) People(_ context.Context) ([]identity.Person, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.unavailable {
		return nil, fmt.Errorf("%w: identitytest", identity.ErrUnavailable)
	}
	out := []identity.Person{}
	for _, p := range d.people {
		if g, err := granted(p); err == nil {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func granted(p identity.Person) (identity.Person, error) {
	if p.Role == "" {
		return identity.Person{}, identity.ErrUnknown
	}
	p.Emails = append([]string{}, p.Emails...)
	p.ActsFor = append([]string{}, p.ActsFor...)
	return p, nil
}

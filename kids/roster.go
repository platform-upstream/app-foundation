package kids

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Kid is a kid profile granted the app, as a Roster hands it to the app.
type Kid struct {
	ID   string
	Name string
	// Families are the families the kid belongs to.
	Families []string
	// PhotoVersion is the version of the kid's photo, "" without one.
	PhotoVersion string
	// Trusted kids may take over one of the app's own kids from before directory by name: an
	// owner acts for them and none of their families is run by a self-registered adult. A kid
	// of a self-registered family never is.
	Trusted bool
}

// Roster keeps the kids granted the app, their families and those families' admins, refreshed
// by Sync (Run: once a minute is usual). Until the first sync it knows nothing (Kids ok false).
// A nil *Roster is off: the app keeps its own kid list.
type Roster struct {
	c *Client

	mu       sync.RWMutex
	ok       bool
	ids      map[string]bool
	families map[string][]string // kid id -> family ids
	admins   map[string][]string // family id -> admin ids
	photos   map[string]string   // kid id -> photo version
	owners   []string            // the owners among the people granted the app
}

// NewRoster returns a roster read through c.
func NewRoster(c *Client) *Roster { return &Roster{c: c} }

// Enabled reports whether the app takes its kids from directory.
func (r *Roster) Enabled() bool { return r != nil && r.c != nil }

// Client is the client the roster reads through.
func (r *Roster) Client() *Client { return r.c }

// Sync reads the people granted the app and its families once, hands the kids to link (the app
// links them to its own kid profiles) and makes them the current list. A failed read or link
// keeps the last list.
func (r *Roster) Sync(ctx context.Context, link func(context.Context, []Kid) error) error {
	people, err := r.c.People(ctx)
	if err != nil {
		return err
	}
	families, err := r.c.Families(ctx)
	if err != nil {
		return err
	}
	self := map[string]bool{}
	ownerActs := map[string]bool{}
	var owners []string
	for _, p := range people {
		if p.Kid || p.ID == "" {
			continue
		}
		self[p.ID] = p.SelfRegistered
		r.c.remember(p.ID, p.SelfRegistered)
		if p.Group == "owners" {
			owners = append(owners, p.ID)
			for _, k := range p.ActsFor {
				ownerActs[k] = true
			}
		}
	}
	admins := map[string][]string{}
	selfFamily := map[string]bool{}
	for _, f := range families {
		admins[f.ID] = f.Admins
		for _, a := range f.Admins {
			if self[a] {
				selfFamily[f.ID] = true
			}
		}
	}
	kids := []Kid{}
	ids := map[string]bool{}
	kidFamilies := map[string][]string{}
	photos := map[string]string{}
	for _, p := range people {
		if !p.Kid || p.ID == "" {
			continue
		}
		trusted := ownerActs[p.ID]
		for _, f := range p.Families {
			if selfFamily[f] {
				trusted = false
			}
		}
		k := Kid{ID: p.ID, Name: p.Name, Families: p.Families, PhotoVersion: p.PhotoVersion(), Trusted: trusted}
		kids = append(kids, k)
		ids[k.ID] = true
		kidFamilies[k.ID] = k.Families
		photos[k.ID] = k.PhotoVersion
	}
	if link != nil {
		if err := link(ctx, kids); err != nil {
			return fmt.Errorf("kids: link: %w", err)
		}
	}
	r.mu.Lock()
	r.ok, r.ids, r.families, r.admins, r.photos, r.owners = true, ids, kidFamilies, admins, photos, owners
	r.mu.Unlock()
	return nil
}

// Run syncs every interval until ctx ends; a failed sync is logged and keeps the last list.
// after (optional) runs once, after the first successful sync.
func (r *Roster) Run(ctx context.Context, interval time.Duration, link func(context.Context, []Kid) error, after func(context.Context)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := r.Sync(ctx, link); err != nil {
			if ctx.Err() == nil {
				slog.Warn("kids: directory sync failed", "error", err)
			}
		} else if after != nil {
			after(ctx)
			after = nil
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Kids returns the ids of the kids granted the app; ok is false until the first sync.
func (r *Roster) Kids() (map[string]bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ids, r.ok
}

// KidFamilies returns the families a kid belongs to.
func (r *Roster) KidFamilies(kid string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.families[kid])
}

// Admins returns the admins of a family.
func (r *Roster) Admins(family string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.admins[family])
}

// IsAdmin reports whether person administers family.
func (r *Roster) IsAdmin(family, person string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Contains(r.admins[family], person)
}

// PhotoVersion is the version of a kid's photo ("" = none or unknown).
func (r *Roster) PhotoVersion(kid string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.photos[kid]
}

// Owners are the owners among the people granted the app (last sync).
func (r *Roster) Owners() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.owners)
}

// Learn adds or updates a kid the app just created or renamed, so it shows before the next sync.
func (r *Roster) Learn(p Person) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ids == nil {
		r.ids, r.families, r.photos = map[string]bool{}, map[string][]string{}, map[string]string{}
	}
	r.ids[p.ID] = true
	if len(p.Families) > 0 {
		r.families[p.ID] = p.Families
	}
	r.photos[p.ID] = p.PhotoVersion()
}

// Forget drops a deleted kid.
func (r *Roster) Forget(kid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ids, kid)
	delete(r.families, kid)
	delete(r.photos, kid)
}

// PhotoChanged gives a kid's photo a new version after an upload ("" after a removal), so photo
// URLs change before the next sync.
func (r *Roster) PhotoChanged(kid string, removed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.photos == nil {
		r.photos = map[string]string{}
	}
	if removed {
		r.photos[kid] = ""
		return
	}
	r.photos[kid] = fmt.Sprint(time.Now().UnixMilli())
}

// KnowFamily reads a family the roster does not know yet (one that signed up after the last
// sync), so its admin can add kids at once.
func (r *Roster) KnowFamily(ctx context.Context, id string) error {
	r.mu.RLock()
	_, known := r.admins[id]
	r.mu.RUnlock()
	if known {
		return nil
	}
	f, err := r.c.Family(ctx, id)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.admins == nil {
		r.admins = map[string][]string{}
	}
	r.admins[f.ID] = f.Admins
	r.mu.Unlock()
	return nil
}

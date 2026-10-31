package identity_test

import (
	"log"
	"net/http"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/identity"
)

// An app resolves every signed-in request to a directory person and keys its data by Person.ID.
// The chart sets DIRECTORY_URL and DIRECTORY_KEY; in local mode the preview acts as a fixed
// person instead (directory is not reachable from a workstation).
func Example() {
	authConfig, err := access.ConfigFromEnv("127.0.0.1:8150")
	if err != nil {
		log.Fatal(err)
	}
	auth, err := access.New(authConfig)
	if err != nil {
		log.Fatal(err)
	}
	var resolver identity.Resolver
	var opts identity.Options
	if authConfig.Mode == access.ModeLocal {
		opts.Local = &identity.Person{ID: "local", Name: "Local preview", Role: "owner"}
	} else {
		cfg, err := identity.ConfigFromEnv()
		if err != nil {
			log.Fatal(err)
		}
		client, err := identity.NewClient(cfg)
		if err != nil {
			log.Fatal(err)
		}
		resolver = identity.NewCache(client, identity.CacheOptions{})
	}

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		person, _ := identity.From(r.Context()) // whose data: the caller, or the kid they act for
		_, _ = w.Write([]byte(person.ID))
	})
	// access first (proves the login), then identity (who it is, fail closed).
	handler := auth.Middleware(nil)(identity.Middleware(resolver, opts)(api))
	_ = handler
}

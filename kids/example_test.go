package kids_test

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/platform-upstream/app-foundation/identity"
	"github.com/platform-upstream/app-foundation/kids"
)

// A game keeps its own settings per kid and links them to directory's kid profiles once a minute;
// a family admin adds a kid from the game, which proxies the call because directory's /internal
// routes answer pods only.
func Example() {
	cfg, err := identity.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client, err := kids.New(cfg, "game")
	if err != nil {
		log.Fatal(err)
	}
	roster := kids.NewRoster(client)
	go roster.Run(context.Background(), time.Minute, func(ctx context.Context, list []kids.Kid) error {
		// Link list to the game's own kid rows (by id; by name only for Trusted kids).
		return nil
	}, nil)

	http.HandleFunc("POST /api/kids", func(w http.ResponseWriter, r *http.Request) {
		actor, _ := identity.ActorFrom(r.Context())
		family, err := client.AdminFamily(r.Context(), actor)
		if err != nil || family == "" {
			http.Error(w, "only a family admin adds kids", http.StatusForbidden)
			return
		}
		kid, err := client.CreateKid(r.Context(), family, actor.ID, r.FormValue("name"))
		switch {
		case errors.Is(err, kids.ErrConflict):
			http.Error(w, "the family is full", http.StatusConflict)
		case kids.Status(err) != 0:
			http.Error(w, "directory refused", http.StatusBadRequest)
		case err != nil:
			http.Error(w, "directory unavailable", http.StatusServiceUnavailable)
		default:
			roster.Learn(kid)
		}
	})
}

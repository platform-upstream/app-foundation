package googleauth_test

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/platform-upstream/app-foundation/googleauth"
	"github.com/platform-upstream/app-foundation/identity"
)

// store is the app's own SQL: an oauth_states table and a table of grants with the three sealed
// columns, the account e-mail and the scopes, keyed by the person.
type store interface {
	SaveState(ctx context.Context, state, person string, expires time.Time) error
	// TakeState deletes the state and reports whether it was unexpired and that person's.
	TakeState(ctx context.Context, state, person string) (bool, error)
	SaveGrant(ctx context.Context, person, email string, token googleauth.Sealed, scopes []string) error
	Grant(ctx context.Context, person string) (email string, token googleauth.Sealed, err error)
	// Forget drops the person's token (disconnect, or Google refused it); it never revokes.
	Forget(ctx context.Context, person string) error
}

// An app connects each person's Google account with its own grant. The routes sit behind access
// and identity, so the callback knows who returns from Google.
func Example() {
	key, err := googleauth.ParseKey(os.Getenv("APP_GOOGLE_TOKEN_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	google, err := googleauth.New(googleauth.Config{
		ClientID:     os.Getenv("APP_GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("APP_GOOGLE_CLIENT_SECRET"),
		RedirectURL:  "https://app.example.org/api/google/callback",
		Scopes:       []string{"https://www.googleapis.com/auth/calendar.app.created"},
	})
	if err != nil {
		log.Fatal(err)
	}
	var db store

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/google/connect", func(w http.ResponseWriter, r *http.Request) {
		person, _ := identity.From(r.Context())
		state := googleauth.NewState()
		if err := db.SaveState(r.Context(), state, person.ID, time.Now().Add(googleauth.StateTTL)); err != nil {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		hint := ""
		if len(person.Emails) > 0 {
			hint = person.Emails[0]
		}
		http.Redirect(w, r, google.AuthCodeURL(state, hint), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /api/google/callback", func(w http.ResponseWriter, r *http.Request) {
		person, _ := identity.From(r.Context())
		q := r.URL.Query()
		if ok, err := db.TakeState(r.Context(), q.Get("state"), person.ID); err != nil || !ok {
			http.Redirect(w, r, "/settings?google=expired", http.StatusSeeOther)
			return
		}
		if q.Get("error") != "" { // access_denied: the person cancelled
			http.Redirect(w, r, "/settings?google=cancelled", http.StatusSeeOther)
			return
		}
		grant, err := google.Exchange(r.Context(), q.Get("code"))
		if errors.Is(err, googleauth.ErrMissingScopes) {
			http.Redirect(w, r, "/settings?google=scopes", http.StatusSeeOther)
			return
		}
		if err != nil {
			http.Redirect(w, r, "/settings?google=failed", http.StatusSeeOther)
			return
		}
		sealed := key.Seal(grant.RefreshToken, grant.Email)
		if err := db.SaveGrant(r.Context(), person.ID, grant.Email, sealed, grant.Scopes); err != nil {
			http.Redirect(w, r, "/settings?google=failed", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/settings?google=connected", http.StatusSeeOther)
	})

	// Before each Google call: the person's access token, or "connect again".
	authorize := func(ctx context.Context, person string, req *http.Request) error {
		email, sealed, err := db.Grant(ctx, person)
		if err != nil {
			return err
		}
		refresh, err := key.Open(sealed, email)
		if err != nil {
			return err // another key or a damaged row: the person connects again as well
		}
		token, err := google.AccessToken(ctx, refresh)
		if errors.Is(err, googleauth.ErrInvalidGrant) {
			_ = db.Forget(ctx, person) // the settings screen now offers "connect again"
		}
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	_, _ = mux, authorize
}

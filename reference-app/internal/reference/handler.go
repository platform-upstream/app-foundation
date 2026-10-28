package reference

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/castillo-platform/app-foundation/access"
	"github.com/castillo-platform/app-foundation/httpx"
	"github.com/castillo-platform/app-foundation/memory"
	"github.com/castillo-platform/app-foundation/server"
	foundationweb "github.com/castillo-platform/app-foundation/web"
)

type MemoryClient interface {
	Enabled() bool
	Status() memory.Status
	People(context.Context) ([]memory.Person, error)
}

type HandlerConfig struct {
	Database       *sql.DB
	Assets         fs.FS
	Authenticator  *access.Authenticator
	AllowedOrigins []string
	Memory         MemoryClient
	LLM            CompletionClient
}

func NewHandler(config HandlerConfig) (http.Handler, error) {
	if config.Database == nil || config.Assets == nil || config.Authenticator == nil || config.Memory == nil || config.LLM == nil {
		return nil, errors.New("reference handler dependencies are required")
	}
	frontend, err := foundationweb.Handler(config.Assets)
	if err != nil {
		return nil, err
	}
	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		items, err := ListItems(r.Context(), config.Database)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "State is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Items      []Item        `json:"items"`
			Memory     memory.Status `json:"memory"`
			LLMEnabled bool          `json:"llmEnabled"`
		}{Items: items, Memory: config.Memory.Status(), LLMEnabled: config.LLM.Enabled()})
	})
	api.HandleFunc("POST /api/items", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name string `json:"name"`
		}
		if err := httpx.DecodeJSON(w, r, &input, httpx.DefaultJSONBodyLimit); err != nil {
			writeError(w, httpx.StatusCode(err), "A bounded JSON item is required.")
			return
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || len(input.Name) > 120 {
			writeError(w, http.StatusBadRequest, "Name must contain 1–120 bytes.")
			return
		}
		item, err := AddItem(r.Context(), config.Database, input.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Item could not be saved.")
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			Item Item `json:"item"`
		}{Item: item})
	})
	api.HandleFunc("GET /api/memory/people", func(w http.ResponseWriter, r *http.Request) {
		people, err := config.Memory.People(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "Memory is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			People []memory.Person `json:"people"`
		}{People: people})
	})
	api.HandleFunc("POST /api/complete", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Prompt string `json:"prompt"`
		}
		if err := httpx.DecodeJSON(w, r, &input, httpx.DefaultJSONBodyLimit); err != nil {
			writeError(w, httpx.StatusCode(err), "A bounded JSON prompt is required.")
			return
		}
		input.Prompt = strings.TrimSpace(input.Prompt)
		if input.Prompt == "" || len(input.Prompt) > 1000 {
			writeError(w, http.StatusBadRequest, "Prompt must contain 1–1000 bytes.")
			return
		}
		completion, err := config.LLM.Complete(r.Context(), input.Prompt)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "Local inference is unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Completion string `json:"completion"`
		}{Completion: completion})
	})
	api.HandleFunc("POST /api/wait", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Milliseconds int `json:"milliseconds"`
		}
		if err := httpx.DecodeJSON(w, r, &input, httpx.DefaultJSONBodyLimit); err != nil {
			writeError(w, httpx.StatusCode(err), "A bounded JSON delay is required.")
			return
		}
		if input.Milliseconds < 1 || input.Milliseconds > 3000 {
			writeError(w, http.StatusBadRequest, "Delay must be 1–3000 milliseconds.")
			return
		}
		timer := time.NewTimer(time.Duration(input.Milliseconds) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			writeJSON(w, http.StatusOK, map[string]bool{"completed": true})
		case <-r.Context().Done():
			return
		}
	})
	api.Handle("/api/", httpx.NotFound("API route not found."))
	apiWithPolicy := noStore(httpx.SameOrigin(config.AllowedOrigins, nil)(api))

	protectedMux := http.NewServeMux()
	protectedMux.Handle("/api/", apiWithPolicy)
	protectedMux.Handle("/", frontend)
	protected := config.Authenticator.Middleware(nil)(protectedMux)

	root := http.NewServeMux()
	root.Handle("GET /healthz", server.Health())
	root.Handle("/", protected)
	return securityHeaders(httpx.RequestIDs(root)), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	if err := httpx.WriteJSON(w, status, value); err != nil {
		http.Error(w, "response encoding failed", http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	_ = httpx.WriteError(w, status, message)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self'; manifest-src 'self'; script-src 'self'; style-src 'self'; worker-src 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

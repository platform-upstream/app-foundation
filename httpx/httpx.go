// Package httpx contains the small HTTP conventions shared by application handlers.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
)

const (
	RequestIDHeader      = "X-Request-ID"
	DefaultJSONBodyLimit = int64(32 << 10)
)

var (
	ErrInvalidJSON          = errors.New("invalid JSON body")
	ErrBodyTooLarge         = errors.New("request body too large")
	ErrUnsupportedMediaType = errors.New("application/json content type required")
	requestIDPattern        = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// OriginRejectFunc writes an application-owned same-origin failure response.
type OriginRejectFunc func(http.ResponseWriter, *http.Request)

// ErrorResponse preserves the existing application API error envelope. The message remains
// application-owned so localized apps can retain their copy.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WriteJSON marshals before committing headers so encoding failures cannot produce partial JSON.
func WriteJSON(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(append(body, '\n'))
	return err
}

// WriteError writes the common minimal error shape with application-owned text.
func WriteError(w http.ResponseWriter, status int, message string) error {
	return WriteJSON(w, status, ErrorResponse{Error: message})
}

// NotFound returns a JSON 404 handler with application-owned copy. Mount it at an API prefix
// before an embedded SPA fallback so unknown API routes cannot accidentally return HTML.
func NotFound(message string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = WriteError(w, http.StatusNotFound, message)
	})
}

// DecodeJSON strictly decodes one bounded JSON value. Unknown struct fields and trailing values
// are rejected. StatusCode maps its errors to the corresponding HTTP response status.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ErrUnsupportedMediaType
	}
	if maxBytes <= 0 {
		return fmt.Errorf("maxBytes must be positive")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("%w: %v", ErrBodyTooLarge, err)
		}
		var invalidTarget *json.InvalidUnmarshalError
		if errors.As(err, &invalidTarget) {
			return fmt.Errorf("decode JSON target: %w", err)
		}
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("%w: %v", ErrBodyTooLarge, err)
		}
		return fmt.Errorf("%w: trailing content", ErrInvalidJSON)
	}
	return nil
}

// StatusCode maps DecodeJSON errors to stable protocol status codes.
func StatusCode(err error) int {
	switch {
	case errors.Is(err, ErrUnsupportedMediaType):
		return http.StatusUnsupportedMediaType
	case errors.Is(err, ErrBodyTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrInvalidJSON):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// SameOrigin rejects cross-site mutations. allowedOrigins contains externally visible origins
// such as the production URL and the owner-gated development proxy. The request's direct origin
// is also accepted, which covers an ordinary same-origin loopback preview.
func SameOrigin(allowedOrigins []string, reject OriginRejectFunc) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin != "" {
			allowed[origin] = true
		}
	}
	if reject == nil {
		reject = func(w http.ResponseWriter, _ *http.Request) {
			_ = WriteError(w, http.StatusForbidden, "Invalid origin.")
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (origin != "" && origin != directOrigin(r) && !allowed[origin]) {
				reject(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func directOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type requestIDKey struct{}

// RequestIDs accepts a bounded safe upstream request ID or creates one, then exposes it through
// both response headers and request context. Applications opt in to this middleware when adding
// the response header is compatible with their existing HTTP contract.
func RequestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestID returns the request ID installed by RequestIDs.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(value[:])
}

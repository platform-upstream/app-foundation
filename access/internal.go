package access

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
)

// MinInternalKeyLength is the shortest bearer key Internal accepts; shorter keys are ignored.
const MinInternalKeyLength = 32

var (
	// ErrForwarded: the request carries a header the ingress, the tunnel or Cloudflare Access
	// adds, so it did not come straight from a pod in the cluster.
	ErrForwarded = errors.New("internal route reached through the ingress")
	// ErrInvalidKey: no single bearer key, or one that matches no caller.
	ErrInvalidKey = errors.New("internal route key is missing or invalid")
)

// forwardedHeaders are added by Traefik, cloudflared or Cloudflare Access. A direct in-cluster
// call carries none of them; their presence alone, with any value, refuses the request.
var forwardedHeaders = []string{
	"Forwarded",
	"X-Forwarded-For",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"X-Real-Ip",
	"Cf-Connecting-Ip",
	"Cf-Ray",
	AssertionHeader,
}

type internalCallerKey struct{}

// InternalCallerFrom returns the caller name Internal matched for this request.
func InternalCallerFrom(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(internalCallerKey{}).(string)
	return name, ok
}

// Internal guards the routes an application serves outside Cloudflare Access for the owner's
// other apps inside the cluster. keys maps a caller name to its bearer key; keys shorter than
// MinInternalKeyLength (an unset one included) are ignored, so a route without a usable key is
// closed. A request passes only when it carries none of the headers the ingress, the tunnel or
// Access add, and exactly one "Authorization: Bearer <key>" header whose key matches a caller's
// in constant time. The matched name is in the request context (InternalCallerFrom). Network
// policy deciding who may connect stays with the deployment; reject owns the response copy
// (nil writes 401 with {"error":"Unauthorized."}).
func Internal(keys map[string]string, reject RejectFunc) func(http.Handler) http.Handler {
	match, reject := keyMatcher(keys), rejectOr401(reject)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Forwarded(r) {
				reject(w, r, ErrForwarded)
				return
			}
			matched := match(r)
			if matched == "" {
				reject(w, r, ErrInvalidKey)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), internalCallerKey{}, matched)))
		})
	}
}

// Remote guards a route that callers outside the cluster reach through the tunnel, on a path the
// deployment bypasses in Cloudflare Access (an agent on a hosted runner, which can send only a
// static bearer key). The key check is Internal's; the ingress headers are allowed, so the key is
// the only gate and keys passed here must be ones meant to leave the cluster. The matched name is
// in the request context (RemoteCallerFrom), apart from Internal's, so a handler always knows
// which door a caller came through.
func Remote(keys map[string]string, reject RejectFunc) func(http.Handler) http.Handler {
	match, reject := keyMatcher(keys), rejectOr401(reject)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			matched := match(r)
			if matched == "" {
				reject(w, r, ErrInvalidKey)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), remoteCallerKey{}, matched)))
		})
	}
}

type remoteCallerKey struct{}

// RemoteCallerFrom returns the caller name Remote matched for this request.
func RemoteCallerFrom(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(remoteCallerKey{}).(string)
	return name, ok
}

// Forwarded reports whether the request carries a header the ingress, the tunnel or Access adds.
func Forwarded(r *http.Request) bool {
	for _, header := range forwardedHeaders {
		if len(r.Header.Values(header)) > 0 {
			return true
		}
	}
	return false
}

// keyMatcher returns the caller whose key the request's one bearer header carries, or "".
func keyMatcher(keys map[string]string) func(*http.Request) string {
	type entry struct {
		name string
		key  []byte
	}
	var callers []entry
	for name, key := range keys {
		if name != "" && len(key) >= MinInternalKeyLength {
			callers = append(callers, entry{name, []byte(key)})
		}
	}
	sort.Slice(callers, func(i, j int) bool { return callers[i].name < callers[j].name })
	return func(r *http.Request) string {
		supplied, ok := bearer(r)
		matched := ""
		if ok {
			// Every key is compared, so the time taken does not tell which caller nearly matched.
			for _, caller := range callers {
				if subtle.ConstantTimeCompare(supplied, caller.key) == 1 && matched == "" {
					matched = caller.name
				}
			}
		}
		return matched
	}
}

func rejectOr401(reject RejectFunc) RejectFunc {
	if reject != nil {
		return reject
	}
	return func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized."})
	}
}

// bearer returns the key of the one Authorization header, which must use the Bearer scheme.
func bearer(r *http.Request) ([]byte, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return nil, false
	}
	scheme, key, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || key == "" {
		return nil, false
	}
	return []byte(key), true
}

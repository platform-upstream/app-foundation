package web

import (
	"fmt"
	"net/http"
	"strings"
)

// DefaultCSP is the Content-Security-Policy Headers sends when HeaderPolicy.CSP is empty: same-origin
// everything, images also from data: and blob: URLs, no plugins, no foreign base URL, no framing
// (frame-ancestors follows HeaderPolicy.FrameOptions). It allows no inline script or style and no
// third-party origin; an app that needs one sets its own CSP.
const DefaultCSP = "default-src 'self'; img-src 'self' data: blob:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

// DefaultPermissionsPolicy turns off the powerful browser features an app rarely needs.
const DefaultPermissionsPolicy = "camera=(), geolocation=(), microphone=(), payment=(), usb=()"

// HeaderPolicy configures Headers. The zero value is the strict default.
type HeaderPolicy struct {
	// CSP is the app's Content-Security-Policy. Empty sends DefaultCSP, which blocks inline scripts
	// and styles and every third-party origin: check the app against it before relying on it.
	CSP string
	// FrameOptions is "DENY" (default) or "SAMEORIGIN". With SAMEORIGIN, the default CSP's
	// frame-ancestors is 'self'.
	FrameOptions string
	// NoHSTS leaves out Strict-Transport-Security (max-age one year, this host only). Browsers
	// ignore the header over plain HTTP, so local previews are unaffected either way.
	NoHSTS bool
	// PermissionsPolicy replaces DefaultPermissionsPolicy, e.g. "camera=(self)" for a scanner.
	PermissionsPolicy string
}

// Headers returns middleware that sets the security headers on every response: Content-Security-
// Policy, X-Content-Type-Options nosniff, Referrer-Policy same-origin, X-Frame-Options,
// Cross-Origin-Opener-Policy same-origin, Strict-Transport-Security and Permissions-Policy. Put it
// outermost, so probes, public files and authentication failures carry the headers too. A handler
// may still override one for its own response. An invalid policy panics at construction.
func Headers(policy HeaderPolicy) func(http.Handler) http.Handler {
	frame := strings.ToUpper(policy.FrameOptions)
	ancestors := "frame-ancestors 'none'"
	switch frame {
	case "", "DENY":
		frame = "DENY"
	case "SAMEORIGIN":
		ancestors = "frame-ancestors 'self'"
	default:
		panic(fmt.Sprintf("web.Headers: FrameOptions %q must be DENY or SAMEORIGIN", policy.FrameOptions))
	}
	csp := policy.CSP
	if csp == "" {
		csp = strings.Replace(DefaultCSP, "frame-ancestors 'none'", ancestors, 1)
	}
	permissions := policy.PermissionsPolicy
	if permissions == "" {
		permissions = DefaultPermissionsPolicy
	}
	for name, value := range map[string]string{"CSP": csp, "PermissionsPolicy": permissions} {
		if strings.ContainsAny(value, "\r\n\x00") {
			panic("web.Headers: " + name + " contains a line break")
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", frame)
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Permissions-Policy", permissions)
			if !policy.NoHSTS {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

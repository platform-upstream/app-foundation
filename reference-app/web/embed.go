// Package web embeds the built reference frontend into the Go binary.
package web

import "embed"

// Dist is populated by the frontend build before the Go build.
//
//go:embed all:dist
var Dist embed.FS

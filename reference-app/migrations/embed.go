// Package migrations owns the reference application's forward-only schema.
package migrations

import "embed"

// Files contains the application migrations, not foundation-owned schema.
//
//go:embed *.sql
var Files embed.FS

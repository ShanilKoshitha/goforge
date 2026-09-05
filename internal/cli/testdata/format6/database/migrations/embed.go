package migrations

import "embed"

// Files contains the application's plain SQL migration files.
//
//go:embed *.sql
var Files embed.FS

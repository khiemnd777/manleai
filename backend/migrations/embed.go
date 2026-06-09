package migrations

import "embed"

// Files contains SQL migrations applied by the API startup migrator.
//
//go:embed *.sql
var Files embed.FS

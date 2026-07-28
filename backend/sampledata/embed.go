package sampledata

import "embed"

// Files owns opt-in sample fixture migrations. It is intentionally separate
// from backend/migrations so startup and production-live schema migration can
// never create sample identities or tenants.
//
//go:embed migrations/*.sql
var Files embed.FS

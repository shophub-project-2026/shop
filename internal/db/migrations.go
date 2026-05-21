package db

import "embed"

// Migrations holds all *.sql migration files embedded at compile time.
// Import this package to get the canonical migration set.
//
//go:embed migrations/*.sql
var Migrations embed.FS

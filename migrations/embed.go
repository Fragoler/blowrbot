// Package migrations holds the database schema history. It lives at the repository
// root so that every package shares one migration set and one version counter.
package migrations

import "embed"

// FS carries the goose migrations compiled into the binary, so a deployed bot
// migrates itself without shipping the .sql files alongside it.
//
//go:embed *.sql
var FS embed.FS

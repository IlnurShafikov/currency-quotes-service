// Package migrations holds the SQL schema migrations of the service. They
// are embedded into the binary so that the service can migrate its own
// database on start-up and tests can build a schema without external tools.
package migrations

import "embed"

// FS contains the goose migration files.
//
//go:embed *.sql
var FS embed.FS

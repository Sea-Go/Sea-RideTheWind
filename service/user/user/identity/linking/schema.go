package linking

import _ "embed"

// MigrationSQL is applied explicitly to RTW's User Center PostgreSQL
// database before opt-in account-link routes are enabled.
//
//go:embed schema.sql
var MigrationSQL string

// Package db embeds the SQL migrations so the binary is self-contained.
package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS

// Package db embeds the database migrations so they always match the
// binary that runs them.
package db

import "embed"

// Migrations contains the SQL migrations for each supported database, in
// db_<database>/migrations.
//
//go:embed db_sqlite3/migrations/*.sql db_mysql/migrations/*.sql
var Migrations embed.FS

// MigrationsDir returns the directory within Migrations holding the
// migrations for the given goose dialect ("sqlite3" or "mysql").
func MigrationsDir(dialect string) string {
	return "db_" + dialect + "/migrations"
}

// Package testdb provides the database configuration used by Gophish's tests.
//
// By default, tests use a fresh in-memory sqlite3 database. To run them
// against MySQL instead, set GOPHISH_TEST_MYSQL_DSN to a DSN for a disposable
// database, for example:
//
//	GOPHISH_TEST_MYSQL_DSN='root:root@(127.0.0.1:3306)/gophish_test?charset=utf8&parseTime=True&loc=UTC' go test -p 1 ./...
//
// The database named in the DSN is dropped and recreated each time a test
// requests a config, so never point it at a database you want to keep. Since
// every package shares that one database, MySQL runs must use -p 1 so that
// packages run one at a time.
package testdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-sql-driver/mysql"
	"github.com/gophish/gophish/config"
)

// Fataler is implemented by *testing.T, *testing.B, and gocheck's *check.C.
type Fataler interface {
	Fatalf(format string, args ...interface{})
}

// MySQLDSNEnv is the environment variable that, when set, runs tests against
// the MySQL database in its DSN.
const MySQLDSNEnv = "GOPHISH_TEST_MYSQL_DSN"

// Config returns the configuration for a fresh, empty test database. root is
// the relative path from the calling package's directory to the repository
// root, used to locate the migrations.
func Config(f Fataler, root string) *config.Config {
	dsn := os.Getenv(MySQLDSNEnv)
	if dsn == "" {
		return &config.Config{
			DBName:         "sqlite3",
			DBPath:         ":memory:",
			MigrationsPath: filepath.Join(root, "db", "db_sqlite3", "migrations") + "/",
		}
	}
	if err := resetMySQL(dsn); err != nil {
		f.Fatalf("error resetting MySQL test database: %v", err)
	}
	return &config.Config{
		DBName:         "mysql",
		DBPath:         dsn,
		MigrationsPath: filepath.Join(root, "db", "db_mysql", "migrations") + "/",
	}
}

// resetMySQL drops and recreates the database named in the DSN.
func resetMySQL(dsn string) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	name := cfg.DBName
	if name == "" {
		return fmt.Errorf("%s must name a database", MySQLDSNEnv)
	}
	cfg.DBName = ""
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	quoted := "`" + name + "`"
	if _, err := db.Exec("DROP DATABASE IF EXISTS " + quoted); err != nil {
		return err
	}
	_, err = db.Exec("CREATE DATABASE " + quoted + " CHARACTER SET utf8mb4")
	return err
}

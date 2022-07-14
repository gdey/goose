package seed

import (
	"embed"
	"github.com/gdey/goose/v3"
)

//go:embed [0-9]*_*.*
var migrationsFS embed.FS

var Provider = goose.NewProvider(
	goose.ProviderPackage("seed", "Provider"),
	goose.Filesystem(migrationsFS),
	goose.Tablename("seed_db_version"),
	goose.Dialect(goose.DialectPostgres),
	goose.BaseDir(""), // use the directory this package is in
)

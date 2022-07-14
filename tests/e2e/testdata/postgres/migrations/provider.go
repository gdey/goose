package migrations

import (
	"embed"
	"github.com/gdey/goose/v3"
)

//go:embed [0-9]*_*.*
var migrationsFS embed.FS

var Provider = goose.NewProvider(
	goose.ProviderPackage("migrations", "Provider"),
	goose.Filesystem(migrationsFS),
	goose.Dialect(goose.DialectPostgres),
	goose.BaseDir(""), // use the directory this package is in
)

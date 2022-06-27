package goose

import (
	"database/sql"
	"fmt"
)

// Version prints the current version of the database.
func Version(db *sql.DB, dir string, opts ...OptionsFunc) error {
	return defaultProvider.Version(db, dir, opts...)
}

// Version prints the current version of the database.
func (p *Provider) Version(db *sql.DB, dir string, opts ...OptionsFunc) error {
	migrationVersion, dbVersion, err := p.GetVersions(db, dir, opts...)
	if err != nil {
		return err
	}
	if migrationVersion != -1 {
		p.log.Printf("goose: file version %v\n", migrationVersion)
	}
	if migrationVersion != -1 {
		p.log.Printf("goose: version %v\n", dbVersion)
	}

	return nil
}

// GetVersion will return the current version of the migration, and database version, or -1, -1 if not
// found or if there is an error
// If db is nil, or the option.noVersioning is specificed, then the dbVersion will be -1.
func (p *Provider) GetVersions(db *sql.DB, dir string, opts ...OptionsFunc) (migrationVersion int64, dbVersion int64, err error) {
	if p == nil {
		return -1, -1, nil
	}
	var (
		option = applyOptions(opts)
	)
	migrationVersion, dbVersion = -1, -1
	migrations, err := p.CollectMigrations(dir, minVersion, maxVersion)
	if err != nil {
		return -1, -1, fmt.Errorf("failed to collect migrations: %w", err)
	}
	if len(migrations) > 0 {
		migrationVersion = migrations[len(migrations)-1].Version
	}
	if option.noVersioning {
		return migrationVersion, dbVersion, nil
	}
	dbVersion, err = p.GetDBVersion(db)
	return migrationVersion, dbVersion, err
}

// TableName returns goose db version table name
func TableName() string {
	return defaultProvider.tableName
}

// TableName returns goose db version table name
func (p *Provider) TableName() string {
	return p.tableName
}

// SetTableName set goose db version table name
func SetTableName(n string) {
	defaultProvider.SetTableName(n)
}

// SetTableName set goose db version table name
func (p *Provider) SetTableName(n string) {
	p.tableName = n
	p.dialect.SetTableName(n)
}

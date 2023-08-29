package goose

import (
	"fmt"
	"strings"
)

type ErrUnwrap struct {
	Err error
}

func (err ErrUnwrap) Unwrap() error { return err.Err }

type MissingMigrations struct {
	Version int64
	Source  string
}

type MissingMigrationsErr struct {
	MissingMigrations []MissingMigrations
}

func (err MissingMigrationsErr) Error() string {
	var buff strings.Builder
	fmt.Fprintf(&buff, "err: found %d missing migrations:", len(err.MissingMigrations))
	for _, m := range err.MissingMigrations {
		fmt.Fprintf(&buff, "\n\tversion %d: %s", m.Version, m.Source)
	}
	return buff.String()
}

func MissingMigrationsErrFromMigrations(migrations Migrations) (err MissingMigrationsErr) {
	err.MissingMigrations = make([]MissingMigrations, len(migrations))
	for i, m := range migrations {
		err.MissingMigrations[i].Version, err.MissingMigrations[i].Source = m.Version, m.Source
	}
	return err
}

type ErrUnknownExtension struct {
	Extension string
}

func (err ErrUnknownExtension) Error() string {
	var str strings.Builder
	str.WriteString("unknown extension ")
	str.WriteString(err.Extension)
	return str.String()
}

type ErrMigrationSQLParse struct {
	Filename string
	Up       bool

	ErrUnwrap
}

func (err ErrMigrationSQLParse) Error() string {
	var str strings.Builder
	str.WriteString("Error ")
	str.WriteString(err.Filename)
	str.WriteString(": failed to parse SQL migration file: ")
	str.WriteString(err.Err.Error())
	return str.String()
}

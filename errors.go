package goose

import (
	"fmt"
	"strings"
)

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

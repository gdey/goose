package goose

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

type options struct {
	allowMissing     bool
	applyUpByOne     bool
	noVersioning     bool
	noOutput         bool
	eventsChannel    chan<- Eventer
	dontCloseChannel bool
}

func (o options) send(e Eventer) {
	if o.eventsChannel == nil {
		return
	}
	o.eventsChannel <- e
}

func (o options) shouldCloseEventsChannel() bool {
	return o.eventsChannel != nil && !o.dontCloseChannel
}

type OptionsFunc func(o *options)

func WithAllowMissing() OptionsFunc {
	return func(o *options) { o.allowMissing = true }
}

func WithNoVersioning() OptionsFunc {
	return func(o *options) { o.noVersioning = true }
}

func withApplyUpByOne() OptionsFunc {
	return func(o *options) { o.applyUpByOne = true }
}

func WithEvents(events chan<- Eventer, DontCloseChannelOnComplete bool) OptionsFunc {
	return func(o *options) {
		o.eventsChannel = events
		o.dontCloseChannel = DontCloseChannelOnComplete
	}
}

func WithNoOutput() OptionsFunc {
	return func(o *options) { o.noOutput = true }
}

func applyOptions(opts []OptionsFunc) *options {
	option := new(options)
	for _, f := range opts {
		f(option)
	}
	return option
}

// UpTo migrates up to a specific version.
func UpTo(db *sql.DB, dir string, version int64, opts ...OptionsFunc) error {
	return defaultProvider.UpTo(db, dir, version, opts...)
}

func (p *Provider) UpTo(db *sql.DB, dir string, version int64, opts ...OptionsFunc) (err error) {
	option := applyOptions(opts)
	foundMigrations, err := p.CollectMigrations(dir, minVersion, version)
	if err != nil {
		return err
	}

	if option.noVersioning {
		if len(foundMigrations) == 0 {
			return nil
		}
		if option.applyUpByOne {
			// For up-by-one this means keep re-applying the first
			// migration over and over.
			version = foundMigrations[0].Version
		}
		return p.upToNoVersioning(db, foundMigrations, version)
	}

	if _, err := p.EnsureDBVersion(db); err != nil {
		return err
	}
	//dbMigrations, err := listAllDBVersions(p.dialect, db)
	dbMigrations, err := listAllDBVersions(p.dialect, db)
	if err != nil {
		return err
	}

	missingMigrations := findMissingMigrations(dbMigrations, foundMigrations)

	// feature(mf): It is very possible someone may want to apply ONLY new migrations
	// and skip missing migrations altogether. At the moment this is not supported,
	// but leaving this comment because that's where that logic will be handled.
	if len(missingMigrations) > 0 && !option.allowMissing {
		var collected []string
		for _, m := range missingMigrations {
			output := fmt.Sprintf("version %d: %s", m.Version, m.Source)
			collected = append(collected, output)
		}
		return fmt.Errorf("error: found %d missing migrations:\n\t%s",
			len(missingMigrations), strings.Join(collected, "\n\t"))
	}

	if option.allowMissing {
		return p.upWithMissing(
			db,
			missingMigrations,
			foundMigrations,
			dbMigrations,
			option,
		)
	}

	var current int64
	for {
		current, err = p.GetDBVersion(db)
		if err != nil {
			return err
		}
		next, err := foundMigrations.Next(current)
		if err != nil {
			if errors.Is(err, ErrNoNextVersion) {
				break
			}
			return fmt.Errorf("failed to find next migration: %v", err)
		}
		if err := next.UpWithProvider(p, db); err != nil {
			return err
		}
		if option.applyUpByOne {
			return nil
		}
	}
	// At this point there are no more migrations to apply. But we need to maintain
	// the following behaviour:
	// UpByOne returns an error to signifying there are no more migrations.
	// Up and UpTo return nil
	p.log.Printf("goose: no migrations to run. current version: %d\n", current)
	if option.applyUpByOne {
		return ErrNoNextVersion
	}
	return nil
}

// upToNoVersioning applies up migrations up to, and including, the
// target version.
func (p *Provider) upToNoVersioning(db *sql.DB, migrations Migrations, version int64) error {
	var finalVersion int64
	for _, current := range migrations {
		if current.Version > version {
			break
		}
		current.noVersioning = true
		if err := current.UpWithProvider(p, db); err != nil {
			return err
		}
		finalVersion = current.Version
	}
	p.log.Printf("goose: up to current file version: %d\n", finalVersion)
	return nil
}

func (p *Provider) upWithMissing(
	db *sql.DB,
	missingMigrations Migrations,
	foundMigrations Migrations,
	dbMigrations Migrations,
	option *options,
) error {
	lookupApplied := make(map[int64]bool)
	for _, found := range dbMigrations {
		lookupApplied[found.Version] = true
	}

	// Apply all missing migrations first.
	for _, missing := range missingMigrations {
		if err := missing.UpWithProvider(p, db); err != nil {
			return err
		}
		// Apply one migration and return early.
		if option.applyUpByOne {
			return nil
		}
		// TODO(mf): do we need this check? It's a bit redundant, but we may
		// want to keep it as a safe-guard. Maybe we should instead have
		// the underlying query (if possible) return the current version as
		// part of the same transaction.
		current, err := p.GetDBVersion(db)
		if err != nil {
			return err
		}
		if current == missing.Version {
			lookupApplied[missing.Version] = true
			continue
		}
		return fmt.Errorf("error: missing migration:%d does not match current db version:%d",
			current, missing.Version)
	}

	// We can no longer rely on the database version_id to be sequential because
	// missing (out-of-order) migrations get applied before newer migrations.

	for _, found := range foundMigrations {
		// TODO(mf): instead of relying on this lookup, consider hitting
		// the database directly?
		// Alternatively, we can skip a bunch migrations and start the cursor
		// at a version that represents 100% applied migrations. But this is
		// risky, and we should aim to keep this logic simple.
		if lookupApplied[found.Version] {
			continue
		}
		if err := found.UpWithProvider(p, db); err != nil {
			return err
		}
		if option.applyUpByOne {
			return nil
		}
	}
	current, err := p.GetDBVersion(db)
	if err != nil {
		return err
	}
	// At this point there are no more migrations to apply. But we need to maintain
	// the following behaviour:
	// UpByOne returns an error to signifying there are no more migrations.
	// Up and UpTo return nil
	p.log.Printf("goose: no migrations to run. current version: %d\n", current)
	if option.applyUpByOne {
		return ErrNoNextVersion
	}
	return nil
}

// Up applies all available migrations.
func Up(db *sql.DB, dir string, opts ...OptionsFunc) error {
	return defaultProvider.UpTo(db, dir, maxVersion, opts...)
}

// Up applies all available migrations.
func (p *Provider) Up(db *sql.DB, dir string, opts ...OptionsFunc) error {
	return p.UpTo(db, dir, maxVersion, opts...)
}

// UpByOne migrates up by a single version.
func UpByOne(db *sql.DB, dir string, opts ...OptionsFunc) error {
	opts = append(opts, withApplyUpByOne())
	return defaultProvider.UpTo(db, dir, maxVersion, opts...)
}

// UpByOne migrates up by a single version.
func (p *Provider) UpByOne(db *sql.DB, dir string, opts ...OptionsFunc) error {
	opts = append(opts, withApplyUpByOne())
	return p.UpTo(db, dir, maxVersion, opts...)
}

// listAllDBVersions returns a list of all migrations, ordered ascending.
// TODO(mf): fairly cheap, but a nice-to-have is pagination support.
func listAllDBVersions(dialect SQLDialect, db *sql.DB) (Migrations, error) {
	rows, err := dialect.dbVersionQuery(db)
	if err != nil {
		return nil, createVersionTable(dialect, db)
	}
	var all Migrations
	for rows.Next() {
		var versionID int64
		var isApplied bool
		if err := rows.Scan(&versionID, &isApplied); err != nil {
			return nil, err
		}
		all = append(all, &Migration{
			Version: versionID,
		})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Version < all[j].Version
	})
	return all, nil
}

// findMissingMigrations migrations returns all missing migrations.
// A migration is considered missing if it has a version less than the
// current known max version.
func findMissingMigrations(knownMigrations, newMigrations Migrations) Migrations {
	max := knownMigrations[len(knownMigrations)-1].Version
	existing := make(map[int64]bool)
	for _, known := range knownMigrations {
		existing[known.Version] = true
	}
	var missing Migrations
	for _, newMigration := range newMigrations {
		if !existing[newMigration.Version] && newMigration.Version < max {
			missing = append(missing, newMigration)
		}
	}
	sort.SliceStable(missing, func(i, j int) bool {
		return missing[i].Version < missing[j].Version
	})
	return missing
}

package goose

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/pkg/errors"
)

type options struct {
	allowMissing     bool
	applyUpByOne     bool
	noVersioning     bool
	noOutput         bool
	eventsChannel    chan<- Eventer
	dontCloseChannel bool
	// sequentialVersionsOnly will only allow up to apply if only sequential version files exist
	sequentialVersionsOnly bool
}

// send will sent the event over the eventsChannel if it is not nil
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

// WithOnlySequentialVersions states that Up should error if there are any timestamp based version files.
// Up will only run if the only version files left are sequential.
func WithOnlySequentialVersions() OptionsFunc {
	return func(o *options) { o.sequentialVersionsOnly = true }
}

func withApplyUpByOne() OptionsFunc {
	return func(o *options) { o.applyUpByOne = true }
}

// WithEvents will publish events to the given channel, and close the channel upon the
// completion of the function.
func WithEvents(events chan<- Eventer, DontCloseChannelOnComplete bool) OptionsFunc {
	return func(o *options) {
		o.eventsChannel = events
		o.dontCloseChannel = DontCloseChannelOnComplete
	}
}

// withDontCloseChannel will prevent the Event channel from being closed after the
// function exits
func withDontCloseChannel() OptionsFunc {
	return func(o *options) { o.dontCloseChannel = true }
}

// WithNoOutput will suppress the output of the function
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

type VersionCountEvent struct {
	*Event
	Version           int64
	VersionSource     string
	TotalVersionsLeft int
}

func (e VersionCountEvent) IsEqual(o Eventer) bool {
	oe, ok := o.(VersionCountEvent)
	if !ok {
		poe, ok := o.(*VersionCountEvent)
		if !ok || poe == nil {
			return false
		}
		oe = *poe
	}
	return e.Version == oe.Version &&
		e.VersionSource == oe.VersionSource &&
		e.TotalVersionsLeft == oe.TotalVersionsLeft
}

var (
	_ = Eventer((*VersionCountEvent)(nil))
	_ = Eventer(VersionCountEvent{})
)

// VersionApplyEvent usually comes in pairs unless there is an error, the first event (with applied set to false) will be emmited
// before the version is applied to the database, and the applied version after the new version has been applied.
type VersionApplyEvent struct {
	*Event
	From       int64
	FromSource string
	To         int64
	ToSource   string
	ApplyAT    time.Time
	Missing    bool
	Applied    bool
	Versioned  bool
	Down       bool
}

func (e VersionApplyEvent) IsEqual(o Eventer) bool {
	oe, ok := o.(VersionApplyEvent)
	if !ok {
		poe, ok := o.(*VersionApplyEvent)
		if !ok || poe == nil {
			return false
		}
		oe = *poe
	}
	return e.From == oe.From &&
		e.FromSource == oe.FromSource &&
		e.To == oe.To &&
		e.ToSource == oe.ToSource &&
		e.Missing == oe.Missing &&
		e.Applied == oe.Applied &&
		e.Versioned == oe.Versioned &&
		e.Down == oe.Down
}

var (
	_ = Eventer((*VersionApplyEvent)(nil))
	_ = Eventer(VersionApplyEvent{})
)

// UpTo migrates up to a specific version.
func UpTo(db *sql.DB, dir string, version int64, opts ...OptionsFunc) error {
	return defaultProvider.UpTo(db, dir, version, opts...)
}

func (p *Provider) UpTo(db *sql.DB, dir string, version int64, opts ...OptionsFunc) (err error) {
	options := applyOptions(opts)
	if options.shouldCloseEventsChannel() {
		defer close(options.eventsChannel)
	}
	foundMigrations, err := p.CollectMigrations(dir, minVersion, version)
	if err != nil {
		return err
	}

	if options.sequentialVersionsOnly {
		tsVers, _ := foundMigrations.timestamped()
		if len(tsVers) > 0 {
			// we should error as there are timestamped version;
			// and we have been asked to only apply sequential version number.
			return ErrTimestampVersionsExist{
				Migrations: tsVers,
			}
		}
	}

	if options.noVersioning {
		totalMigrations := len(foundMigrations)
		if totalMigrations == 0 {
			options.send(VersionCountEvent{
				Version:           -1,
				VersionSource:     "",
				TotalVersionsLeft: 0,
			})
			return nil
		}
		if options.applyUpByOne {
			// For up-by-one this means keep re-applying the first
			// migration over and over.
			version = foundMigrations[0].Version
			totalMigrations = 1
		}
		options.send(VersionCountEvent{
			Version:           -1,
			VersionSource:     "",
			TotalVersionsLeft: totalMigrations,
		})
		finalVersion, err := p.upToNoVersioning(db, foundMigrations, version, options)
		if err != nil {
			return err
		}
		if !options.noOutput {
			p.log.Printf("goose: up to current file version: %d\n", finalVersion)
		}
		return nil
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
	if len(missingMigrations) > 0 && !options.allowMissing {
		return MissingMigrationsErrFromMigrations(missingMigrations)
	}

	if options.allowMissing {
		return p.upWithMissing(
			db,
			missingMigrations,
			foundMigrations,
			dbMigrations,
			options,
		)
	}

	var current int64
	var sendTotal = true
	for {
		current, err = p.GetDBVersion(db)
		if err != nil {
			return err
		}
		cMigration, _ := foundMigrations.Current(current)
		if cMigration == nil {
			cMigration = &Migration{
				Version: -1,
				Source:  "",
			}
		}
		if sendTotal {
			sendTotal = false
			options.send(VersionCountEvent{
				Version:           cMigration.Version,
				VersionSource:     cMigration.Source,
				TotalVersionsLeft: foundMigrations.NumberOfMigrations(current, false),
			})
		}
		next, err := foundMigrations.Next(current)
		if err != nil {
			if errors.Is(err, ErrNoNextVersion) {
				break
			}
			return fmt.Errorf("failed to find next migration: %v", err)
		}
		options.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         next.Version,
			ToSource:   next.Source,
			ApplyAT:    time.Now(),
			Applied:    false,
			Versioned:  true,
		})
		if err := next.UpWithProvider(p, db); err != nil {
			return err
		}
		options.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         next.Version,
			ToSource:   next.Source,
			ApplyAT:    time.Now(),
			Applied:    true,
			Versioned:  true,
		})
		if options.applyUpByOne {
			return nil
		}
	}
	// At this point there are no more migrations to apply. But we need to maintain
	// the following behaviour:
	// UpByOne returns an error to signifying there are no more migrations.
	// Up and UpTo return nil
	if !options.noOutput {
		p.log.Printf("goose: no migrations to run. current version: %d\n", current)
	}
	if options.applyUpByOne {
		return ErrNoNextVersion
	}
	return nil
}

// upToNoVersioning applies up migrations up to, and including, the
// target version.
func (p *Provider) upToNoVersioning(db *sql.DB, migrations Migrations, version int64, options *options) (int64, error) {
	var finalVersion int64
	var cMigration = &Migration{
		Version: -1,
		Source:  "",
	}
	for _, current := range migrations {
		if current.Version > version {
			break
		}
		current.noVersioning = true
		options.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         current.Version,
			ToSource:   current.Source,
			ApplyAT:    time.Now(),
			Applied:    false,
		})
		if err := current.UpWithProvider(p, db); err != nil {
			return -1, err
		}
		options.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         current.Version,
			ToSource:   current.Source,
			ApplyAT:    time.Now(),
			Applied:    true,
		})
		finalVersion = current.Version
	}
	return finalVersion, nil
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

	current, err := p.GetDBVersion(db)
	if err != nil {
		return err
	}
	var cMigration Migration
	{
		m, _ := foundMigrations.Current(current)
		if m == nil {
			cMigration = Migration{Version: -1}
		} else {
			cMigration = *m
		}
	}
	totalMigrations := func() int {
		if option.applyUpByOne {
			return 1
		}
		return foundMigrations.NumberOfMigrations(current, false) + len(missingMigrations)
	}()
	option.send(VersionCountEvent{
		Version:           cMigration.Version,
		VersionSource:     cMigration.Source,
		TotalVersionsLeft: totalMigrations,
	})

	// Apply all missing migrations first.
	for _, missing := range missingMigrations {
		option.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         missing.Version,
			ToSource:   missing.Source,
			ApplyAT:    time.Now(),
			Applied:    false,
			Missing:    true,
			Versioned:  true,
		})
		if err := missing.UpWithProvider(p, db); err != nil {
			return err
		}
		option.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         missing.Version,
			ToSource:   missing.Source,
			ApplyAT:    time.Now(),
			Applied:    true,
			Missing:    true,
			Versioned:  true,
		})
		cMigration = *missing
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
			cMigration = *found
			continue
		}
		option.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         found.Version,
			ToSource:   found.Source,
			ApplyAT:    time.Now(),
			Applied:    false,
			Versioned:  true,
		})
		if err := found.UpWithProvider(p, db); err != nil {
			return err
		}
		option.send(VersionApplyEvent{
			From:       cMigration.Version,
			FromSource: cMigration.Source,
			To:         found.Version,
			ToSource:   found.Source,
			ApplyAT:    time.Now(),
			Applied:    true,
			Versioned:  true,
		})
		if option.applyUpByOne {
			return nil
		}
		cMigration = *found
	}

	if !option.noOutput {
		current, err = p.GetDBVersion(db)
		if err != nil {
			return err
		}
		p.log.Printf("goose: no migrations to run. current version: %d\n", current)
	}

	// At this point there are no more migrations to apply. But we need to maintain
	// the following behaviour:
	// UpByOne returns an error to signifying there are no more migrations.
	// Up and UpTo return nil
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
	existing := make(map[int64]bool, len(knownMigrations))
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

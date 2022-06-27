package goose

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"
)

const (
	noVersioning   = "no versioning"
	pendingVersion = "pending"
)

// StatusEvent is a version number, and source of a
// migration, and whether it has been applied
type StatusEvent struct {
	*Event

	// Source is the full path of the source file, use `Script` to get the name
	Source string
	// Version is the version number, it should be -1 if not set
	Version   int64
	Versioned bool
	// If not zero then the time the migration was applied at
	AppliedAt time.Time
}

func (se StatusEvent) AppliedString() string {
	if se.AppliedAt.IsZero() {
		return pendingVersion
	}
	return se.AppliedAt.Format(time.ANSIC)
}

func (se StatusEvent) String() string {
	if !se.Versioned {
		return noVersioning
	}
	return se.AppliedString()
}

func (se StatusEvent) Script() string { return filepath.Base(se.Source) }

// Status prints the status of all migrations.
func Status(db *sql.DB, dir string, opts ...OptionsFunc) error {
	return defaultProvider.Status(db, dir, opts...)
}

func (p *Provider) Status(db *sql.DB, dir string, opts ...OptionsFunc) (err error) {
	if p == nil {
		return nil
	}
	var events = make(chan Eventer)
	var options = applyOptions(opts)
	if options.shouldCloseEventsChannel() {
		defer close(options.eventsChannel)
	}
	go func() {
		err = p.eventStatus(db, dir, events, options.noVersioning)
	}()
	if !options.noOutput {
		p.log.Println("    Applied At                  Migration")
		p.log.Println("    =======================================")
	}
	for event := range events {
		options.send(event)
		current, ok := event.(StatusEvent)
		if !ok {
			continue
		}

		if !options.noOutput {
			p.log.Printf("    %-24s -- %v\n", current.AppliedString(), current.Script())
		}
	}
	return err
}

// eventStatus will send events to the provided channel, closing the channel after all events or an error is encountered.
// If an error is encountered it will be returned by the function
func (p *Provider) eventStatus(db *sql.DB, dir string, eventsChannel chan<- Eventer, noVersioning bool) error {
	if eventsChannel == nil {
		return nil
	}
	defer close(eventsChannel)

	migrations, err := p.CollectMigrations(dir, minVersion, maxVersion)

	if err != nil {
		return fmt.Errorf("failed to collect migrations: %w", err)
	}
	if noVersioning || db == nil {
		for _, current := range migrations {
			eventsChannel <- StatusEvent{
				Source:    current.Source,
				Version:   current.Version,
				Versioned: false,
			}
		}
		return nil
	}

	// must ensure that the version table exists if we're running on a pristine DB
	if _, err := p.EnsureDBVersion(db); err != nil {
		return fmt.Errorf("failed to ensure DB version: %w", err)
	}

	// we have a db so, let's get the versions of the database
	q := p.dialect.migrationSQL()
	var row MigrationRecord
	for _, current := range migrations {
		err := db.QueryRow(q, current.Version).Scan(&row.TStamp, &row.IsApplied)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to query the latest migration: %w", err)
		}
		var at time.Time
		if row.IsApplied {
			at = row.TStamp
		}

		eventsChannel <- StatusEvent{
			Source:    current.Source,
			Version:   current.Version,
			Versioned: true,
			AppliedAt: at,
		}
	}
	return nil
}

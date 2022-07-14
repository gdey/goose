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

func (se StatusEvent) VersionedString() string {
	if !se.Versioned {
		return noVersioning
	}
	return se.AppliedString()
}

func (se StatusEvent) String() string {
	return fmt.Sprintf("%s : %s (%d)", se.VersionedString(), se.Source, se.Version)
}

func (se StatusEvent) Script() string { return filepath.Base(se.Source) }

func (se StatusEvent) IsEqual(e Eventer) bool {
	otherSE, ok := e.(StatusEvent)
	if !ok {
		// check to see if it's a pointer
		pSE, ok := e.(*StatusEvent)
		if !ok || pSE == nil {
			return false
		}
		otherSE = *pSE
	}
	return se.Versioned == otherSE.Versioned &&
		se.AppliedAt.IsZero() == otherSE.AppliedAt.IsZero() &&
		se.Version == otherSE.Version &&
		se.Source == otherSE.Source
}

var (
	_ = Eventer((*StatusEvent)(nil))
	_ = Eventer(StatusEvent{})
)

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
		err = p.eventsStatus(db, dir, events, options.noVersioning)
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

// eventsStatus will send events to the provided channel, closing the channel after all events or an error is encountered.
// If an error is encountered it will be returned by the function
func (p *Provider) eventsStatus(db *sql.DB, dir string, eventsChannel chan<- Eventer, noVersioning bool) error {
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
	for _, current := range migrations {
		var (
			isApplied bool
			at        time.Time
		)
		err := db.QueryRow(q, current.Version).Scan(&at, &isApplied)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to query the latest migration: %w", err)
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

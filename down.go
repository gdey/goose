package goose

import (
	"database/sql"
	"fmt"
	"time"
)

// Down rolls back a single migration from the current version.
func Down(db *sql.DB, dir string, opts ...OptionsFunc) error {
	return defaultProvider.Down(db, dir, opts...)
}

// Down rolls back a single migration from the current version.
func (p *Provider) Down(db *sql.DB, dir string, opts ...OptionsFunc) error {
	option := applyOptions(opts)
	if option.shouldCloseEventsChannel() {
		defer close(option.eventsChannel)
	}
	migrations, err := p.CollectMigrations(dir, minVersion, maxVersion)
	if err != nil {
		return err
	}
	if option.noVersioning {
		if len(migrations) == 0 {
			return nil
		}
		currentVersion := migrations[len(migrations)-1].Version
		// Migrate only the latest migration down.
		return downToNoVersioning(p, db, migrations, currentVersion-1, opts...)
	}
	currentVersion, err := p.GetDBVersion(db)
	if err != nil {
		return err
	}
	current, err := migrations.Current(currentVersion)
	if err != nil {
		return fmt.Errorf("no migration %v", currentVersion)
	}
	previous, err := migrations.Previous(currentVersion)
	if err != nil {
		return fmt.Errorf("no previous migration for %v", currentVersion)
	}
	option.send(VersionCountEvent{
		Version:           current.Version,
		VersionSource:     current.Source,
		TotalVersionsLeft: 1,
	})
	option.send(VersionApplyEvent{
		From:       current.Version,
		FromSource: current.Source,
		To:         previous.Version,
		ToSource:   previous.Source,
		ApplyAT:    time.Now(),
		Applied:    false,
		Down:       true,
		Versioned:  true,
	})
	err = current.DownWithProvider(p, db)
	if err != nil {
		return err
	}
	option.send(VersionApplyEvent{
		From:       current.Version,
		FromSource: current.Source,
		To:         previous.Version,
		ToSource:   previous.Source,
		ApplyAT:    time.Now(),
		Applied:    true,
		Down:       true,
		Versioned:  true,
	})
	return nil
}

// DownTo rolls back migrations to a specific version.
func DownTo(db *sql.DB, dir string, version int64, opts ...OptionsFunc) error {
	return defaultProvider.DownTo(db, dir, version, opts...)
}

// DownTo rolls back migrations to a specific version.
func (p *Provider) DownTo(db *sql.DB, dir string, version int64, opts ...OptionsFunc) error {
	option := applyOptions(opts)
	if option.shouldCloseEventsChannel() {
		close(option.eventsChannel)
	}
	migrations, err := p.CollectMigrations(dir, minVersion, maxVersion)
	if err != nil {
		return err
	}
	if option.noVersioning {
		return downToNoVersioning(p, db, migrations, version, opts...)
	}

	for {
		currentVersion, err := p.GetDBVersion(db)
		if err != nil {
			return err
		}

		if currentVersion == 0 {
			if !option.noOutput {
				p.log.Printf("goose: no migrations to run. current version: %d\n", currentVersion)
			}
			return nil
		}
		current, err := migrations.Current(currentVersion)
		if err != nil {
			if !option.noOutput {
				p.log.Printf("goose: migration file not found for current version (%d), error: %s\n", currentVersion, err)
			}
			return err
		}

		if current.Version <= version {
			if !option.noOutput {
				p.log.Printf("goose: no migrations to run. current version: %d\n", currentVersion)
			}
			return nil
		}

		if err = current.DownWithProvider(p, db); err != nil {
			return err
		}
	}
}

// downToNoVersioning applies down migrations down to, but not including, the
// target version.
func downToNoVersioning(p *Provider, db *sql.DB, migrations Migrations, version int64, opts ...OptionsFunc) error {
	if p == nil {
		p = defaultProvider
	}
	option := applyOptions(opts)
	ver, err := migrations.Last()
	if err != nil {
		// There are not versions to migrate down to, so just return
		return nil
	}

	var finalVersion int64
	var finalI = 0
	for i := len(migrations) - 1; i >= 0; i-- {
		if version >= migrations[i].Version {
			finalVersion = migrations[i].Version
			finalI = i
			break
		}
	}

	option.send(VersionCountEvent{
		Version:           ver.Version,
		VersionSource:     ver.Source,
		TotalVersionsLeft: migrations.NumberOfMigrationsTo(ver.Version, finalVersion),
	})

	for i := len(migrations) - 1; i >= finalI; i-- {
		preV := int64(0) // 0 represents there are not version in the database
		preS := ""
		if i > 0 {
			preV = migrations[i-1].Version
			preS = migrations[i-1].Source
		}
		migrations[i].noVersioning = true
		option.send(VersionApplyEvent{
			From:       migrations[i].Version,
			FromSource: migrations[i].Source,
			To:         preV,
			ToSource:   preS,
			ApplyAT:    time.Now(),
			Applied:    false,
			Down:       true,
		})
		if err := migrations[i].DownWithProvider(p, db); err != nil {
			return err
		}
		option.send(VersionApplyEvent{
			From:       migrations[i].Version,
			FromSource: migrations[i].Source,
			To:         preV,
			ToSource:   preS,
			ApplyAT:    time.Now(),
			Applied:    true,
			Down:       true,
		})
	}
	if !option.noOutput {
		p.log.Printf("goose: down to current file version: %d\n", finalVersion)
	}
	return nil
}

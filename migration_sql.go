package goose

import (
	"database/sql"
	"regexp"

	"github.com/pkg/errors"
)

// Run a migration specified in raw SQL.
//
// Sections of the script can be annotated with a special comment,
// starting with "-- +goose" to specify whether the section should
// be applied during an Up or Down migration
//
// All statements following an Up or Down directive are grouped together
// until another direction directive is found.
func runSQLMigration(p *Provider, db *sql.DB, statements []string, useTx bool, v int64, direction bool, noVersioning bool) error {
	if p == nil {
		p = defaultProvider
	}
	if useTx {
		// TRANSACTION.

		p.verboseInfo("Begin transaction")

		tx, err := db.Begin()
		if err != nil {
			return errors.Wrap(err, "failed to begin transaction")
		}

		for _, query := range statements {
			p.verboseInfo("Executing statement: %s\n", clearStatement(query))
			if _, err = tx.Exec(query); err != nil {
				p.verboseInfo("Rollback transaction")
				tx.Rollback()
				return errors.Wrapf(err, "failed to execute SQL query %q", clearStatement(query))
			}
		}

		if !noVersioning {
			if direction {
				if _, err := tx.Exec(p.dialect.insertVersionSQL(), v, direction); err != nil {
					p.verboseInfo("Rollback transaction")
					tx.Rollback()
					return errors.Wrap(err, "failed to insert new goose version")
				}
			} else {
				if _, err := tx.Exec(p.dialect.deleteVersionSQL(), v); err != nil {
					p.verboseInfo("Rollback transaction")
					tx.Rollback()
					return errors.Wrap(err, "failed to delete goose version")
				}
			}
		}

		p.verboseInfo("Commit transaction")
		if err := tx.Commit(); err != nil {
			return errors.Wrap(err, "failed to commit transaction")
		}

		return nil
	}

	// NO TRANSACTION.
	for _, query := range statements {
		p.verboseInfo("Executing statement: %s", clearStatement(query))
		if _, err := db.Exec(query); err != nil {
			return errors.Wrapf(err, "failed to execute SQL query %q", clearStatement(query))
		}
	}
	if !noVersioning {
		if direction {
			if _, err := db.Exec(p.dialect.insertVersionSQL(), v, direction); err != nil {
				return errors.Wrap(err, "failed to insert new goose version")
			}
		} else {
			if _, err := db.Exec(p.dialect.deleteVersionSQL(), v); err != nil {
				return errors.Wrap(err, "failed to delete goose version")
			}
		}
	}

	return nil
}

const (
	grayColor  = "\033[90m"
	resetColor = "\033[00m"
)

func (p *Provider) verboseInfo(s string, args ...interface{}) {
	if p == nil {
		p = defaultProvider
	}
	if p.verbose {
		p.log.Printf(grayColor+s+resetColor, args...)
	}
}

var (
	matchSQLComments = regexp.MustCompile(`(?m)^--.*$[\r\n]*`)
	matchEmptyEOL    = regexp.MustCompile(`(?m)^$[\r\n]*`) // TODO: Duplicate
)

func clearStatement(s string) string {
	s = matchSQLComments.ReplaceAllString(s, ``)
	return matchEmptyEOL.ReplaceAllString(s, ``)
}

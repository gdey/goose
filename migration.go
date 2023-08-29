package goose

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// MigrationRecord struct.
type MigrationRecord struct {
	VersionID int64
	TStamp    time.Time
	IsApplied bool // was this a result of up() or down()
}

// Migration struct.
type Migration struct {
	Version      int64
	Next         int64  // next version, or -1 if none
	Previous     int64  // previous version, -1 if none
	Source       string // path to .sql script or go file
	Registered   bool
	UpFn         func(*sql.Tx) error // Up go migration function
	DownFn       func(*sql.Tx) error // Down go migration function
	noVersioning bool
}

func (m *Migration) String() string {
	return fmt.Sprintf(m.Source)
}

// Up runs an up migration.
// Deprecated: please use UpWithProvider
func (m *Migration) Up(db *sql.DB) error {
	return m.UpWithProvider(defaultProvider, db)
}

func (m *Migration) UpWithProvider(p *Provider, db *sql.DB) error {
	return m.run(p, db, true)
}

// Down runs a down migration.
// Deprecated: please use DownWithProvider
func (m *Migration) Down(db *sql.DB) error {
	return m.DownWithProvider(defaultProvider, db)
}

func (m *Migration) DownWithProvider(p *Provider, db *sql.DB) error {
	return m.run(p, db, false)
}

// runSql will parse out the sql statements from the given io.Reader for the direction, and apply them to the
// provided db connection
func (m *Migration) runSql(f io.Reader, p *Provider, db *sql.DB, direction bool) error {
	statements, useTx, err := parseSQLMigration(p, f, direction)
	if err != nil {
		return ErrMigrationSQLParse{
			Filename:  filepath.Base(m.Source),
			ErrUnwrap: ErrUnwrap{err},
			Up:        direction,
		}
	}

	if err := runSQLMigration(p, db, statements, useTx, m.Version, direction, m.noVersioning); err != nil {
		return fmt.Errorf("ERROR %v: failed to run SQL migration: %w", filepath.Base(m.Source), err)
	}

	if len(statements) > 0 {
		p.log.Println("OK   ", filepath.Base(m.Source))
		return nil
	}

	p.log.Println("EMPTY", filepath.Base(m.Source))
	return nil
}

func getExtension(s string) string {
	b := []byte(filepath.Base(s)) // shadow
	i := bytes.LastIndexByte(b, '.')
	if i == -1 {
		return ""
	}
	ext := b[i:]
	if i = bytes.LastIndexByte(b[:i], '.'); i == -1 {
		return string(ext)
	}
	return string(b[i:])
}

func (m *Migration) parseAndRunSQLMigration(p *Provider, db *sql.DB, f io.Reader, direction bool) error {
	statements, useTx, err := parseSQLMigration(p, f, direction)
	if err != nil {
		return fmt.Errorf("ERROR %v: failed to parse SQL migration file: %w", filepath.Base(m.Source), err)
	}

	if err := runSQLMigration(p, db, statements, useTx, m.Version, direction, m.noVersioning); err != nil {
		return fmt.Errorf("ERROR %v: failed to run SQL migration: %w", filepath.Base(m.Source), err)
	}

	if len(statements) > 0 {
		p.log.Println("OK   ", filepath.Base(m.Source))
	} else {
		p.log.Println("EMPTY", filepath.Base(m.Source))
	}
	return nil
}

func parseExecuteTplSql(filesys fs.FS, source, packageName string) (*bytes.Buffer, error) {
	type tplValue struct {
		Filename    string
		PackageName string
	}
	var buff bytes.Buffer
	baseSource := filepath.Base(source)
	tpl, err := template.ParseFS(filesys, source)
	if err != nil {
		return nil, fmt.Errorf("ERROR %v: failed to open/parse template SQL migration file: %w", baseSource, err)
	}
	if err = tpl.Execute(&buff, tplValue{
		Filename:    baseSource,
		PackageName: packageName,
	}); err != nil {
		return nil, fmt.Errorf("ERROR %v: failed to execute template SQL migration file: %w", baseSource, err)
	}
	return &buff, nil

}

func (m *Migration) run(p *Provider, db *sql.DB, direction bool) error {
	if p == nil {
		p = defaultProvider
	}

	switch ext := getExtension(m.Source); ext {
	default:
		return ErrUnknownExtension{Extension: ext}
	case ".sql":
		f, err := p.baseFS.Open(m.Source)
		if err != nil {
			return fmt.Errorf("ERROR %v: failed to open SQL migration file: %w", filepath.Base(m.Source), err)
		}
		defer f.Close()
		return m.parseAndRunSQLMigration(p, db, f, direction)

	case ".tpl.sql":
		buff, err := parseExecuteTplSql(p.baseFS, m.Source, p.packageName)
		if err != nil {
			return err
		}
		return m.parseAndRunSQLMigration(p, db, buff, direction)

	case ".go":
		if !m.Registered {
			return fmt.Errorf("ERROR %v: failed to run Go migration: Go functions must be registered and built into a custom binary (see https://github.com/gdey/goose/tree/master/examples/go-migrations)", m.Source)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("ERROR failed to begin transaction: %w", err)
		}

		fn := m.UpFn
		if !direction {
			fn = m.DownFn
		}

		if fn != nil {
			// Run Go migration function.
			if err := fn(tx); err != nil {
				tx.Rollback()
				return fmt.Errorf("ERROR %v: failed to run Go migration function %T: %w", filepath.Base(m.Source), fn, err)
			}
		}
		if !m.noVersioning {
			if direction {
				if _, err := tx.Exec(p.dialect.insertVersionSQL(), m.Version, direction); err != nil {
					tx.Rollback()
					return fmt.Errorf("ERROR failed to execute transaction: %w", err)
				}
			} else {
				if _, err := tx.Exec(p.dialect.deleteVersionSQL(), m.Version); err != nil {
					tx.Rollback()
					return fmt.Errorf("ERROR failed to execute transaction: %w", err)
				}
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("ERROR failed to commit transaction: %w", err)
		}

		if fn != nil {
			p.log.Println("OK   ", filepath.Base(m.Source))
		} else {
			p.log.Println("EMPTY", filepath.Base(m.Source))
		}

		return nil
	}
}

// NumericComponent looks for migration scripts with names in the form:
// XXX_descriptive_name.ext where XXX specifies the version number
// and ext specifies the type of migration
func NumericComponent(name string) (int64, error) {
	base := filepath.Base(name)

	if ext := filepath.Ext(base); ext != ".go" && ext != ".sql" {
		return 0, errors.New("not a recognized migration file type")
	}

	idx := strings.Index(base, "_")
	if idx < 0 {
		return 0, errors.New("no filename separator '_' found")
	}

	n, e := strconv.ParseInt(base[:idx], 10, 64)
	if e == nil && n <= 0 {
		return 0, errors.New("migration IDs must be greater than zero")
	}

	return n, e
}

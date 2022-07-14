package goose_test

import (
	"database/sql"
	_ "github.com/lib/pq"
	"testing"
	"time"

	"github.com/gdey/goose/v3"
	"github.com/gdey/goose/v3/internal/testdb"
	"github.com/gdey/goose/v3/tests/e2e/testdata/postgres/migrations"
)

func Test_status_events(t *testing.T) {
	t.Parallel()
	type tcase struct {
		p     *goose.Provider
		setup func(provider *goose.Provider, db *sql.DB, path string) error
		// events are the expected events
		events []goose.StatusEvent

		debug bool
	}

	fn := func(tc tcase) func(*testing.T) {
		return func(t *testing.T) {
			db, cleanup, err := testdb.NewPostgres(
				//testdb.WithDebug(tc.debug),
				testdb.WithBindPort(0),
			)
			if err != nil {
				t.Errorf("failed to start up database container: %v", err)
				return
			}
			defer cleanup()

			// setup the database
			if tc.setup != nil {
				err = tc.setup(tc.p, db, ".")
				if err != nil {
					t.Fatalf("failed to setup test: %v", err)
				}
			}
			events := make(chan goose.Eventer)

			go func() {
				err = tc.p.Status(db, ".",
					goose.WithEvents(events, false),
					goose.WithNoOutput(),
				)
			}()
			var (
				failed bool
				i      int
			)
			for event := range events {
				event := event.(goose.StatusEvent)
				if tc.debug {
					t.Logf("Got event %v ", event)
				}

				if failed {
					// don't care any more just collect all events and exit
					continue
				}
				if i >= len(tc.events) {
					t.Errorf("more events, got %v+ expected %v", i+1, len(tc.events))
					failed = true
					continue
				}
				if !goose.AreEventsEqual(tc.events[i], event) {
					failed = true
					t.Errorf("event %d, got %v expected %v", i, event, tc.events[i])
				}
				i++
			}
			if failed {
				return
			}
			if err != nil {
				t.Errorf("error, got %v expected nil", err)
			}
		}
	}
	tests := map[string]tcase{
		"brand new db": {
			p: migrations.Provider,
			events: []goose.StatusEvent{
				{
					Source:    "00001_a.sql",
					Version:   1,
					Versioned: true,
				},
				{
					Source:    "00002_b.sql",
					Version:   2,
					Versioned: true,
				},
				{
					Source:    "00003_c.sql",
					Version:   3,
					Versioned: true,
				},
				{
					Source:    "00004_d.sql",
					Version:   4,
					Versioned: true,
				},
				{
					Source:    "00005_e.sql",
					Version:   5,
					Versioned: true,
				},
				{
					Source:    "00006_f.sql",
					Version:   6,
					Versioned: true,
				},
				{
					Source:    "00007_g.sql",
					Version:   7,
					Versioned: true,
				},
				{
					Source:    "00008_h.sql",
					Version:   8,
					Versioned: true,
				},
				{
					Source:    "00009_i.sql",
					Version:   9,
					Versioned: true,
				},
				{
					Source:    "00010_j.sql",
					Version:   10,
					Versioned: true,
				},
				{
					Source:    "00011_k.sql",
					Version:   11,
					Versioned: true,
				},
			},
		},
		"applied first 3": {
			p: migrations.Provider,
			setup: func(p *goose.Provider, db *sql.DB, path string) error {
				return p.UpTo(db, path, 3, goose.WithNoOutput())
			},
			events: []goose.StatusEvent{
				{
					Source:    "00001_a.sql",
					Version:   1,
					Versioned: true,
					AppliedAt: time.Now(),
				},
				{
					Source:    "00002_b.sql",
					Version:   2,
					Versioned: true,
					AppliedAt: time.Now(),
				},
				{
					Source:    "00003_c.sql",
					Version:   3,
					Versioned: true,
					AppliedAt: time.Now(),
				},
				{
					Source:    "00004_d.sql",
					Version:   4,
					Versioned: true,
				},
				{
					Source:    "00005_e.sql",
					Version:   5,
					Versioned: true,
				},
				{
					Source:    "00006_f.sql",
					Version:   6,
					Versioned: true,
				},
				{
					Source:    "00007_g.sql",
					Version:   7,
					Versioned: true,
				},
				{
					Source:    "00008_h.sql",
					Version:   8,
					Versioned: true,
				},
				{
					Source:    "00009_i.sql",
					Version:   9,
					Versioned: true,
				},
				{
					Source:    "00010_j.sql",
					Version:   10,
					Versioned: true,
				},
				{
					Source:    "00011_k.sql",
					Version:   11,
					Versioned: true,
				},
			},
			debug: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, fn(tc))
	}
}

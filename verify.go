package goose

import (
	"errors"
	"fmt"
)

type VerifyStatus struct {
	Status int
	Error  error
}

func (vs VerifyStatus) Ok() bool {
	return vs.Status == VerifyStatusOK ||
		vs.Status&VerifyStatusTsMigrations == VerifyStatusTsMigrations
}
func (vs VerifyStatus) HasTsMigrations() bool {
	return vs.Status&VerifyStatusTsMigrations == VerifyStatusTsMigrations
}

const (
	// VerifyStatusOK indicates that no issue were found, this includes not having any timestamp-based migrations.
	VerifyStatusOK = 0
)

const (
	// VerifyStatusErr indicates an error occurred of some sort. If this is the only bit set,
	// then it was a generic error dealing with accessing the migrations. The Error field of
	// VerifyStatus will have the error or errors
	VerifyStatusErr = 1 << iota
	// VerifyStatusTsMigrations indicates that there are timestamp-based migrations
	VerifyStatusTsMigrations
	// VerifyStatusTplSql indicates that there was an error loading, parsing, or executing sql templates.
	// the Error field will contain an error list with the error for each template that errored out.
	VerifyStatusTplSql = VerifyStatusErr | (1 << iota)
)

// Verify will check the migration directory to see if there are any errors, or other issues.
// It will return a VerifyStatus with any errors it found
func Verify(dir string) VerifyStatus { return defaultProvider.Verify(dir) }

// Verify will check the migration directory to see if there are any errors, or other issues.
// It will return a VerifyStatus with any errors it found
func (p *Provider) Verify(dir string) VerifyStatus {
	if p.baseDir != "" && (dir == "" || dir == ".") {
		dir = p.baseDir
	}
	migrations, err := p.collectMigrationsFS(p.baseFS, dir, minVersion, maxVersion)
	if err != nil {
		return VerifyStatus{
			Status: VerifyStatusErr,
			Error:  fmt.Errorf("failed to get migrations: %w", err),
		}
	}
	status := VerifyStatusOK
	// split into timestamped and versioned migrations
	tsMigrations, err := migrations.timestamped()
	if err != nil {
		return VerifyStatus{
			Status: VerifyStatusErr,
			Error:  fmt.Errorf("failed to get timestamped migrations: %w", err),
		}
	}
	vMigrations, err := migrations.versioned()
	if err != nil {
		return VerifyStatus{
			Status: VerifyStatusErr,
			Error:  fmt.Errorf("failed to get migrations: %w", err),
		}
	}
	if len(tsMigrations) > 0 {
		status |= VerifyStatusTsMigrations
	}

	// Check to see if there are any template sql files, and if there are try and compile them.
	// We are going to assume that vMigrations are less likely to have parsed errors in them.
	// These should have been deployed.
	var errs = make([]error, 0, len(tsMigrations))
	for _, m := range vMigrations {
		if getExtension(m.Source) != ".tpl.sql" {
			continue
		}
		if _, err := parseExecuteTplSql(p.baseFS, m.Source, p.packageName); err != nil {
			status |= VerifyStatusTplSql
			errs = append(errs, err)
		}
	}
	for _, m := range tsMigrations {
		if getExtension(m.Source) != ".tpl.sql" {
			continue
		}
		if _, err := parseExecuteTplSql(p.baseFS, m.Source, p.packageName); err != nil {
			status |= VerifyStatusTplSql
			errs = append(errs, err)
		}
	}

	return VerifyStatus{
		Status: status,
		Error:  errors.Join(errs...),
	}

}

package store

import (
	"os"
	"strings"
	"testing"
)

// TestBootstrapSchemaMatchesMigration prevents the operator-run bootstrap script
// from drifting from the schema embedded in the backend binary.
func TestBootstrapSchemaMatchesMigration(t *testing.T) {
	bootstrap, err := os.ReadFile("../../database/bootstrap.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "-- Schema for the lab environment monitoring backend (SHIXUN-4)."
	index := strings.Index(string(bootstrap), marker)
	if index < 0 {
		t.Fatal("bootstrap script does not contain the full schema")
	}
	if got, want := string(bootstrap[index:]), string(migration); got != want {
		t.Fatal("bootstrap schema differs from the backend's embedded migration")
	}
}

package store_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/store"
)

// testDatabaseEnv is the environment variable that enables the PostgreSQL
// integration tests. It holds a DSN to a disposable database: the suite creates
// and drops its own schema objects, so pointing it at a database that holds
// anything else will destroy that data.
const testDatabaseEnv = "TEST_DATABASE_URL"

// requireDatabase skips the test when no database is configured, and returns a
// context bounded by the test's lifetime.
//
// Skipping rather than failing is deliberate: the suite must run in CI where a
// database is provisioned, and must not block a developer who only has the
// in-memory store. The README states which environment runs it.
func requireDatabase(t *testing.T) (context.Context, string) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(testDatabaseEnv))
	if dsn == "" {
		t.Skipf("%s is not set; the PostgreSQL integration suite is skipped", testDatabaseEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx, dsn
}

// TestPostgresMigrations verifies that the embedded schema applies cleanly and
// is re-appliable, which is what lets the process migrate on every start.
func TestPostgresMigrations(t *testing.T) {
	ctx, dsn := requireDatabase(t)

	first, err := store.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("open with migrations: %v", err)
	}
	defer first.Close()

	if err := first.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	// Running the migration again is what start-up does, so it must not fail on
	// already-created objects.
	if err := first.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// TestPostgresConformance runs the shared contract suite against PostgreSQL, so
// that the deployment store cannot diverge from the behaviour the API and the
// alert engine rely on.
func TestPostgresConformance(t *testing.T) {
	ctx, dsn := requireDatabase(t)

	// Each subtest gets its own schema so parallel cases cannot see each other's
	// rows; the suite asserts exact counts.
	counter := 0
	factory := func(t *testing.T) store.Store {
		t.Helper()
		counter++
		schema := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), counter)

		admin, err := store.OpenPostgres(ctx, dsn)
		if err != nil {
			t.Fatalf("open admin connection: %v", err)
		}
		if err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
			t.Fatalf("create schema: %v", err)
		}
		admin.Close()

		scoped := scopedDSN(dsn, schema)
		instance, err := store.OpenPostgres(ctx, scoped)
		if err != nil {
			t.Fatalf("open scoped store: %v", err)
		}
		t.Cleanup(func() {
			instance.Close()
			cleanup, err := store.OpenPostgres(context.Background(), dsn)
			if err != nil {
				return
			}
			defer cleanup.Close()
			if err := cleanup.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
				t.Logf("dropping schema %s: %v", schema, err)
			}
		})
		return instance
	}

	runConformance(t, factory)
}

// scopedDSN pins a DSN to one schema by appending search_path.
func scopedDSN(dsn, schema string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "search_path=" + schema
}

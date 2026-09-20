package store_test

import (
	"testing"
	"time"

	"github.com/BobcGn/final/backend/internal/store"
)

// idTime is a fixed instant so identifier tests are reproducible.
var idTime = time.Date(2026, 9, 18, 11, 20, 0, 0, time.UTC)

// TestMemoryConformance runs the shared contract suite against the in-memory
// store that the API tests and local development use.
func TestMemoryConformance(t *testing.T) {
	runConformance(t, func(t *testing.T) store.Store {
		t.Helper()
		return store.NewMemory()
	})
}

// TestNewIDIsMonotonic verifies that identifiers created in the same
// millisecond still sort in creation order, which is what keeps cursor
// pagination of alerts stable.
func TestNewIDIsMonotonic(t *testing.T) {
	now := store.NewID(idTime)
	previous := now
	for index := 0; index < 1000; index++ {
		current := store.NewID(idTime)
		if current <= previous {
			t.Fatalf("identifier %d (%s) did not sort after %s", index, current, previous)
		}
		previous = current
	}
}

// TestNewIDFormat verifies the ULID length so a database column sized for it
// cannot truncate an identifier.
func TestNewIDFormat(t *testing.T) {
	id := store.NewID(idTime)
	if len(id) != 26 {
		t.Fatalf("identifier %q has length %d, want 26", id, len(id))
	}
	for index := 0; index < len(id); index++ {
		if !isCrockford(id[index]) {
			t.Fatalf("identifier %q contains %q outside the Crockford alphabet", id, id[index])
		}
	}
}

// isCrockford reports whether c belongs to the Crockford base32 alphabet.
func isCrockford(c byte) bool {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for index := 0; index < len(alphabet); index++ {
		if alphabet[index] == c {
			return true
		}
	}
	return false
}

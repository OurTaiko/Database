package httpapi

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"ourtaiko.dev/database/api/internal/database"
)

// Every schema file records its own version. One that forgets runs again at the
// next start, where its changes are already made, and the server stops starting.
func TestMigrationsRecordTheirVersions(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	entries, err := os.ReadDir("../database")
	if err != nil {
		t.Fatal(err)
	}
	latest := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, _ := strings.Cut(entry.Name(), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("%s needs a numeric prefix", entry.Name())
		}
		latest = max(latest, version)
	}
	var recorded int
	if err = pool.QueryRow(ctx, `SELECT max(version) FROM internal.schema_migrations`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != latest {
		t.Fatalf("recorded schema version %d, latest file %d", recorded, latest)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating a second time: %v", err)
	}
}

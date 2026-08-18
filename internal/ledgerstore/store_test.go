package ledgerstore

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpenWithConfigRejectsNilConfig(t *testing.T) {
	t.Parallel()

	store, err := OpenWithConfig(context.Background(), nil)
	if err == nil {
		store.Close()
		t.Fatal("OpenWithConfig(nil) error = nil, want error")
	}
	if err.Error() != "pgxpool config cannot be nil" {
		t.Fatalf("OpenWithConfig(nil) error = %q, want 'pgxpool config cannot be nil'", err.Error())
	}
}

func TestOpenRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), "://invalid-url")
	if err == nil {
		store.Close()
		t.Fatal("Open(invalid-url) error = nil, want error")
	}
}

func TestOpenWithConfigConnects(t *testing.T) {
	t.Parallel()

	databaseURL, ok := os.LookupEnv(testDatabaseURLEnvironment)
	if !ok || databaseURL == "" {
		t.Skipf("%s is not set; skipping database integration test", testDatabaseURLEnvironment)
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.MaxConns = 3

	store, err := OpenWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenWithConfig: %v", err)
	}
	defer store.Close()

	if _, err := store.Head(context.Background(), newTestLedgerID(t)); err != nil {
		t.Fatalf("Head: %v", err)
	}
}

package ledgerstore

import (
	"context"
	"os"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

const testDatabaseURLEnvironment = "CHAIN_TEST_DATABASE_URL"

// openTestStore skips when no test database is configured, so the default
// offline `make test` run stays clean.
func openTestStore(t *testing.T) *Store {
	t.Helper()

	databaseURL, ok := os.LookupEnv(testDatabaseURLEnvironment)
	if !ok || databaseURL == "" {
		t.Skipf("%s is not set; skipping database integration test", testDatabaseURLEnvironment)
	}

	store, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// newTestLedgerID returns a distinct ledger ID per test so tests sharing one
// database never collide on the sequence uniqueness constraint.
func newTestLedgerID(t *testing.T) ledgerv1.LedgerID {
	t.Helper()

	var ledgerID ledgerv1.LedgerID
	copy(ledgerID[:], t.Name())
	ledgerID[len(ledgerID)-1] = byte(len(t.Name()))
	return ledgerID
}

func newTestGenesisRecord(t *testing.T, ledgerID ledgerv1.LedgerID) ledgerv1.StructuralRecord {
	t.Helper()

	event, err := ledgerv1.NewGenesisEvent(ledgerID, 1_755_000_000_000)
	if err != nil {
		t.Fatalf("NewGenesisEvent: %v", err)
	}
	record, err := ledgerv1.NewRecord(event, "test-signer-key-reference", make([]byte, 70))
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	return record
}

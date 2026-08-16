package ledgerstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

// testRunSalt makes ledger IDs unique per test run. ledger_record is
// append-only and cannot be truncated, so a fixed ID would collide with rows
// left by an earlier run against the same database.
var testRunSalt = func() [8]byte {
	var salt [8]byte
	if _, err := rand.Read(salt[:]); err != nil {
		panic("generate test run salt: " + err.Error())
	}
	return salt
}()

// newTestLedgerID returns a ledger ID unique to this test and this run. The
// name and the run salt both feed a digest so that subtests sharing a long
// name prefix cannot collide, and rows left by an earlier run cannot either.
// ledger_record is append-only and cannot be truncated, so a collision is
// unrecoverable.
func newTestLedgerID(t *testing.T) ledgerv1.LedgerID {
	t.Helper()

	digest := sha256.Sum256(append([]byte(t.Name()), testRunSalt[:]...))
	return ledgerv1.LedgerID(digest)
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

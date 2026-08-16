package ledgerstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

// The unique sequence constraint is the sole arbiter of concurrent appends.
// Exactly one writer wins; the loser must rebuild and re-sign.
func TestConcurrentAppendsYieldOneWinner(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)

	const writers = 2
	results := make(chan error, writers)

	// Records are built up front, on the test goroutine, because
	// newTestGenesisRecord calls t.Fatalf on failure and calling t.Fatalf
	// from a non-test goroutine is invalid Go.
	records := make([]ledgerv1.StructuralRecord, writers)
	for index := range records {
		records[index] = newTestGenesisRecord(t, ledgerID)
	}

	var start sync.WaitGroup
	start.Add(1)
	for index := range writers {
		go func() {
			start.Wait()
			results <- store.Append(context.Background(), records[index])
		}()
	}
	start.Done()

	var succeeded, rejected int
	for range writers {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDuplicateRecord), errors.Is(err, ErrChainHeadMoved):
			rejected++
		default:
			t.Fatalf("unexpected Append error: %v", err)
		}
	}
	if succeeded != 1 || rejected != writers-1 {
		t.Fatalf("succeeded = %d, rejected = %d, want 1 and %d", succeeded, rejected, writers-1)
	}
}

func TestLedgerRecordRejectsMutation(t *testing.T) {
	store := openTestStore(t)
	record := newTestGenesisRecord(t, newTestLedgerID(t))
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	digest := record.RecordDigest()
	statements := map[string]string{
		"update":   `UPDATE ledger_record SET ledger_id = ledger_id WHERE record_digest = $1`,
		"delete":   `DELETE FROM ledger_record WHERE record_digest = $1`,
		"truncate": `TRUNCATE ledger_record`,
	}

	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			var err error
			if name == "truncate" {
				_, err = store.pool.Exec(context.Background(), statement)
			} else {
				_, err = store.pool.Exec(context.Background(), statement, digest[:])
			}
			if err == nil {
				t.Fatalf("%s succeeded, want append-only rejection", name)
			}
			if !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("%s error = %v, want append-only rejection", name, err)
			}
		})
	}
}

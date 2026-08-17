package ledgerstore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

// The unique sequence constraint, which the schema decision record calls the
// sole arbiter of concurrent appends, is what this test proves: writers race
// for the same (ledger_id, sequence_number) with genesis records that carry
// distinct signer key references, so each writer's record digest is unique
// and the digest primary key cannot be what arbitrates the race. Exactly one
// writer wins, and every loser must be rejected specifically with
// ErrChainHeadMoved; the loser must rebuild and re-sign.
func TestConcurrentAppendsYieldOneWinner(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)

	const writers = 2
	results := make(chan error, writers)

	// Records are built up front, on the test goroutine, because
	// newTestGenesisRecordWithSigner calls t.Fatalf on failure and calling
	// t.Fatalf from a non-test goroutine is invalid Go.
	records := make([]ledgerv1.StructuralRecord, writers)
	for index := range records {
		records[index] = newTestGenesisRecordWithSigner(t, ledgerID, strconv.Itoa(index))
	}
	for index := range records {
		for other := index + 1; other < len(records); other++ {
			if records[index].RecordDigest() == records[other].RecordDigest() {
				t.Fatalf(
					"writer %d and writer %d built identical record digests; "+
						"the test cannot isolate the sequence constraint", index, other,
				)
			}
		}
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
		case errors.Is(err, ErrChainHeadMoved):
			rejected++
		default:
			t.Fatalf("unexpected Append error: %v, want nil or ErrChainHeadMoved", err)
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

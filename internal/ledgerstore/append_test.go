package ledgerstore

import (
	"context"
	"testing"
)

func TestAppendStoresGenesisRecord(t *testing.T) {
	store := openTestStore(t)
	ledgerID := newTestLedgerID(t)
	record := newTestGenesisRecord(t, ledgerID)

	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

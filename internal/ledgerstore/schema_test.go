package ledgerstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

var recordBytesBoundPattern = regexp.MustCompile(
	`octet_length\(record_bytes\) BETWEEN 1 AND (\d+)`,
)

// The migration must carry a SQL literal because SQL cannot read the Go
// constant. This test is the only thing keeping the two from drifting.
func TestMigrationRecordSizeBoundMatchesProtocol(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*_create_ledger_record.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d create_ledger_record migrations, want exactly 1", len(matches))
	}

	migration, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	found := recordBytesBoundPattern.FindSubmatch(migration)
	if found == nil {
		t.Fatalf("migration %s has no record_bytes size bound", matches[0])
	}
	bound, err := strconv.Atoi(string(found[1]))
	if err != nil {
		t.Fatalf("parse bound: %v", err)
	}
	if bound != ledgerv1.MaxRecordBytes {
		t.Fatalf("migration bound = %d, want ledgerv1.MaxRecordBytes (%d)", bound, ledgerv1.MaxRecordBytes)
	}
}

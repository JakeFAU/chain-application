package admission

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/JakeFAU/chain-application/internal/ledgerstore"
	"github.com/JakeFAU/chain-application/internal/signer"
)

const testDatabaseURLEnvironment = "CHAIN_TEST_DATABASE_URL"

func TestNewAdmissionServiceValidation(t *testing.T) {
	t.Parallel()

	localSigner, err := signer.GenerateLocalSigner("local:p256:v1")
	if err != nil {
		t.Fatalf("GenerateLocalSigner: %v", err)
	}

	t.Run("nil store", func(t *testing.T) {
		t.Parallel()
		if _, err := New(nil, localSigner); err == nil {
			t.Fatal("New(nil store) error = nil, want error")
		}
	})

	t.Run("nil signer", func(t *testing.T) {
		t.Parallel()
		fakeStore := &ledgerstore.Store{}
		if _, err := New(fakeStore, nil); err == nil {
			t.Fatal("New(nil signer) error = nil, want error")
		}
	})
}

func openTestAdmissionService(t *testing.T) (*Service, *ledgerstore.Store) {
	t.Helper()

	databaseURL, ok := os.LookupEnv(testDatabaseURLEnvironment)
	if !ok || databaseURL == "" {
		t.Skipf("%s is not set; skipping database integration test", testDatabaseURLEnvironment)
	}

	store, err := ledgerstore.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("ledgerstore.Open: %v", err)
	}
	t.Cleanup(store.Close)

	localSigner, err := signer.GenerateLocalSigner("local:test:signer:v1")
	if err != nil {
		t.Fatalf("GenerateLocalSigner: %v", err)
	}

	fixedTime := time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)
	svc, err := New(store, localSigner, WithClock(func() time.Time {
		return fixedTime
	}))
	if err != nil {
		t.Fatalf("New admission service: %v", err)
	}

	return svc, store
}

var testRunSalt = func() [8]byte {
	var salt [8]byte
	if _, err := rand.Read(salt[:]); err != nil {
		panic("generate test run salt: " + err.Error())
	}
	return salt
}()

func newTestLedgerID(t *testing.T) ledgerv1.LedgerID {
	t.Helper()

	digest := sha256.Sum256(append([]byte(t.Name()), testRunSalt[:]...))
	return ledgerv1.LedgerID(digest)
}

func TestInitLedgerCreatesGenesisRecord(t *testing.T) {
	t.Parallel()

	svc, store := openTestAdmissionService(t)
	ledgerID := newTestLedgerID(t)
	ctx := context.Background()

	genesisRecord, err := svc.InitLedger(ctx, ledgerID)
	if err != nil {
		t.Fatalf("InitLedger: %v", err)
	}

	if genesisRecord.Event().Sequence() != 1 {
		t.Fatalf("sequence = %d, want 1", genesisRecord.Event().Sequence())
	}
	if genesisRecord.Event().Kind() != ledgerv1.EventKindLedgerInitialized {
		t.Fatalf("kind = %d, want LedgerInitialized", genesisRecord.Event().Kind())
	}
	if _, hasPrev := genesisRecord.Event().PreviousRecordDigest(); hasPrev {
		t.Fatal("genesis record has previous record digest, want none")
	}

	// Check that head in store matches genesis
	head, err := store.Head(ctx, ledgerID)
	if err != nil {
		t.Fatalf("store.Head: %v", err)
	}
	headDigest, ok := head.LastRecordDigest()
	if !ok || headDigest != genesisRecord.RecordDigest() {
		t.Fatalf("head digest = (%x, %t), want (%x, true)", headDigest, ok, genesisRecord.RecordDigest())
	}

	// Test that re-running InitLedger on an existing ledger fails with ErrLedgerAlreadyInitialized
	_, err = svc.InitLedger(ctx, ledgerID)
	if err == nil {
		t.Fatal("duplicate InitLedger error = nil, want ErrLedgerAlreadyInitialized")
	}
	if !errors.Is(err, ErrLedgerAlreadyInitialized) {
		t.Fatalf("duplicate InitLedger error = %v, want ErrLedgerAlreadyInitialized", err)
	}
}

func TestAdmitContinuationRecords(t *testing.T) {
	t.Parallel()

	svc, store := openTestAdmissionService(t)
	ledgerID := newTestLedgerID(t)
	ctx := context.Background()

	genesis, err := svc.InitLedger(ctx, ledgerID)
	if err != nil {
		t.Fatalf("InitLedger: %v", err)
	}

	payload1 := []byte{0xa1, 0x00, 0x01} // {0: 1}
	rec1, err := svc.Admit(ctx, AdmitRequest{
		LedgerID:       ledgerID,
		EventKind:      ledgerv1.EventKind(10),
		PayloadVersion: 1,
		PayloadBytes:   payload1,
	})
	if err != nil {
		t.Fatalf("Admit seq 2: %v", err)
	}

	if rec1.Event().Sequence() != 2 {
		t.Fatalf("rec1 sequence = %d, want 2", rec1.Event().Sequence())
	}
	prevDigest, ok := rec1.Event().PreviousRecordDigest()
	if !ok || prevDigest != genesis.RecordDigest() {
		t.Fatalf("rec1 previous digest = (%x, %t), want (%x, true)", prevDigest, ok, genesis.RecordDigest())
	}

	payload2 := []byte{0xa1, 0x00, 0x02} // {0: 2}
	rec2, err := svc.Admit(ctx, AdmitRequest{
		LedgerID:       ledgerID,
		EventKind:      ledgerv1.EventKind(11),
		PayloadVersion: 1,
		PayloadBytes:   payload2,
	})
	if err != nil {
		t.Fatalf("Admit seq 3: %v", err)
	}

	if rec2.Event().Sequence() != 3 {
		t.Fatalf("rec2 sequence = %d, want 3", rec2.Event().Sequence())
	}
	prevDigest2, ok := rec2.Event().PreviousRecordDigest()
	if !ok || prevDigest2 != rec1.RecordDigest() {
		t.Fatalf("rec2 previous digest = (%x, %t), want (%x, true)", prevDigest2, ok, rec1.RecordDigest())
	}

	// Verify head in database is rec2
	head, err := store.Head(ctx, ledgerID)
	if err != nil {
		t.Fatalf("store.Head: %v", err)
	}
	headDigest, ok := head.LastRecordDigest()
	if !ok || headDigest != rec2.RecordDigest() {
		t.Fatalf("head digest = (%x, %t), want (%x, true)", headDigest, ok, rec2.RecordDigest())
	}
}

func TestConcurrentAdmitContentionResolves(t *testing.T) {
	t.Parallel()

	svc, store := openTestAdmissionService(t)
	ledgerID := newTestLedgerID(t)
	ctx := context.Background()

	if _, err := svc.InitLedger(ctx, ledgerID); err != nil {
		t.Fatalf("InitLedger: %v", err)
	}

	const numWorkers = 5
	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		workerID := byte(i + 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := []byte{0xa1, 0x00, workerID}
			_, err := svc.Admit(ctx, AdmitRequest{
				LedgerID:       ledgerID,
				EventKind:      ledgerv1.EventKind(20),
				PayloadVersion: 1,
				PayloadBytes:   payload,
			})
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Admit error: %v", err)
		}
	}

	head, err := store.Head(ctx, ledgerID)
	if err != nil {
		t.Fatalf("store.Head: %v", err)
	}
	expectedFinalSeq := uint64(1 + numWorkers)
	if head.LastSequence() != expectedFinalSeq {
		t.Fatalf("final sequence = %d, want %d", head.LastSequence(), expectedFinalSeq)
	}
}

func TestAdmitEndorsementAndRevocation(t *testing.T) {
	t.Parallel()

	svc, store := openTestAdmissionService(t)
	ledgerID := newTestLedgerID(t)
	ctx := context.Background()

	if _, err := svc.InitLedger(ctx, ledgerID); err != nil {
		t.Fatalf("InitLedger: %v", err)
	}

	proposerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("proposer key: %v", err)
	}
	subjectKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("subject key: %v", err)
	}

	proposedAt := uint64(1_735_689_600_000)
	acceptedAt := uint64(1_735_689_700_000)
	topic := "engineering:ai-systems"
	claimBody := []byte{0xa1, 0x65, 's', 'c', 'o', 'r', 'e', 0x0a}

	proposal, err := endorsementv1.NewProposal(&proposerKey.PublicKey, &subjectKey.PublicKey, topic, claimBody, proposedAt)
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	proposerSig, err := proposal.Sign(proposerKey)
	if err != nil {
		t.Fatalf("proposal.Sign: %v", err)
	}

	acceptance, err := endorsementv1.NewAcceptance(proposal.Digest(), acceptedAt)
	if err != nil {
		t.Fatalf("NewAcceptance: %v", err)
	}
	subjectSig, err := acceptance.Sign(subjectKey)
	if err != nil {
		t.Fatalf("acceptance.Sign: %v", err)
	}

	acceptedPayload, err := endorsementv1.NewAcceptedPayload(
		&proposerKey.PublicKey,
		&subjectKey.PublicKey,
		topic,
		claimBody,
		proposedAt,
		proposerSig,
		acceptedAt,
		subjectSig,
	)
	if err != nil {
		t.Fatalf("NewAcceptedPayload: %v", err)
	}

	// Admit endorsement
	admittedRecord, err := svc.AdmitEndorsement(ctx, ledgerID, acceptedPayload)
	if err != nil {
		t.Fatalf("AdmitEndorsement: %v", err)
	}

	if admittedRecord.Event().Kind() != ledgerv1.EventKindEndorsementAccepted {
		t.Fatalf("kind = %d, want EndorsementAccepted", admittedRecord.Event().Kind())
	}
	if admittedRecord.Event().Sequence() != 2 {
		t.Fatalf("sequence = %d, want 2", admittedRecord.Event().Sequence())
	}

	// Verify head in database
	head, err := store.Head(ctx, ledgerID)
	if err != nil {
		t.Fatalf("store.Head: %v", err)
	}
	if head.LastSequence() != 2 {
		t.Fatalf("head sequence = %d, want 2", head.LastSequence())
	}

	// Admit revocation by proposer
	revokedAt := uint64(1_735_689_800_000)
	revocation, err := endorsementv1.NewRevocation(
		admittedRecord.RecordDigest(),
		&proposerKey.PublicKey,
		endorsementv1.RevokerRoleProposer,
		revokedAt,
		"project deprecated",
	)
	if err != nil {
		t.Fatalf("NewRevocation: %v", err)
	}
	revokerSig, err := revocation.Sign(proposerKey)
	if err != nil {
		t.Fatalf("revocation.Sign: %v", err)
	}

	revokedPayload, err := endorsementv1.NewRevokedPayload(
		admittedRecord.RecordDigest(),
		&proposerKey.PublicKey,
		endorsementv1.RevokerRoleProposer,
		revokedAt,
		"project deprecated",
		revokerSig,
	)
	if err != nil {
		t.Fatalf("NewRevokedPayload: %v", err)
	}

	revocationRecord, err := svc.AdmitRevocation(ctx, ledgerID, revokedPayload)
	if err != nil {
		t.Fatalf("AdmitRevocation: %v", err)
	}

	if revocationRecord.Event().Kind() != ledgerv1.EventKindEndorsementRevoked {
		t.Fatalf("kind = %d, want EndorsementRevoked", revocationRecord.Event().Kind())
	}
	if revocationRecord.Event().Sequence() != 3 {
		t.Fatalf("sequence = %d, want 3", revocationRecord.Event().Sequence())
	}

	// Verify head in database is now seq 3
	headAfterRev, err := store.Head(ctx, ledgerID)
	if err != nil {
		t.Fatalf("store.Head: %v", err)
	}
	if headAfterRev.LastSequence() != 3 {
		t.Fatalf("head sequence = %d, want 3", headAfterRev.LastSequence())
	}
}

package graphv1

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"math"
	"os"
	"testing"

	"github.com/JakeFAU/chain-application/internal/admission"
	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/JakeFAU/chain-application/internal/ledgerstore"
	"github.com/JakeFAU/chain-application/internal/signer"
)

const testDatabaseURLEnvironment = "CHAIN_TEST_DATABASE_URL"

func openTestAdmissionService(t *testing.T) (*admission.Service, *ledgerstore.Store) {
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

	localSigner, err := signer.GenerateLocalSigner("local:test:graph:signer:v1")
	if err != nil {
		t.Fatalf("GenerateLocalSigner: %v", err)
	}

	svc, err := admission.New(store, localSigner)
	if err != nil {
		t.Fatalf("admission.New: %v", err)
	}
	return svc, store
}

func TestIdentityKeyValidation(t *testing.T) {
	t.Parallel()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := IdentityKeyFromPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("IdentityKeyFromPublicKey: %v", err)
	}

	if len(key.Bytes()) != 65 {
		t.Fatalf("len(key.Bytes()) = %d, want 65", len(key.Bytes()))
	}
	if key.Bytes()[0] != 0x04 {
		t.Fatalf("key.Bytes()[0] = 0x%02x, want 0x04", key.Bytes()[0])
	}

	parsedPub, err := key.PublicKey()
	if err != nil {
		t.Fatalf("key.PublicKey(): %v", err)
	}
	if !parsedPub.Equal(&priv.PublicKey) {
		t.Fatal("parsed public key does not equal original")
	}

	// Hex roundtrip
	hexStr := key.Hex()
	keyFromHex, err := IdentityKeyFromHex(hexStr)
	if err != nil {
		t.Fatalf("IdentityKeyFromHex: %v", err)
	}
	if keyFromHex != key {
		t.Fatal("keyFromHex != key")
	}

	// Error cases
	if _, err := IdentityKeyFromBytes([]byte{0x04, 0x01}); err == nil {
		t.Fatal("IdentityKeyFromBytes with short slice succeeded, want error")
	}
	invalidPrefix := key.Bytes()
	invalidPrefix[0] = 0x02 // compressed format
	if _, err := IdentityKeyFromBytes(invalidPrefix); err == nil {
		t.Fatal("IdentityKeyFromBytes with compressed prefix succeeded, want error")
	}
}

func TestProjectorReplayDeterminismAndConfidence(t *testing.T) {
	t.Parallel()

	admSvc, store := openTestAdmissionService(t)
	ledgerID := newRandomLedgerID(t)

	// Generate 4 actor identities: Alice -> Bob -> Charlie -> Dave
	aliceKey := generateActorKey(t)
	bobKey := generateActorKey(t)
	charlieKey := generateActorKey(t)
	daveKey := generateActorKey(t)

	aliceID := mustIdentityKey(t, &aliceKey.PublicKey)
	bobID := mustIdentityKey(t, &bobKey.PublicKey)
	charlieID := mustIdentityKey(t, &charlieKey.PublicKey)
	daveID := mustIdentityKey(t, &daveKey.PublicKey)

	// 1. Genesis
	genesisRec, err := admSvc.InitLedger(context.Background(), ledgerID)
	if err != nil {
		t.Fatalf("InitLedger: %v", err)
	}

	// 2. Alice endorses Bob on "ai:alignment"
	abPayload := createTestEndorsementPayload(t, aliceKey, bobKey, "ai:alignment", []byte("claim-ab"), 1_735_689_610_000, 1_735_689_620_000)
	abRec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, abPayload)
	if err != nil {
		t.Fatalf("AdmitEndorsement AB: %v", err)
	}

	// 3. Bob endorses Charlie on "ai:alignment"
	bcPayload := createTestEndorsementPayload(t, bobKey, charlieKey, "ai:alignment", []byte("claim-bc"), 1_735_689_640_000, 1_735_689_650_000)
	bcRec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, bcPayload)
	if err != nil {
		t.Fatalf("AdmitEndorsement BC: %v", err)
	}

	// 4. Charlie endorses Dave on "ai:alignment"
	cdPayload := createTestEndorsementPayload(t, charlieKey, daveKey, "ai:alignment", []byte("claim-cd"), 1_735_689_670_000, 1_735_689_680_000)
	cdRec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, cdPayload)
	if err != nil {
		t.Fatalf("AdmitEndorsement CD: %v", err)
	}

	// Apply into Projector
	projector := NewProjector(ledgerID)
	for _, rec := range []ledgerv1.StructuralRecord{genesisRec, abRec, bcRec, cdRec} {
		if err := projector.ApplyRecord(rec); err != nil {
			t.Fatalf("ApplyRecord: %v", err)
		}
	}

	graph := projector.Graph()
	if graph.NodeCount() != 4 {
		t.Fatalf("NodeCount = %d, want 4", graph.NodeCount())
	}
	if graph.ActiveEdgeCount() != 3 {
		t.Fatalf("ActiveEdgeCount = %d, want 3", graph.ActiveEdgeCount())
	}

	// Evaluate confidence from Alice (weight 1.0) to Dave
	evaluator := NewEvaluator(graph)
	policy := NewDefaultPolicy(map[IdentityKey]float64{aliceID: 1.0}, "ai:alignment")
	policy.DecayFactor = 0.6
	policy.MaxHops = 3

	res, err := evaluator.Evaluate(policy, daveID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Expected score: 1.0 * 0.6 * 0.6 * 0.6 = 0.216
	expectedScore := 0.216
	if math.Abs(res.ConfidenceScore-expectedScore) > 1e-6 {
		t.Fatalf("ConfidenceScore = %f, want %f", res.ConfidenceScore, expectedScore)
	}
	if len(res.ContributingPaths) != 1 {
		t.Fatalf("len(ContributingPaths) = %d, want 1", len(res.ContributingPaths))
	}
	if len(res.ContributingRecords) != 3 {
		t.Fatalf("len(ContributingRecords) = %d, want 3", len(res.ContributingRecords))
	}
	if len(res.ProvenanceSubgraph.Nodes) != 4 {
		t.Fatalf("len(ProvenanceSubgraph.Nodes) = %d, want 4", len(res.ProvenanceSubgraph.Nodes))
	}

	// Check topic isolation: query on unrelated topic "quantum-computing"
	unrelatedPolicy := NewDefaultPolicy(map[IdentityKey]float64{aliceID: 1.0}, "quantum-computing")
	unrelatedRes, err := evaluator.Evaluate(unrelatedPolicy, daveID)
	if err != nil {
		t.Fatalf("Evaluate unrelated topic: %v", err)
	}
	if unrelatedRes.ConfidenceScore != 0.0 {
		t.Fatalf("unrelated topic score = %f, want 0.0", unrelatedRes.ConfidenceScore)
	}

	// Test Replay Invariance from PostgreSQL store: wipe and re-project using ReplayFromStore
	replayedProjector := NewProjector(ledgerID)
	applied, err := replayedProjector.ReplayFromStore(context.Background(), store, 2)
	if err != nil {
		t.Fatalf("ReplayFromStore: %v", err)
	}
	if applied != 4 {
		t.Fatalf("applied = %d, want 4", applied)
	}

	replayedRes, err := NewEvaluator(replayedProjector.Graph()).Evaluate(policy, daveID)
	if err != nil {
		t.Fatalf("replayed Evaluate: %v", err)
	}
	if replayedRes.ConfidenceScore != res.ConfidenceScore {
		t.Fatalf("replayed ConfidenceScore = %f, original = %f", replayedRes.ConfidenceScore, res.ConfidenceScore)
	}
	_ = bobID
	_ = charlieID
}

func TestRevocationDropsConfidence(t *testing.T) {
	t.Parallel()

	admSvc, _ := openTestAdmissionService(t)
	ledgerID := newRandomLedgerID(t)

	aliceKey := generateActorKey(t)
	bobKey := generateActorKey(t)
	aliceID := mustIdentityKey(t, &aliceKey.PublicKey)
	bobID := mustIdentityKey(t, &bobKey.PublicKey)

	// Genesis
	genesisRec, err := admSvc.InitLedger(context.Background(), ledgerID)
	if err != nil {
		t.Fatal(err)
	}

	// Endorsement Alice -> Bob
	abPayload := createTestEndorsementPayload(t, aliceKey, bobKey, "security", []byte("claim"), 1_735_689_610_000, 1_735_689_620_000)
	abRec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, abPayload)
	if err != nil {
		t.Fatal(err)
	}

	projector := NewProjector(ledgerID)
	_ = projector.ApplyRecord(genesisRec)
	_ = projector.ApplyRecord(abRec)

	evaluator := NewEvaluator(projector.Graph())
	policy := NewDefaultPolicy(map[IdentityKey]float64{aliceID: 1.0}, "security")
	policy.DecayFactor = 0.6

	resBefore, err := evaluator.Evaluate(policy, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(resBefore.ConfidenceScore-0.6) > 1e-6 {
		t.Fatalf("score before revocation = %f, want 0.6", resBefore.ConfidenceScore)
	}

	// Revoke Endorsement by Proposer Alice
	revocation, err := endorsementv1.NewRevocation(
		abRec.RecordDigest(),
		&aliceKey.PublicKey,
		endorsementv1.RevokerRoleProposer,
		1_735_689_700_000,
		"no longer valid",
	)
	if err != nil {
		t.Fatal(err)
	}
	revokerSig, err := revocation.Sign(aliceKey)
	if err != nil {
		t.Fatal(err)
	}
	revokedPayload, err := endorsementv1.NewRevokedPayload(
		abRec.RecordDigest(),
		&aliceKey.PublicKey,
		endorsementv1.RevokerRoleProposer,
		1_735_689_700_000,
		"no longer valid",
		revokerSig,
	)
	if err != nil {
		t.Fatal(err)
	}

	revRec, err := admSvc.AdmitRevocation(context.Background(), ledgerID, revokedPayload)
	if err != nil {
		t.Fatal(err)
	}

	if err := projector.ApplyRecord(revRec); err != nil {
		t.Fatalf("ApplyRecord revocation: %v", err)
	}

	// Re-evaluate after revocation
	resAfter, err := evaluator.Evaluate(policy, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if resAfter.ConfidenceScore != 0.0 {
		t.Fatalf("score after revocation = %f, want 0.0", resAfter.ConfidenceScore)
	}
	if len(resAfter.ContributingPaths) != 0 {
		t.Fatalf("contributing paths = %d, want 0", len(resAfter.ContributingPaths))
	}
}

func TestMultiPathConvergenceAndCycleResistance(t *testing.T) {
	t.Parallel()

	admSvc, _ := openTestAdmissionService(t)
	ledgerID := newRandomLedgerID(t)

	// 2 Trust roots (R1, R2) and a shared Subject (Target)
	r1Key := generateActorKey(t)
	r2Key := generateActorKey(t)
	targetKey := generateActorKey(t)

	r1ID := mustIdentityKey(t, &r1Key.PublicKey)
	r2ID := mustIdentityKey(t, &r2Key.PublicKey)
	targetID := mustIdentityKey(t, &targetKey.PublicKey)

	genesisRec, err := admSvc.InitLedger(context.Background(), ledgerID)
	if err != nil {
		t.Fatal(err)
	}

	// R1 -> Target (weight 0.6)
	r1Payload := createTestEndorsementPayload(t, r1Key, targetKey, "crypto", []byte("c1"), 1_735_689_610_000, 1_735_689_620_000)
	r1Rec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, r1Payload)
	if err != nil {
		t.Fatal(err)
	}

	// R2 -> Target (weight 0.6)
	r2Payload := createTestEndorsementPayload(t, r2Key, targetKey, "crypto", []byte("c2"), 1_735_689_640_000, 1_735_689_650_000)
	r2Rec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, r2Payload)
	if err != nil {
		t.Fatal(err)
	}

	// Add a cycle: Target -> R1 (cycle should be skipped by traversal)
	cyclePayload := createTestEndorsementPayload(t, targetKey, r1Key, "crypto", []byte("c3"), 1_735_689_670_000, 1_735_689_680_000)
	cycleRec, err := admSvc.AdmitEndorsement(context.Background(), ledgerID, cyclePayload)
	if err != nil {
		t.Fatal(err)
	}

	projector := NewProjector(ledgerID)
	for _, rec := range []ledgerv1.StructuralRecord{genesisRec, r1Rec, r2Rec, cycleRec} {
		if err := projector.ApplyRecord(rec); err != nil {
			t.Fatalf("ApplyRecord: %v", err)
		}
	}

	evaluator := NewEvaluator(projector.Graph())
	policy := NewDefaultPolicy(map[IdentityKey]float64{
		r1ID: 1.0,
		r2ID: 1.0,
	}, "crypto")
	policy.DecayFactor = 0.6

	res, err := evaluator.Evaluate(policy, targetID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Two independent paths: P1 = 0.6, P2 = 0.6
	// Combined score = 1 - (1 - 0.6)*(1 - 0.6) = 1 - 0.16 = 0.84
	expectedScore := 0.84
	if math.Abs(res.ConfidenceScore-expectedScore) > 1e-6 {
		t.Fatalf("ConfidenceScore = %f, want %f", res.ConfidenceScore, expectedScore)
	}
	if len(res.ContributingPaths) != 2 {
		t.Fatalf("len(ContributingPaths) = %d, want 2", len(res.ContributingPaths))
	}
}

func generateActorKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustIdentityKey(t *testing.T, pub *ecdsa.PublicKey) IdentityKey {
	t.Helper()
	k, err := IdentityKeyFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newRandomLedgerID(t *testing.T) ledgerv1.LedgerID {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return ledgerv1.LedgerID(sha256.Sum256(b[:]))
}

func createTestEndorsementPayload(
	t *testing.T,
	proposerKey *ecdsa.PrivateKey,
	subjectKey *ecdsa.PrivateKey,
	topic string,
	claimBody []byte,
	proposedAt uint64,
	acceptedAt uint64,
) *endorsementv1.EndorsementAcceptedPayload {
	t.Helper()

	proposal, err := endorsementv1.NewProposal(&proposerKey.PublicKey, &subjectKey.PublicKey, topic, claimBody, proposedAt)
	if err != nil {
		t.Fatal(err)
	}
	proposerSig, err := proposal.Sign(proposerKey)
	if err != nil {
		t.Fatal(err)
	}

	acceptance, err := endorsementv1.NewAcceptance(proposal.Digest(), acceptedAt)
	if err != nil {
		t.Fatal(err)
	}
	subjectSig, err := acceptance.Sign(subjectKey)
	if err != nil {
		t.Fatal(err)
	}

	payload, err := endorsementv1.NewAcceptedPayload(
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
		t.Fatal(err)
	}
	return payload
}

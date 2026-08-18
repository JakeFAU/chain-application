package endorsementv1

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"
)

func generateTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

func TestEndorsementLifecycle(t *testing.T) {
	t.Parallel()

	proposerKey := generateTestKey(t)
	subjectKey := generateTestKey(t)

	proposedAt := uint64(time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC).UnixMilli())
	acceptedAt := uint64(time.Date(2026, 8, 17, 10, 30, 0, 0, time.UTC).UnixMilli())
	topic := "engineering:distributed-systems"
	claimBody := []byte{0xa1, 0x64, 'r', 'a', 'n', 'k', 0x05} // {"rank": 5}

	// 1. Proposer creates and signs proposal
	proposal, err := NewProposal(
		&proposerKey.PublicKey,
		&subjectKey.PublicKey,
		topic,
		claimBody,
		proposedAt,
	)
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}

	proposerSig, err := proposal.Sign(proposerKey)
	if err != nil {
		t.Fatalf("proposal.Sign: %v", err)
	}

	// Verify proposal standalone
	if err := proposal.VerifySignature(proposerSig); err != nil {
		t.Fatalf("VerifySignature on proposal: %v", err)
	}

	// 2. Subject accepts and signs
	acceptance, err := NewAcceptance(proposal.Digest(), acceptedAt)
	if err != nil {
		t.Fatalf("NewAcceptance: %v", err)
	}

	subjectSig, err := acceptance.Sign(subjectKey)
	if err != nil {
		t.Fatalf("acceptance.Sign: %v", err)
	}

	if err := acceptance.VerifySignature(&subjectKey.PublicKey, subjectSig); err != nil {
		t.Fatalf("VerifySignature on acceptance: %v", err)
	}

	// 3. Assemble accepted payload
	acceptedPayload, err := NewAcceptedPayload(
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

	// 4. Encode to canonical CBOR and decode/verify
	encodedBytes, err := acceptedPayload.Encode()
	if err != nil {
		t.Fatalf("Encode accepted payload: %v", err)
	}

	decodedPayload, err := DecodeAcceptedPayload(encodedBytes)
	if err != nil {
		t.Fatalf("DecodeAcceptedPayload: %v", err)
	}

	if err := decodedPayload.Verify(); err != nil {
		t.Fatalf("decodedPayload.Verify: %v", err)
	}

	// 5. Test Revocation by Proposer
	targetRecordDigest := [32]byte{0xaa, 0xbb, 0xcc}
	revokedAt := uint64(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC).UnixMilli())
	revocation, err := NewRevocation(
		targetRecordDigest,
		&proposerKey.PublicKey,
		RevokerRoleProposer,
		revokedAt,
		"retracted by endorser",
	)
	if err != nil {
		t.Fatalf("NewRevocation: %v", err)
	}

	revokerSig, err := revocation.Sign(proposerKey)
	if err != nil {
		t.Fatalf("revocation.Sign: %v", err)
	}

	revokedPayload, err := NewRevokedPayload(
		targetRecordDigest,
		&proposerKey.PublicKey,
		RevokerRoleProposer,
		revokedAt,
		"retracted by endorser",
		revokerSig,
	)
	if err != nil {
		t.Fatalf("NewRevokedPayload: %v", err)
	}

	revEncoded, err := revokedPayload.Encode()
	if err != nil {
		t.Fatalf("Encode revoked payload: %v", err)
	}

	decodedRev, err := DecodeRevokedPayload(revEncoded)
	if err != nil {
		t.Fatalf("DecodeRevokedPayload: %v", err)
	}

	if err := decodedRev.Verify(); err != nil {
		t.Fatalf("decodedRev.Verify: %v", err)
	}
}

func TestEndorsementSignatureForgeryDetection(t *testing.T) {
	t.Parallel()

	proposerKey := generateTestKey(t)
	subjectKey := generateTestKey(t)
	attackerKey := generateTestKey(t)

	proposedAt := uint64(1_735_689_600_000)
	acceptedAt := uint64(1_735_689_700_000)
	topic := "security:cryptography"
	claimBody := []byte{0xa1, 0x64, 't', 'e', 's', 't', 0x01}

	proposal, err := NewProposal(&proposerKey.PublicKey, &subjectKey.PublicKey, topic, claimBody, proposedAt)
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	validProposerSig, err := proposal.Sign(proposerKey)
	if err != nil {
		t.Fatalf("proposal.Sign: %v", err)
	}

	acceptance, err := NewAcceptance(proposal.Digest(), acceptedAt)
	if err != nil {
		t.Fatalf("NewAcceptance: %v", err)
	}
	validSubjectSig, err := acceptance.Sign(subjectKey)
	if err != nil {
		t.Fatalf("acceptance.Sign: %v", err)
	}

	t.Run("forged proposer signature", func(t *testing.T) {
		t.Parallel()
		forgedProposerSig, err := proposal.Sign(attackerKey)
		if err != nil {
			t.Fatalf("forged sign: %v", err)
		}
		payload, err := NewAcceptedPayload(
			&proposerKey.PublicKey,
			&subjectKey.PublicKey,
			topic,
			claimBody,
			proposedAt,
			forgedProposerSig,
			acceptedAt,
			validSubjectSig,
		)
		if err != nil {
			t.Fatalf("NewAcceptedPayload: %v", err)
		}
		if err := payload.Verify(); err == nil {
			t.Fatal("Verify() with forged proposer signature succeeded, want error")
		}
	})

	t.Run("forged subject signature", func(t *testing.T) {
		t.Parallel()
		forgedSubjectSig, err := acceptance.Sign(attackerKey)
		if err != nil {
			t.Fatalf("forged sign: %v", err)
		}
		payload, err := NewAcceptedPayload(
			&proposerKey.PublicKey,
			&subjectKey.PublicKey,
			topic,
			claimBody,
			proposedAt,
			validProposerSig,
			acceptedAt,
			forgedSubjectSig,
		)
		if err != nil {
			t.Fatalf("NewAcceptedPayload: %v", err)
		}
		if err := payload.Verify(); err == nil {
			t.Fatal("Verify() with forged subject signature succeeded, want error")
		}
	})

	t.Run("tampered claim body", func(t *testing.T) {
		t.Parallel()
		tamperedClaimBody := []byte{0xa1, 0x64, 't', 'e', 's', 't', 0x99}
		payload, err := NewAcceptedPayload(
			&proposerKey.PublicKey,
			&subjectKey.PublicKey,
			topic,
			tamperedClaimBody,
			proposedAt,
			validProposerSig,
			acceptedAt,
			validSubjectSig,
		)
		if err != nil {
			t.Fatalf("NewAcceptedPayload: %v", err)
		}
		if err := payload.Verify(); err == nil {
			t.Fatal("Verify() with tampered claim body succeeded, want error")
		}
	})
}

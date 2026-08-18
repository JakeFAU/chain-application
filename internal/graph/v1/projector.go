package graphv1

import (
	"context"
	"errors"
	"fmt"

	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/JakeFAU/chain-application/internal/ledgerstore"
)

var (
	ErrLedgerMismatch   = errors.New("record ledger ID does not match graph ledger ID")
	ErrSequenceBreak    = errors.New("record sequence is not strictly continuous")
	ErrUnsupportedEvent = errors.New("unsupported event kind for graph projection")
)

// Projector consumes an ordered stream of ledger records to mutate and maintain the Graph projection.
type Projector struct {
	graph *Graph
}

// NewProjector creates a Projector with a new Graph for a ledger.
func NewProjector(ledgerID ledgerv1.LedgerID) *Projector {
	return &Projector{
		graph: NewGraph(ledgerID),
	}
}

// NewProjectorWithGraph wraps an existing Graph.
func NewProjectorWithGraph(graph *Graph) *Projector {
	return &Projector{
		graph: graph,
	}
}

// Graph returns the underlying Graph projection.
func (p *Projector) Graph() *Graph {
	return p.graph
}

// ApplyRecord validates sequence continuity and projects a single record into the graph.
func (p *Projector) ApplyRecord(record ledgerv1.StructuralRecord) error {
	event := record.Event()
	ledgerID := event.LedgerID()
	if ledgerID != p.graph.LedgerID() {
		return fmt.Errorf("%w: record has %x, graph has %x", ErrLedgerMismatch, ledgerID, p.graph.LedgerID())
	}

	seq := event.Sequence()
	lastSeq := p.graph.LastSequence()

	if lastSeq == 0 {
		if seq != 1 {
			return fmt.Errorf("%w: initial record sequence must be 1, got %d", ErrSequenceBreak, seq)
		}
	} else {
		if seq != lastSeq+1 {
			return fmt.Errorf("%w: expected sequence %d, got %d", ErrSequenceBreak, lastSeq+1, seq)
		}
	}

	switch event.Kind() {
	case ledgerv1.EventKindLedgerInitialized:
		p.graph.SetLastSequence(seq)
		return nil

	case ledgerv1.EventKindEndorsementAccepted:
		payload, err := endorsementv1.DecodeAcceptedPayload(event.PayloadBytes())
		if err != nil {
			return fmt.Errorf("decode accepted payload for record %x: %w", record.RecordDigest(), err)
		}

		proposerKey, err := IdentityKeyFromPublicKey(payload.ProposerPublicKey)
		if err != nil {
			return fmt.Errorf("invalid proposer public key: %w", err)
		}
		subjectKey, err := IdentityKeyFromPublicKey(payload.SubjectPublicKey)
		if err != nil {
			return fmt.Errorf("invalid subject public key: %w", err)
		}

		edge := NewAttestationEdge(
			record.RecordDigest(),
			seq,
			proposerKey,
			subjectKey,
			payload.Topic,
			payload.ClaimBodyBytes,
			payload.ProposedAtUnixMS,
			payload.AcceptedAtUnixMS,
		)

		return p.graph.AddEdge(edge)

	case ledgerv1.EventKindEndorsementRevoked:
		payload, err := endorsementv1.DecodeRevokedPayload(event.PayloadBytes())
		if err != nil {
			return fmt.Errorf("decode revoked payload for record %x: %w", record.RecordDigest(), err)
		}

		revokerKey, err := IdentityKeyFromPublicKey(payload.RevokerPublicKey)
		if err != nil {
			return fmt.Errorf("invalid revoker public key: %w", err)
		}

		return p.graph.RevokeEdge(
			payload.TargetRecordDigest,
			revokerKey,
			payload.Role,
			payload.RevokedAtUnixMS,
			record.RecordDigest(),
			payload.Reason,
			seq,
		)

	default:
		// For future extensible event kinds that do not mutate attestation edges,
		// advance sequence watermark without failing.
		p.graph.SetLastSequence(seq)
		return nil
	}
}

// ReplayFromStore streams unapplied records from PostgreSQL starting from lastSequence + 1.
func (p *Projector) ReplayFromStore(ctx context.Context, store *ledgerstore.Store, batchSize int) (int, error) {
	if store == nil {
		return 0, errors.New("store cannot be nil")
	}
	if batchSize <= 0 {
		batchSize = 256
	}

	totalApplied := 0
	for {
		startSeq := p.graph.LastSequence() + 1
		records, err := store.ScanRecords(ctx, p.graph.LedgerID(), startSeq, batchSize)
		if err != nil {
			return totalApplied, fmt.Errorf("scan records at sequence %d: %w", startSeq, err)
		}
		if len(records) == 0 {
			break
		}

		for _, record := range records {
			if err := p.ApplyRecord(record); err != nil {
				return totalApplied, fmt.Errorf("apply record %x at sequence %d: %w", record.RecordDigest(), record.Event().Sequence(), err)
			}
			totalApplied++
		}

		if len(records) < batchSize {
			break
		}
	}

	return totalApplied, nil
}

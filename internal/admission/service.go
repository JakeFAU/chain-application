package admission

import (
	"context"
	"errors"
	"fmt"
	"time"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/JakeFAU/chain-application/internal/ledgerstore"
	"github.com/JakeFAU/chain-application/internal/signer"
)

const (
	defaultMaxRetries = 3
)

// ErrLedgerAlreadyInitialized is returned when attempting to initialize a ledger that already exists.
var ErrLedgerAlreadyInitialized = errors.New("ledger already initialized")

// Clock returns the current time.
type Clock func() time.Time

// Service coordinates the ordering, signing, and persistence of ledger admission records.
type Service struct {
	store      *ledgerstore.Store
	signer     signer.Signer
	clock      Clock
	maxRetries int
}

// Option configures a Service.
type Option func(*Service)

// WithClock overrides the default time provider.
func WithClock(clock Clock) Option {
	return func(s *Service) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// WithMaxRetries overrides the maximum retry count on sequence contention.
func WithMaxRetries(maxRetries int) Option {
	return func(s *Service) {
		if maxRetries >= 0 {
			s.maxRetries = maxRetries
		}
	}
}

// New creates a new Admission Service.
func New(store *ledgerstore.Store, sysSigner signer.Signer, options ...Option) (*Service, error) {
	if store == nil {
		return nil, errors.New("ledgerstore cannot be nil")
	}
	if sysSigner == nil {
		return nil, errors.New("signer cannot be nil")
	}
	svc := &Service{
		store:      store,
		signer:     sysSigner,
		clock:      time.Now,
		maxRetries: defaultMaxRetries,
	}
	for _, opt := range options {
		opt(svc)
	}
	return svc, nil
}

// InitLedger initializes a brand new ledger with its genesis record.
func (s *Service) InitLedger(ctx context.Context, ledgerID ledgerv1.LedgerID) (ledgerv1.StructuralRecord, error) {
	head, err := s.store.Head(ctx, ledgerID)
	if err != nil {
		return ledgerv1.StructuralRecord{}, fmt.Errorf("read ledger head: %w", err)
	}
	if head.Initialized() {
		return ledgerv1.StructuralRecord{}, ErrLedgerAlreadyInitialized
	}

	nowMS := uint64(s.clock().UTC().UnixMilli())
	genesisEvent, err := ledgerv1.NewGenesisEvent(ledgerID, nowMS)
	if err != nil {
		return ledgerv1.StructuralRecord{}, fmt.Errorf("create genesis event: %w", err)
	}

	eventDigest := genesisEvent.Digest()
	sig, err := s.signer.Sign(ctx, eventDigest[:])
	if err != nil {
		return ledgerv1.StructuralRecord{}, fmt.Errorf("sign genesis event: %w", err)
	}

	record, err := ledgerv1.NewRecord(genesisEvent, s.signer.KeyReference(), sig)
	if err != nil {
		return ledgerv1.StructuralRecord{}, fmt.Errorf("assemble genesis record: %w", err)
	}

	if err := s.store.Append(ctx, record); err != nil {
		if errors.Is(err, ledgerstore.ErrChainHeadMoved) || errors.Is(err, ledgerstore.ErrDuplicateRecord) {
			return ledgerv1.StructuralRecord{}, ErrLedgerAlreadyInitialized
		}
		return ledgerv1.StructuralRecord{}, fmt.Errorf("persist genesis record: %w", err)
	}

	return record, nil
}

// AdmitRequest contains the inputs for admitting an event to the ledger.
type AdmitRequest struct {
	LedgerID       ledgerv1.LedgerID
	EventKind      ledgerv1.EventKind
	PayloadVersion uint64
	PayloadBytes   []byte
}

// Admit orders, signs, and persists a continuation event on the ledger.
func (s *Service) Admit(ctx context.Context, req AdmitRequest) (ledgerv1.StructuralRecord, error) {
	if len(req.PayloadBytes) == 0 {
		return ledgerv1.StructuralRecord{}, errors.New("payload bytes cannot be empty")
	}

	attempts := s.maxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		head, err := s.store.Head(ctx, req.LedgerID)
		if err != nil {
			return ledgerv1.StructuralRecord{}, fmt.Errorf("read ledger head: %w", err)
		}
		if !head.Initialized() {
			return ledgerv1.StructuralRecord{}, errors.New("ledger is uninitialized; call InitLedger first")
		}

		headDigest, _ := head.LastRecordDigest()
		nextSeq := head.LastSequence() + 1
		nowMS := uint64(s.clock().UTC().UnixMilli())

		event, err := ledgerv1.NewEvent(
			req.LedgerID,
			nextSeq,
			&headDigest,
			nowMS,
			req.EventKind,
			req.PayloadVersion,
			req.PayloadBytes,
		)
		if err != nil {
			return ledgerv1.StructuralRecord{}, fmt.Errorf("create continuation event: %w", err)
		}

		eventDigest := event.Digest()
		sig, err := s.signer.Sign(ctx, eventDigest[:])
		if err != nil {
			return ledgerv1.StructuralRecord{}, fmt.Errorf("sign continuation event: %w", err)
		}

		record, err := ledgerv1.NewRecord(event, s.signer.KeyReference(), sig)
		if err != nil {
			return ledgerv1.StructuralRecord{}, fmt.Errorf("assemble record: %w", err)
		}

		appendErr := s.store.Append(ctx, record)
		if appendErr == nil {
			return record, nil
		}

		if errors.Is(appendErr, ledgerstore.ErrDuplicateRecord) {
			existing, readErr := s.store.Record(ctx, record.RecordDigest())
			if readErr == nil {
				return existing, nil
			}
			return record, nil
		}

		if errors.Is(appendErr, ledgerstore.ErrChainHeadMoved) {
			select {
			case <-ctx.Done():
				return ledgerv1.StructuralRecord{}, ctx.Err()
			default:
				continue
			}
		}

		return ledgerv1.StructuralRecord{}, fmt.Errorf("persist record: %w", appendErr)
	}

	return ledgerv1.StructuralRecord{}, fmt.Errorf("admit record exceeded %d retries due to chain head contention", s.maxRetries)
}

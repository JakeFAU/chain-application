package ledgerv1

import "math"

type ChainState struct {
	initialized      bool
	ledgerID         LedgerID
	lastSequence     uint64
	lastRecordDigest Digest
}

func ValidateChainConsistency(state ChainState, record StructuralRecord) (ChainState, error) {
	if len(record.bytes) == 0 {
		return state, wrapValidationError(stageChainConsistency, ErrSchemaViolation)
	}

	event := record.Event()
	if !state.initialized {
		if err := ValidateEventSemantics(event); err != nil {
			return state, err
		}
		return ChainState{
			initialized:      true,
			ledgerID:         event.LedgerID(),
			lastSequence:     event.Sequence(),
			lastRecordDigest: record.RecordDigest(),
		}, nil
	}

	if state.lastSequence == math.MaxUint64 {
		return state, wrapValidationError(stageChainConsistency, ErrInvalidSequence)
	}
	if event.LedgerID() != state.ledgerID {
		return state, wrapValidationError(stageChainConsistency, ErrLedgerMismatch)
	}
	if event.Sequence() != state.lastSequence+1 {
		return state, wrapValidationError(stageChainConsistency, ErrInvalidSequence)
	}
	previousRecordDigest, ok := event.PreviousRecordDigest()
	if !ok || previousRecordDigest != state.lastRecordDigest {
		return state, wrapValidationError(stageChainConsistency, ErrChainLinkMismatch)
	}
	if event.Kind() == EventKindLedgerInitialized {
		return state, wrapValidationError(stageChainConsistency, ErrInvalidSequence)
	}

	return ChainState{
		initialized:      true,
		ledgerID:         state.ledgerID,
		lastSequence:     event.Sequence(),
		lastRecordDigest: record.RecordDigest(),
	}, nil
}

func (state ChainState) Initialized() bool {
	return state.initialized
}

func (state ChainState) LedgerID() (LedgerID, bool) {
	return state.ledgerID, state.initialized
}

func (state ChainState) LastSequence() uint64 {
	return state.lastSequence
}

func (state ChainState) LastRecordDigest() (Digest, bool) {
	return state.lastRecordDigest, state.initialized
}

type ReplayState struct {
	chain ChainState
}

func Apply(state ReplayState, record StructuralRecord) (ReplayState, error) {
	if err := ValidateEventSemantics(record.Event()); err != nil {
		return state, err
	}
	nextChain, err := ValidateChainConsistency(state.chain, record)
	if err != nil {
		return state, err
	}
	return ReplayState{chain: nextChain}, nil
}

func (state ReplayState) ChainState() ChainState {
	return state.chain
}

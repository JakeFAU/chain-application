package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/JakeFAU/chain-application/internal/admission"
	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	graphv1 "github.com/JakeFAU/chain-application/internal/graph/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/JakeFAU/chain-application/internal/ledgerstore"
	"github.com/go-chi/chi/v5"
)

type Server struct {
	admission *admission.Service
	store     *ledgerstore.Store
}

const (
	healthStatus               = "ok"
	invalidRequestMessage      = "invalid request"
	internalServerErrorMessage = "internal server error"
)

var _ StrictServerInterface = (*Server)(nil)

// NewServer creates a new HTTP API Server.
func NewServer(admissionService *admission.Service, store *ledgerstore.Store) *Server {
	return &Server{
		admission: admissionService,
		store:     store,
	}
}

func (*Server) GetHealthz(
	context.Context,
	GetHealthzRequestObject,
) (GetHealthzResponseObject, error) {
	return GetHealthz200JSONResponse{Status: healthStatus}, nil
}

func (s *Server) InitLedger(
	ctx context.Context,
	request InitLedgerRequestObject,
) (InitLedgerResponseObject, error) {
	if s.admission == nil {
		return InitLedger400JSONResponse{Error: "admission service unavailable"}, nil
	}

	ledgerID, err := parseHex32(request.LedgerId)
	if err != nil {
		return InitLedger400JSONResponse{Error: "invalid ledger_id: must be 32-byte hex"}, nil
	}

	record, err := s.admission.InitLedger(ctx, ledgerv1.LedgerID(ledgerID))
	if err != nil {
		if errors.Is(err, admission.ErrLedgerAlreadyInitialized) {
			return InitLedger409JSONResponse{Error: "ledger already initialized"}, nil
		}
		return nil, err
	}

	digest := record.RecordDigest()
	return InitLedger201JSONResponse{
		LedgerId:         hex.EncodeToString(ledgerID[:]),
		SequenceNumber:   int(record.Event().Sequence()),
		RecordDigest:     hex.EncodeToString(digest[:]),
		AdmittedAtUnixMs: int(record.Event().AdmittedAtUnixMS()),
	}, nil
}

func (s *Server) GetLedgerHead(
	ctx context.Context,
	request GetLedgerHeadRequestObject,
) (GetLedgerHeadResponseObject, error) {
	if s.store == nil {
		return GetLedgerHead404JSONResponse{Error: "ledger store unavailable"}, nil
	}

	ledgerID, err := parseHex32(request.LedgerId)
	if err != nil {
		return GetLedgerHead404JSONResponse{Error: "invalid ledger_id: must be 32-byte hex"}, nil
	}

	head, err := s.store.Head(ctx, ledgerv1.LedgerID(ledgerID))
	if err != nil {
		return nil, err
	}
	if !head.Initialized() {
		return GetLedgerHead404JSONResponse{Error: "ledger not found or uninitialized"}, nil
	}

	digest, _ := head.LastRecordDigest()
	return GetLedgerHead200JSONResponse{
		LedgerId:       hex.EncodeToString(ledgerID[:]),
		SequenceNumber: int(head.LastSequence()),
		RecordDigest:   hex.EncodeToString(digest[:]),
	}, nil
}

func (s *Server) GetRecord(
	ctx context.Context,
	request GetRecordRequestObject,
) (GetRecordResponseObject, error) {
	if s.store == nil {
		return GetRecord404JSONResponse{Error: "ledger store unavailable"}, nil
	}

	digest, err := parseHex32(request.RecordDigest)
	if err != nil {
		return GetRecord404JSONResponse{Error: "invalid record_digest: must be 32-byte hex"}, nil
	}

	record, err := s.store.Record(ctx, ledgerv1.Digest(digest))
	if err != nil {
		if errors.Is(err, ledgerstore.ErrRecordNotFound) {
			return GetRecord404JSONResponse{Error: "record not found"}, nil
		}
		return nil, err
	}

	event := record.Event()
	ledgerID := event.LedgerID()
	resp := GetRecord200JSONResponse{
		RecordDigest:       hex.EncodeToString(digest[:]),
		LedgerId:           hex.EncodeToString(ledgerID[:]),
		SequenceNumber:     int(event.Sequence()),
		EventKind:          int(event.Kind()),
		PayloadVersion:     int(event.PayloadVersion()),
		AdmittedAtUnixMs:   int(event.AdmittedAtUnixMS()),
		SignerKeyReference: record.SignerKeyReference(),
		RecordBytesHex:     hex.EncodeToString(record.Bytes()),
	}
	if prevDigest, ok := event.PreviousRecordDigest(); ok {
		prevHex := hex.EncodeToString(prevDigest[:])
		resp.PreviousRecordDigest = &prevHex
	}

	return resp, nil
}

func (s *Server) AcceptEndorsement(
	ctx context.Context,
	request AcceptEndorsementRequestObject,
) (AcceptEndorsementResponseObject, error) {
	if s.admission == nil {
		return AcceptEndorsement400JSONResponse{Error: "admission service unavailable"}, nil
	}

	ledgerID, err := parseHex32(request.LedgerId)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: "invalid ledger_id"}, nil
	}
	if request.Body == nil {
		return AcceptEndorsement400JSONResponse{Error: "missing request body"}, nil
	}

	proposerPub, err := parseHexPublicKey(request.Body.ProposerPublicKeyHex)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: "invalid proposer_public_key_hex"}, nil
	}
	subjectPub, err := parseHexPublicKey(request.Body.SubjectPublicKeyHex)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: "invalid subject_public_key_hex"}, nil
	}
	claimBody, err := hex.DecodeString(request.Body.ClaimBodyHex)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: "invalid claim_body_hex"}, nil
	}
	proposerSig, err := hex.DecodeString(request.Body.ProposerSignatureHex)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: "invalid proposer_signature_hex"}, nil
	}
	subjectSig, err := hex.DecodeString(request.Body.SubjectSignatureHex)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: "invalid subject_signature_hex"}, nil
	}

	payload, err := endorsementv1.NewAcceptedPayload(
		proposerPub,
		subjectPub,
		request.Body.Topic,
		claimBody,
		uint64(request.Body.ProposedAtUnixMs),
		proposerSig,
		uint64(request.Body.AcceptedAtUnixMs),
		subjectSig,
	)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: err.Error()}, nil
	}

	record, err := s.admission.AdmitEndorsement(ctx, ledgerv1.LedgerID(ledgerID), payload)
	if err != nil {
		return AcceptEndorsement400JSONResponse{Error: err.Error()}, nil
	}

	digest := record.RecordDigest()
	return AcceptEndorsement201JSONResponse{
		LedgerId:       hex.EncodeToString(ledgerID[:]),
		SequenceNumber: int(record.Event().Sequence()),
		RecordDigest:   hex.EncodeToString(digest[:]),
		EventKind:      int(record.Event().Kind()),
	}, nil
}

func (s *Server) RevokeEndorsement(
	ctx context.Context,
	request RevokeEndorsementRequestObject,
) (RevokeEndorsementResponseObject, error) {
	if s.admission == nil {
		return RevokeEndorsement400JSONResponse{Error: "admission service unavailable"}, nil
	}

	ledgerID, err := parseHex32(request.LedgerId)
	if err != nil {
		return RevokeEndorsement400JSONResponse{Error: "invalid ledger_id"}, nil
	}
	if request.Body == nil {
		return RevokeEndorsement400JSONResponse{Error: "missing request body"}, nil
	}

	targetDigest, err := parseHex32(request.Body.TargetRecordDigest)
	if err != nil {
		return RevokeEndorsement400JSONResponse{Error: "invalid target_record_digest"}, nil
	}
	revokerPub, err := parseHexPublicKey(request.Body.RevokerPublicKeyHex)
	if err != nil {
		return RevokeEndorsement400JSONResponse{Error: "invalid revoker_public_key_hex"}, nil
	}
	revokerSig, err := hex.DecodeString(request.Body.RevokerSignatureHex)
	if err != nil {
		return RevokeEndorsement400JSONResponse{Error: "invalid revoker_signature_hex"}, nil
	}

	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
	}

	payload, err := endorsementv1.NewRevokedPayload(
		targetDigest,
		revokerPub,
		endorsementv1.RevokerRole(request.Body.Role),
		uint64(request.Body.RevokedAtUnixMs),
		reason,
		revokerSig,
	)
	if err != nil {
		return RevokeEndorsement400JSONResponse{Error: err.Error()}, nil
	}

	record, err := s.admission.AdmitRevocation(ctx, ledgerv1.LedgerID(ledgerID), payload)
	if err != nil {
		return RevokeEndorsement400JSONResponse{Error: err.Error()}, nil
	}

	digest := record.RecordDigest()
	return RevokeEndorsement201JSONResponse{
		LedgerId:       hex.EncodeToString(ledgerID[:]),
		SequenceNumber: int(record.Event().Sequence()),
		RecordDigest:   hex.EncodeToString(digest[:]),
		EventKind:      int(record.Event().Kind()),
	}, nil
}

func (s *Server) EvaluateConfidence(
	ctx context.Context,
	request EvaluateConfidenceRequestObject,
) (EvaluateConfidenceResponseObject, error) {
	if s.store == nil {
		return EvaluateConfidence404JSONResponse{Error: "ledger store unavailable"}, nil
	}

	ledgerID, err := parseHex32(request.LedgerId)
	if err != nil {
		return EvaluateConfidence400JSONResponse{Error: "invalid ledger_id: must be 32-byte hex"}, nil
	}
	if request.Body == nil {
		return EvaluateConfidence400JSONResponse{Error: "missing request body"}, nil
	}

	targetKey, err := graphv1.IdentityKeyFromHex(request.Body.TargetPublicKeyHex)
	if err != nil {
		return EvaluateConfidence400JSONResponse{Error: fmt.Sprintf("invalid target_public_key_hex: %v", err)}, nil
	}

	trustRoots := make(map[graphv1.IdentityKey]float64, len(request.Body.TrustRoots))
	for _, root := range request.Body.TrustRoots {
		rootKey, err := graphv1.IdentityKeyFromHex(root.PublicKeyHex)
		if err != nil {
			return EvaluateConfidence400JSONResponse{Error: fmt.Sprintf("invalid trust root %s: %v", root.PublicKeyHex, err)}, nil
		}
		trustRoots[rootKey] = float64(root.Weight)
	}

	topic := ""
	if request.Body.Topic != nil {
		topic = *request.Body.Topic
	}

	policy := graphv1.NewDefaultPolicy(trustRoots, topic)
	if request.Body.MaxHops != nil {
		policy.MaxHops = *request.Body.MaxHops
	}
	if request.Body.DecayFactor != nil {
		policy.DecayFactor = float64(*request.Body.DecayFactor)
	}

	projector := graphv1.NewProjector(ledgerv1.LedgerID(ledgerID))
	if _, err := projector.ReplayFromStore(ctx, s.store, 256); err != nil {
		return nil, fmt.Errorf("replay ledger for confidence evaluation: %w", err)
	}

	evaluator := graphv1.NewEvaluator(projector.Graph())
	result, err := evaluator.Evaluate(policy, targetKey)
	if err != nil {
		return EvaluateConfidence400JSONResponse{Error: err.Error()}, nil
	}

	contributingHex := make([]string, len(result.ContributingRecords))
	for i, digest := range result.ContributingRecords {
		contributingHex[i] = hex.EncodeToString(digest[:])
	}

	var topicPtr *string
	if result.Topic != "" {
		topicPtr = &result.Topic
	}

	return EvaluateConfidence200JSONResponse{
		TargetPublicKeyHex:     result.Target.Hex(),
		Topic:                  topicPtr,
		ConfidenceScore:        float32(result.ConfidenceScore),
		Algorithm:              result.Algorithm,
		EvaluatedAtSequence:    int(result.EvaluatedAtSequence),
		ContributingRecordsHex: contributingHex,
		Explanation:            result.Explanation,
	}, nil
}

func parseHex32(s string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, err
	}
	if len(b) != 32 {
		return out, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return out, nil
}

func parseHexPublicKey(s string) (*ecdsa.PublicKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b)
}

// NewHandler builds the routed strict API handler and applies wrap once at the
// outside. A nil wrap means no outer instrumentation and is not an error.
func NewHandler(server StrictServerInterface, wrap func(http.Handler) http.Handler) http.Handler {
	router := chi.NewRouter()
	strictHandler := HandlerWithOptions(
		NewStrictHandlerWithOptions(server, nil, strictHTTPServerOptions()),
		chiServerOptions(router),
	)
	if wrap == nil {
		return strictHandler
	}
	return wrap(strictHandler)
}

func chiServerOptions(router chi.Router) ChiServerOptions {
	return ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(response, invalidRequestMessage, http.StatusBadRequest)
		},
	}
}

func strictHTTPServerOptions() StrictHTTPServerOptions {
	return StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(response, invalidRequestMessage, http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(response, internalServerErrorMessage, http.StatusInternalServerError)
		},
	}
}

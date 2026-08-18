package httpapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/JakeFAU/chain-application/internal/admission"
	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
	"github.com/JakeFAU/chain-application/internal/ledgerstore"
	"github.com/JakeFAU/chain-application/internal/signer"
	"github.com/go-chi/chi/v5"
)

const testDatabaseURLEnvironment = "CHAIN_TEST_DATABASE_URL"

func TestHealthzReturnsContractResponse(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	NewHandler(&Server{}, identityWrapper).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var response map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 1 {
		t.Fatalf("response members = %d, want 1", len(response))
	}

	status, ok := response["status"]
	if !ok {
		t.Fatal("response is missing status")
	}

	var value string
	if err := json.Unmarshal(status, &value); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if value != "ok" {
		t.Fatalf("status body = %q, want %q", value, "ok")
	}
}

func TestNewHandlerAppliesOneOuterWrapper(t *testing.T) {
	t.Parallel()

	const wrapperHeader = "X-Test-Outer-Wrapper"
	wrapperCalls := 0
	wrappedRequests := 0
	handler := NewHandler(&Server{}, func(next http.Handler) http.Handler {
		wrapperCalls++
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			wrappedRequests++
			response.Header().Set(wrapperHeader, "applied")
			next.ServeHTTP(response, request)
		})
	})
	if wrapperCalls != 1 {
		t.Fatalf("wrapper construction calls = %d, want 1", wrapperCalls)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "matched route", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "unmatched route", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound},
		{name: "unsupported method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, http.NoBody))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Header().Get(wrapperHeader) != "applied" {
				t.Fatal("outer wrapper did not observe the request")
			}
		})
	}
	if wrappedRequests != len(tests) {
		t.Fatalf("wrapped requests = %d, want %d", wrappedRequests, len(tests))
	}
}

func TestStrictErrorResponsesDoNotExposeInternalErrors(t *testing.T) {
	t.Parallel()

	const sentinel = "private-strict-handler-error-7003b856"
	options := strictHTTPServerOptions()
	tests := []struct {
		name       string
		handle     func(http.ResponseWriter, *http.Request, error)
		wantStatus int
		wantBody   string
	}{
		{
			name:       "request",
			handle:     options.RequestErrorHandlerFunc,
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid request\n",
		},
		{
			name:       "response",
			handle:     options.ResponseErrorHandlerFunc,
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal server error\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
			test.handle(recorder, request, errors.New(sentinel))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Body.String() != test.wantBody {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), test.wantBody)
			}
			if strings.Contains(recorder.Body.String(), sentinel) {
				t.Fatalf("body disclosed sentinel: %q", recorder.Body.String())
			}
		})
	}
}

func TestChiServerOptionsErrorHandlerDoesNotExposeInternalErrors(t *testing.T) {
	t.Parallel()

	const sentinel = "private-chi-parser-error-4710bc82"
	options := chiServerOptions(chi.NewRouter())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	options.ErrorHandlerFunc(recorder, request, errors.New(sentinel))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if recorder.Body.String() != "invalid request\n" {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), "invalid request\n")
	}
	if strings.Contains(recorder.Body.String(), sentinel) {
		t.Fatalf("body disclosed sentinel: %q", recorder.Body.String())
	}
}

func TestNewHandlerUsesStaticStrictResponseError(t *testing.T) {
	t.Parallel()

	const sentinel = "private-handler-response-error-854f90be"
	handler := NewHandler(errorServer{err: errors.New(sentinel)}, identityWrapper)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() != "internal server error\n" {
		t.Fatalf("body = %q, want static internal error", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), sentinel) {
		t.Fatalf("body disclosed sentinel: %q", recorder.Body.String())
	}
}

type errorServer struct {
	err error
}

func (server errorServer) GetHealthz(
	context.Context,
	GetHealthzRequestObject,
) (GetHealthzResponseObject, error) {
	return nil, server.err
}

func (server errorServer) InitLedger(context.Context, InitLedgerRequestObject) (InitLedgerResponseObject, error) {
	return nil, server.err
}

func (server errorServer) GetLedgerHead(context.Context, GetLedgerHeadRequestObject) (GetLedgerHeadResponseObject, error) {
	return nil, server.err
}

func (server errorServer) GetRecord(context.Context, GetRecordRequestObject) (GetRecordResponseObject, error) {
	return nil, server.err
}

func (server errorServer) AcceptEndorsement(context.Context, AcceptEndorsementRequestObject) (AcceptEndorsementResponseObject, error) {
	return nil, server.err
}

func (server errorServer) RevokeEndorsement(context.Context, RevokeEndorsementRequestObject) (RevokeEndorsementResponseObject, error) {
	return nil, server.err
}

func (server errorServer) EvaluateConfidence(context.Context, EvaluateConfidenceRequestObject) (EvaluateConfidenceResponseObject, error) {
	return nil, server.err
}

func identityWrapper(handler http.Handler) http.Handler {
	return handler
}

func openTestHTTPServer(t *testing.T) http.Handler {
	t.Helper()

	databaseURL, ok := os.LookupEnv(testDatabaseURLEnvironment)
	if !ok || databaseURL == "" {
		t.Skipf("%s is not set; skipping HTTP database integration test", testDatabaseURLEnvironment)
	}

	store, err := ledgerstore.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("ledgerstore.Open: %v", err)
	}
	t.Cleanup(store.Close)

	localSigner, err := signer.GenerateLocalSigner("local:test:http:signer:v1")
	if err != nil {
		t.Fatalf("GenerateLocalSigner: %v", err)
	}

	admissionSvc, err := admission.New(store, localSigner)
	if err != nil {
		t.Fatalf("admission.New: %v", err)
	}

	apiServer := NewServer(admissionSvc, store)
	return NewHandler(apiServer, nil)
}

func TestHTTPAdmissionLifecycleEndToEnd(t *testing.T) {
	t.Parallel()

	handler := openTestHTTPServer(t)

	// Unique random ledger ID
	var randBytes [16]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(randBytes[:])
	ledgerID := hex.EncodeToString(hash[:])

	// 1. Initialize Ledger
	initReq := httptest.NewRequest(http.MethodPost, "/v1/ledgers/"+ledgerID+"/init", http.NoBody)
	initRec := httptest.NewRecorder()
	handler.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusCreated {
		t.Fatalf("init ledger status = %d, want 201; body: %s", initRec.Code, initRec.Body.String())
	}

	var initResp InitLedgerResponse
	if err := json.Unmarshal(initRec.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("decode init response: %v", err)
	}
	if initResp.SequenceNumber != 1 {
		t.Fatalf("sequence number = %d, want 1", initResp.SequenceNumber)
	}
	genesisRecordDigest := initResp.RecordDigest

	// 2. Query Head
	headReq := httptest.NewRequest(http.MethodGet, "/v1/ledgers/"+ledgerID+"/head", http.NoBody)
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)

	if headRec.Code != http.StatusOK {
		t.Fatalf("get head status = %d, want 200", headRec.Code)
	}
	var headResp HeadResponse
	if err := json.Unmarshal(headRec.Body.Bytes(), &headResp); err != nil {
		t.Fatalf("decode head response: %v", err)
	}
	if headResp.RecordDigest != genesisRecordDigest || headResp.SequenceNumber != 1 {
		t.Fatalf("head = (%s, %d), want (%s, 1)", headResp.RecordDigest, headResp.SequenceNumber, genesisRecordDigest)
	}

	// 3. Query Genesis Record by Digest
	recReq := httptest.NewRequest(http.MethodGet, "/v1/records/"+genesisRecordDigest, http.NoBody)
	recRec := httptest.NewRecorder()
	handler.ServeHTTP(recRec, recReq)

	if recRec.Code != http.StatusOK {
		t.Fatalf("get record status = %d, want 200", recRec.Code)
	}
	var recordResp RecordResponse
	if err := json.Unmarshal(recRec.Body.Bytes(), &recordResp); err != nil {
		t.Fatalf("decode record response: %v", err)
	}
	if recordResp.SequenceNumber != 1 || recordResp.EventKind != int(ledgerv1.EventKindLedgerInitialized) {
		t.Fatalf("record = seq %d kind %d, want seq 1 kind 1", recordResp.SequenceNumber, recordResp.EventKind)
	}

	// 4. Create and Accept Endorsement via HTTP
	proposerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	proposerPubHex := hex.EncodeToString(mustEncodePublicKey(t, &proposerKey.PublicKey))
	subjectPubHex := hex.EncodeToString(mustEncodePublicKey(t, &subjectKey.PublicKey))
	topic := "ai:alignment"
	claimBody := []byte{0xa1, 0x64, 't', 'e', 's', 't', 0x01}
	proposedAt := uint64(1_735_689_600_000)
	acceptedAt := uint64(1_735_689_700_000)

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

	acceptReqBody := AcceptEndorsementRequest{
		ProposerPublicKeyHex: proposerPubHex,
		SubjectPublicKeyHex:  subjectPubHex,
		Topic:                topic,
		ClaimBodyHex:         hex.EncodeToString(claimBody),
		ProposedAtUnixMs:     int(proposedAt),
		ProposerSignatureHex: hex.EncodeToString(proposerSig),
		AcceptedAtUnixMs:     int(acceptedAt),
		SubjectSignatureHex:  hex.EncodeToString(subjectSig),
	}
	acceptJSON, _ := json.Marshal(acceptReqBody)
	acceptReq := httptest.NewRequest(http.MethodPost, "/v1/ledgers/"+ledgerID+"/endorsements/accept", bytes.NewReader(acceptJSON))
	acceptReq.Header.Set("Content-Type", "application/json")
	acceptRec := httptest.NewRecorder()
	handler.ServeHTTP(acceptRec, acceptReq)

	if acceptRec.Code != http.StatusCreated {
		t.Fatalf("accept endorsement status = %d, want 201; body: %s", acceptRec.Code, acceptRec.Body.String())
	}
	var acceptResp AdmitResponse
	if err := json.Unmarshal(acceptRec.Body.Bytes(), &acceptResp); err != nil {
		t.Fatalf("decode accept response: %v", err)
	}
	if acceptResp.SequenceNumber != 2 || acceptResp.EventKind != int(ledgerv1.EventKindEndorsementAccepted) {
		t.Fatalf("admit endorsement = seq %d kind %d, want seq 2 kind 2", acceptResp.SequenceNumber, acceptResp.EventKind)
	}
	admittedEndorsementDigest := acceptResp.RecordDigest

	// 5. Evaluate Confidence via HTTP (Before Revocation)
	confReqBody := EvaluateConfidenceRequest{
		TargetPublicKeyHex: subjectPubHex,
		Topic:              &topic,
		TrustRoots: []TrustRootSpec{
			{
				PublicKeyHex: proposerPubHex,
				Weight:       1.0,
			},
		},
	}
	confJSON, _ := json.Marshal(confReqBody)
	confReq := httptest.NewRequest(http.MethodPost, "/v1/ledgers/"+ledgerID+"/confidence", bytes.NewReader(confJSON))
	confReq.Header.Set("Content-Type", "application/json")
	confRec := httptest.NewRecorder()
	handler.ServeHTTP(confRec, confReq)

	if confRec.Code != http.StatusOK {
		t.Fatalf("evaluate confidence status = %d, want 200; body: %s", confRec.Code, confRec.Body.String())
	}
	var confResp ConfidenceResponse
	if err := json.Unmarshal(confRec.Body.Bytes(), &confResp); err != nil {
		t.Fatalf("decode confidence response: %v", err)
	}
	if confResp.ConfidenceScore != 0.6 {
		t.Fatalf("confidence score = %f, want 0.6", confResp.ConfidenceScore)
	}
	if len(confResp.ContributingRecordsHex) != 1 || confResp.ContributingRecordsHex[0] != admittedEndorsementDigest {
		t.Fatalf("contributing records = %v, want [%s]", confResp.ContributingRecordsHex, admittedEndorsementDigest)
	}

	// 6. Revoke Endorsement via HTTP
	revokedAt := uint64(1_735_689_800_000)
	reason := "expired endorsement"
	revocation, err := endorsementv1.NewRevocation(
		mustParseHex32(t, admittedEndorsementDigest),
		&proposerKey.PublicKey,
		endorsementv1.RevokerRoleProposer,
		revokedAt,
		reason,
	)
	if err != nil {
		t.Fatal(err)
	}
	revokerSig, err := revocation.Sign(proposerKey)
	if err != nil {
		t.Fatal(err)
	}

	revokeReqBody := RevokeEndorsementRequest{
		TargetRecordDigest:  admittedEndorsementDigest,
		RevokerPublicKeyHex: proposerPubHex,
		Role:                1,
		RevokedAtUnixMs:     int(revokedAt),
		Reason:              &reason,
		RevokerSignatureHex: hex.EncodeToString(revokerSig),
	}
	revokeJSON, _ := json.Marshal(revokeReqBody)
	revokeReq := httptest.NewRequest(http.MethodPost, "/v1/ledgers/"+ledgerID+"/endorsements/revoke", bytes.NewReader(revokeJSON))
	revokeReq.Header.Set("Content-Type", "application/json")
	revokeRec := httptest.NewRecorder()
	handler.ServeHTTP(revokeRec, revokeReq)

	if revokeRec.Code != http.StatusCreated {
		t.Fatalf("revoke endorsement status = %d, want 201; body: %s", revokeRec.Code, revokeRec.Body.String())
	}
	var revokeResp AdmitResponse
	if err := json.Unmarshal(revokeRec.Body.Bytes(), &revokeResp); err != nil {
		t.Fatalf("decode revoke response: %v", err)
	}
	if revokeResp.SequenceNumber != 3 || revokeResp.EventKind != int(ledgerv1.EventKindEndorsementRevoked) {
		t.Fatalf("admit revocation = seq %d kind %d, want seq 3 kind 3", revokeResp.SequenceNumber, revokeResp.EventKind)
	}

	// 7. Evaluate Confidence via HTTP (After Revocation)
	confAfterReq := httptest.NewRequest(http.MethodPost, "/v1/ledgers/"+ledgerID+"/confidence", bytes.NewReader(confJSON))
	confAfterReq.Header.Set("Content-Type", "application/json")
	confAfterRec := httptest.NewRecorder()
	handler.ServeHTTP(confAfterRec, confAfterReq)

	if confAfterRec.Code != http.StatusOK {
		t.Fatalf("evaluate confidence after revocation status = %d, want 200", confAfterRec.Code)
	}
	var confAfterResp ConfidenceResponse
	if err := json.Unmarshal(confAfterRec.Body.Bytes(), &confAfterResp); err != nil {
		t.Fatalf("decode confidence response after revocation: %v", err)
	}
	if confAfterResp.ConfidenceScore != 0.0 {
		t.Fatalf("confidence score after revocation = %f, want 0.0", confAfterResp.ConfidenceScore)
	}
	if len(confAfterResp.ContributingRecordsHex) != 0 {
		t.Fatalf("contributing records after revocation = %v, want []", confAfterResp.ContributingRecordsHex)
	}
}

func mustEncodePublicKey(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()
	b, err := pub.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustParseHex32(t *testing.T, s string) [32]byte {
	t.Helper()
	var out [32]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		t.Fatalf("invalid hex 32: %s", s)
	}
	copy(out[:], b)
	return out
}

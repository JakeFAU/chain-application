package ledgerv1

const (
	protocolVersionV1                 uint64 = 1
	recordVersionV1                   uint64 = 1
	ledgerInitializedPayloadVersionV1 uint64 = 1

	ledgerRecordKeyVersion         uint64 = 0
	ledgerRecordKeyRecordBodyBytes uint64 = 1
	ledgerRecordKeyDigestAlgorithm uint64 = 2
	ledgerRecordKeyDigest          uint64 = 3

	recordBodyKeyVersion              uint64 = 0
	recordBodyKeyEventBodyBytes       uint64 = 1
	recordBodyKeyEventDigestAlgorithm uint64 = 2
	recordBodyKeyEventDigest          uint64 = 3
	recordBodyKeySignerKeyReference   uint64 = 4
	recordBodyKeySignatureAlgorithm   uint64 = 5
	recordBodyKeySignatureEncoding    uint64 = 6
	recordBodyKeySignatureBytes       uint64 = 7

	eventBodyKeyProtocolVersion      uint64 = 0
	eventBodyKeyLedgerID             uint64 = 1
	eventBodyKeySequence             uint64 = 2
	eventBodyKeyPreviousRecordDigest uint64 = 3
	eventBodyKeyAdmittedAtUnixMS     uint64 = 4
	eventBodyKeyEventKind            uint64 = 5
	eventBodyKeyPayloadVersion       uint64 = 6
	eventBodyKeyPayloadBytes         uint64 = 7

	digestAlgorithmSHA256    = "sha-256"
	signatureAlgorithmP256   = "ecdsa-p256-sha256"
	signatureEncodingASN1DER = "asn1-der"

	domainEventDigestV1  = "attribution-chain:event-digest:v1"
	domainRecordDigestV1 = "attribution-chain:record-digest:v1"

	eventDigestDomainSeparator   byte = 0
	ledgerInitializedPayloadCBOR      = "\xa0"

	maxPayloadBytes         = 65_536
	maxEventBodyBytes       = 98_304
	maxRecordBodyBytes      = 131_072
	maxLedgerRecordBytes    = 131_200
	maxSignerReferenceBytes = 512
	minSignatureBytes       = 8
	maxSignatureBytes       = 72

	digestBytes   = 32
	ledgerIDBytes = 32

	maxProtocolNestingLevels = 4
	maxProtocolArrayElements = 16
	maxProtocolMapPairs      = 16
)

type LedgerID [ledgerIDBytes]byte

type Digest [digestBytes]byte

type EventKind uint64

const EventKindLedgerInitialized EventKind = 1

type SignatureStatus uint8

const SignatureStatusUnverified SignatureStatus = iota

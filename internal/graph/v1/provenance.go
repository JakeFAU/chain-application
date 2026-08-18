package graphv1

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

// PathHop models a single step along an active attestation path.
type PathHop struct {
	From         IdentityKey     `json:"from"`
	To           IdentityKey     `json:"to"`
	Topic        string          `json:"topic"`
	RecordDigest ledgerv1.Digest `json:"record_digest"`
	Sequence     uint64          `json:"sequence"`
}

// PathTrace records an end-to-end chain of testimony from a trust root to the subject.
type PathTrace struct {
	Root       IdentityKey `json:"root"`
	RootWeight float64     `json:"root_weight"`
	Hops       []PathHop   `json:"hops"`
	PathWeight float64     `json:"path_weight"`
}

// ProvenanceSubgraph contains the exact nodes and edges that established the confidence result.
type ProvenanceSubgraph struct {
	Nodes []IdentityKey      `json:"nodes"`
	Edges []*AttestationEdge `json:"edges"`
}

// ConfidenceResult contains the explainable trust evaluation result with complete cryptographic provenance.
type ConfidenceResult struct {
	Target              IdentityKey        `json:"target"`
	Topic               string             `json:"topic"`
	ConfidenceScore     float64            `json:"confidence_score"`
	Algorithm           string             `json:"algorithm"`
	EvaluatedAtSequence uint64             `json:"evaluated_at_sequence"`
	ContributingPaths   []PathTrace        `json:"contributing_paths"`
	ContributingRecords []ledgerv1.Digest  `json:"contributing_records"`
	ProvenanceSubgraph  ProvenanceSubgraph `json:"provenance_subgraph"`
	Explanation         string             `json:"explanation"`
}

// BuildExplanation generates a human-readable explanation of how the confidence score was derived.
func (res *ConfidenceResult) BuildExplanation() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Evaluated confidence for identity %s on topic %q at ledger sequence %d: score = %.4f (%s).\n",
		res.Target, res.Topic, res.EvaluatedAtSequence, res.ConfidenceScore, res.Algorithm))

	if len(res.ContributingPaths) == 0 {
		sb.WriteString("No active attestation paths found from the configured trust roots.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Confidence derived from %d independent testimonial path(s) across %d underlying ledger record(s):\n",
		len(res.ContributingPaths), len(res.ContributingRecords)))

	for i, path := range res.ContributingPaths {
		sb.WriteString(fmt.Sprintf("  Path #%d (weight = %.4f): Root %s (w=%.2f)", i+1, path.PathWeight, path.Root, path.RootWeight))
		for _, hop := range path.Hops {
			sb.WriteString(fmt.Sprintf(" -> [seq %d / record %x... / %q] -> %s", hop.Sequence, hop.RecordDigest[:8], hop.Topic, hop.To))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// DeduplicateDigests sorts and deduplicates ledger record digests for reproducible provenance output.
func DeduplicateDigests(digests []ledgerv1.Digest) []ledgerv1.Digest {
	if len(digests) == 0 {
		return nil
	}

	sort.Slice(digests, func(i, j int) bool {
		return bytes.Compare(digests[i][:], digests[j][:]) < 0
	})

	deduped := make([]ledgerv1.Digest, 0, len(digests))
	for i, d := range digests {
		if i == 0 || d != digests[i-1] {
			deduped = append(deduped, d)
		}
	}
	return deduped
}

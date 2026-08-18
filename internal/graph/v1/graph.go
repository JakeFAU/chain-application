package graphv1

import (
	"errors"
	"fmt"
	"sync"

	endorsementv1 "github.com/JakeFAU/chain-application/internal/endorsement/v1"
	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

var (
	ErrEdgeNotFound       = errors.New("target attestation edge not found")
	ErrEdgeAlreadyRevoked = errors.New("attestation edge is already revoked")
	ErrUnauthorizedRevoker = errors.New("revoker public key does not match proposer or subject")
	ErrDuplicateEdge      = errors.New("attestation edge with record digest already exists")
)

// Graph represents the thread-safe in-memory projection of admitted endorsements.
type Graph struct {
	mu           sync.RWMutex
	ledgerID     ledgerv1.LedgerID
	lastSequence uint64

	nodes         map[IdentityKey]struct{}
	edgesByDigest map[ledgerv1.Digest]*AttestationEdge
	outEdges      map[IdentityKey][]*AttestationEdge
	inEdges       map[IdentityKey][]*AttestationEdge
	topicIndex    map[string][]*AttestationEdge
}

// NewGraph instantiates an empty attestation graph for a ledger.
func NewGraph(ledgerID ledgerv1.LedgerID) *Graph {
	return &Graph{
		ledgerID:      ledgerID,
		nodes:         make(map[IdentityKey]struct{}),
		edgesByDigest: make(map[ledgerv1.Digest]*AttestationEdge),
		outEdges:      make(map[IdentityKey][]*AttestationEdge),
		inEdges:       make(map[IdentityKey][]*AttestationEdge),
		topicIndex:    make(map[string][]*AttestationEdge),
	}
}

// LedgerID returns the ledger identifier for this graph.
func (g *Graph) LedgerID() ledgerv1.LedgerID {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ledgerID
}

// LastSequence returns the highest sequence number processed into the graph.
func (g *Graph) LastSequence() uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lastSequence
}

// SetLastSequence updates the ledger sequence water mark.
func (g *Graph) SetLastSequence(seq uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastSequence = seq
}

// AddEdge inserts a new admitted endorsement into the graph projection.
func (g *Graph) AddEdge(edge *AttestationEdge) error {
	if edge == nil {
		return errors.New("edge cannot be nil")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	digest := edge.RecordDigest()
	if _, exists := g.edgesByDigest[digest]; exists {
		return fmt.Errorf("%w: %x", ErrDuplicateEdge, digest)
	}

	g.edgesByDigest[digest] = edge
	g.nodes[edge.Proposer()] = struct{}{}
	g.nodes[edge.Subject()] = struct{}{}

	g.outEdges[edge.Proposer()] = append(g.outEdges[edge.Proposer()], edge)
	g.inEdges[edge.Subject()] = append(g.inEdges[edge.Subject()], edge)
	g.topicIndex[edge.Topic()] = append(g.topicIndex[edge.Topic()], edge)

	if edge.Sequence() > g.lastSequence {
		g.lastSequence = edge.Sequence()
	}

	return nil
}

// RevokeEdge marks an existing endorsement as revoked and updates its provenance metadata.
func (g *Graph) RevokeEdge(
	targetDigest ledgerv1.Digest,
	revoker IdentityKey,
	role endorsementv1.RevokerRole,
	revokedAt uint64,
	revocationDigest ledgerv1.Digest,
	reason string,
	revocationSequence uint64,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	edge, exists := g.edgesByDigest[targetDigest]
	if !exists {
		return fmt.Errorf("%w: %x", ErrEdgeNotFound, targetDigest)
	}
	if !edge.isActive {
		return fmt.Errorf("%w: %x", ErrEdgeAlreadyRevoked, targetDigest)
	}

	switch role {
	case endorsementv1.RevokerRoleProposer:
		if revoker != edge.proposer {
			return fmt.Errorf("%w: revoker %s is not proposer %s", ErrUnauthorizedRevoker, revoker, edge.proposer)
		}
	case endorsementv1.RevokerRoleSubject:
		if revoker != edge.subject {
			return fmt.Errorf("%w: revoker %s is not subject %s", ErrUnauthorizedRevoker, revoker, edge.subject)
		}
	default:
		return fmt.Errorf("unknown revoker role: %d", role)
	}

	edge.isActive = false
	edge.revokedAtUnixMS = &revokedAt
	edge.revocationRecordDigest = &revocationDigest
	edge.revocationReason = reason
	edge.revokerRole = role

	if revocationSequence > g.lastSequence {
		g.lastSequence = revocationSequence
	}

	return nil
}

// EdgeByDigest looks up an endorsement edge by its admission record digest.
func (g *Graph) EdgeByDigest(digest ledgerv1.Digest) (*AttestationEdge, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	edge, ok := g.edgesByDigest[digest]
	return edge, ok
}

// OutEdges returns the outgoing endorsement edges from a proposer.
func (g *Graph) OutEdges(proposer IdentityKey, topic string, activeOnly bool) []*AttestationEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := g.outEdges[proposer]
	var result []*AttestationEdge
	for _, edge := range edges {
		if activeOnly && !edge.isActive {
			continue
		}
		if topic != "" && edge.topic != topic {
			continue
		}
		result = append(result, edge)
	}
	return result
}

// InEdges returns the incoming endorsement edges to a subject.
func (g *Graph) InEdges(subject IdentityKey, topic string, activeOnly bool) []*AttestationEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := g.inEdges[subject]
	var result []*AttestationEdge
	for _, edge := range edges {
		if activeOnly && !edge.isActive {
			continue
		}
		if topic != "" && edge.topic != topic {
			continue
		}
		result = append(result, edge)
	}
	return result
}

// TopicEdges returns all edges matching a specific topic.
func (g *Graph) TopicEdges(topic string, activeOnly bool) []*AttestationEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := g.topicIndex[topic]
	var result []*AttestationEdge
	for _, edge := range edges {
		if activeOnly && !edge.isActive {
			continue
		}
		result = append(result, edge)
	}
	return result
}

// Nodes returns a slice of all known identity keys in the graph.
func (g *Graph) Nodes() []IdentityKey {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]IdentityKey, 0, len(g.nodes))
	for node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// NodeCount returns the total number of distinct identities in the graph.
func (g *Graph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgeCount returns the total number of edges (active + revoked).
func (g *Graph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.edgesByDigest)
}

// ActiveEdgeCount returns the count of currently active edges.
func (g *Graph) ActiveEdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	count := 0
	for _, edge := range g.edgesByDigest {
		if edge.isActive {
			count++
		}
	}
	return count
}

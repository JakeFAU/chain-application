package graphv1

import (
	"fmt"
	"math"
	"sort"

	ledgerv1 "github.com/JakeFAU/chain-application/internal/ledger/v1"
)

// Evaluator executes explainable contextual confidence calculations over an Attestation Graph.
type Evaluator struct {
	graph *Graph
}

// NewEvaluator creates an Evaluator bound to a Graph projection.
func NewEvaluator(graph *Graph) *Evaluator {
	return &Evaluator{graph: graph}
}

// Evaluate computes the contextual confidence and provenance subgraph for a target subject under an explicit policy.
func (e *Evaluator) Evaluate(policy EvaluationPolicy, target IdentityKey) (*ConfidenceResult, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("invalid evaluation policy: %w", err)
	}

	var allPaths []PathTrace
	var contributingDigests []ledgerv1.Digest
	subgraphNodeSet := make(map[IdentityKey]struct{})
	subgraphEdgeSet := make(map[ledgerv1.Digest]*AttestationEdge)

	// Sort trust roots deterministically for reproducible path enumeration
	type rootEntry struct {
		key    IdentityKey
		weight float64
	}
	roots := make([]rootEntry, 0, len(policy.TrustRoots))
	for k, w := range policy.TrustRoots {
		roots = append(roots, rootEntry{key: k, weight: w})
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].key.Hex() < roots[j].key.Hex()
	})

	for _, root := range roots {
		if root.key == target {
			// Direct trust root query
			subgraphNodeSet[target] = struct{}{}
			continue
		}

		visited := make(map[IdentityKey]bool)
		visited[root.key] = true

		e.findPaths(
			root.key,
			root.weight,
			root.key,
			root.weight,
			target,
			policy,
			nil,
			visited,
			1,
			&allPaths,
			subgraphNodeSet,
			subgraphEdgeSet,
			&contributingDigests,
		)
	}

	// Calculate combined confidence score
	directTrust := 0.0
	if directWeight, isRoot := policy.TrustRoots[target]; isRoot {
		directTrust = directWeight
	}

	// Combine independent paths using noisy-OR: Score = 1 - (1 - directTrust) * Prod(1 - pathWeight)
	unconfidence := 1.0 - directTrust
	for _, path := range allPaths {
		unconfidence *= (1.0 - path.PathWeight)
	}
	confidenceScore := math.Max(0.0, math.Min(1.0, 1.0-unconfidence))

	// Assemble deduplicated subgraph nodes and edges
	subgraphNodes := make([]IdentityKey, 0, len(subgraphNodeSet))
	for node := range subgraphNodeSet {
		subgraphNodes = append(subgraphNodes, node)
	}
	sort.Slice(subgraphNodes, func(i, j int) bool {
		return subgraphNodes[i].Hex() < subgraphNodes[j].Hex()
	})

	subgraphEdges := make([]*AttestationEdge, 0, len(subgraphEdgeSet))
	for _, edge := range subgraphEdgeSet {
		subgraphEdges = append(subgraphEdges, edge)
	}
	sort.Slice(subgraphEdges, func(i, j int) bool {
		return subgraphEdges[i].Sequence() < subgraphEdges[j].Sequence()
	})

	result := &ConfidenceResult{
		Target:              target,
		Topic:               policy.Topic,
		ConfidenceScore:     confidenceScore,
		Algorithm:           policy.AlgorithmVersion,
		EvaluatedAtSequence: e.graph.LastSequence(),
		ContributingPaths:   allPaths,
		ContributingRecords: DeduplicateDigests(contributingDigests),
		ProvenanceSubgraph: ProvenanceSubgraph{
			Nodes: subgraphNodes,
			Edges: subgraphEdges,
		},
	}
	result.Explanation = result.BuildExplanation()

	return result, nil
}

func (e *Evaluator) findPaths(
	root IdentityKey,
	rootWeight float64,
	current IdentityKey,
	currentWeight float64,
	target IdentityKey,
	policy EvaluationPolicy,
	currentHops []PathHop,
	visited map[IdentityKey]bool,
	depth int,
	allPaths *[]PathTrace,
	subgraphNodeSet map[IdentityKey]struct{},
	subgraphEdgeSet map[ledgerv1.Digest]*AttestationEdge,
	contributingDigests *[]ledgerv1.Digest,
) {
	if depth > policy.MaxHops {
		return
	}

	outEdges := e.graph.OutEdges(current, policy.Topic, true)
	// Sort edges deterministically by sequence number
	sort.Slice(outEdges, func(i, j int) bool {
		return outEdges[i].Sequence() < outEdges[j].Sequence()
	})

	for _, edge := range outEdges {
		neighbor := edge.Subject()
		if visited[neighbor] {
			// Prevent circular cycles on this path
			continue
		}

		hop := PathHop{
			From:         current,
			To:           neighbor,
			Topic:        edge.Topic(),
			RecordDigest: edge.RecordDigest(),
			Sequence:     edge.Sequence(),
		}

		pathWeight := currentWeight * policy.DecayFactor
		if pathWeight < policy.MinConfidence {
			// Damped weight below significance threshold
			continue
		}

		nextHops := make([]PathHop, len(currentHops), len(currentHops)+1)
		copy(nextHops, currentHops)
		nextHops = append(nextHops, hop)

		if neighbor == target {
			*allPaths = append(*allPaths, PathTrace{
				Root:       root,
				RootWeight: rootWeight,
				Hops:       nextHops,
				PathWeight: pathWeight,
			})

			// Add all elements on this winning path to the provenance subgraph
			subgraphNodeSet[root] = struct{}{}
			subgraphNodeSet[target] = struct{}{}
			for _, h := range nextHops {
				subgraphNodeSet[h.From] = struct{}{}
				subgraphNodeSet[h.To] = struct{}{}
				if edgeObj, ok := e.graph.EdgeByDigest(h.RecordDigest); ok {
					subgraphEdgeSet[h.RecordDigest] = edgeObj
				}
				*contributingDigests = append(*contributingDigests, h.RecordDigest)
			}
		} else if depth < policy.MaxHops {
			visited[neighbor] = true
			e.findPaths(
				root,
				rootWeight,
				neighbor,
				pathWeight,
				target,
				policy,
				nextHops,
				visited,
				depth+1,
				allPaths,
				subgraphNodeSet,
				subgraphEdgeSet,
				contributingDigests,
			)
			visited[neighbor] = false
		}
	}
}

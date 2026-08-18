package graphv1

import (
	"errors"
	"fmt"
)

const (
	DefaultAlgorithmVersion = "transitive-path-damping:v1"
	DefaultMaxHops          = 3
	DefaultDecayFactor      = 0.6
	DefaultMinConfidence    = 0.001
)

var (
	ErrEmptyTrustRoots    = errors.New("trust roots map cannot be empty")
	ErrInvalidTrustWeight = errors.New("trust root weight must be in (0, 1.0]")
	ErrInvalidDecayFactor = errors.New("decay factor must be in (0, 1.0)")
	ErrInvalidMaxHops     = errors.New("max hops must be greater than 0")
)

// EvaluationPolicy specifies the explicit, versioned rules used to derive confidence from the attestation graph.
type EvaluationPolicy struct {
	// TrustRoots maps starting seed identities to their initial trusted confidence weight in (0, 1.0].
	TrustRoots map[IdentityKey]float64

	// Topic optionally filters endorsements to a specific domain topic. If empty, evaluates across all topics.
	Topic string

	// MaxHops sets the maximum path distance from any trust root (default 3).
	MaxHops int

	// DecayFactor attenuates trust per hop (default 0.6).
	DecayFactor float64

	// MinConfidence sets the lower bound cutoff for path propagation (default 0.001).
	MinConfidence float64

	// AlgorithmVersion identifies the deterministic scoring rule used.
	AlgorithmVersion string
}

// NewDefaultPolicy creates a standard evaluation policy with the provided trust roots and topic.
func NewDefaultPolicy(trustRoots map[IdentityKey]float64, topic string) EvaluationPolicy {
	rootsCopy := make(map[IdentityKey]float64, len(trustRoots))
	for k, v := range trustRoots {
		rootsCopy[k] = v
	}

	return EvaluationPolicy{
		TrustRoots:       rootsCopy,
		Topic:            topic,
		MaxHops:          DefaultMaxHops,
		DecayFactor:      DefaultDecayFactor,
		MinConfidence:    DefaultMinConfidence,
		AlgorithmVersion: DefaultAlgorithmVersion,
	}
}

// Validate checks all policy parameters and returns an error if any invariant is violated.
func (p *EvaluationPolicy) Validate() error {
	if len(p.TrustRoots) == 0 {
		return ErrEmptyTrustRoots
	}
	for root, weight := range p.TrustRoots {
		if weight <= 0 || weight > 1.0 {
			return fmt.Errorf("%w: root %s has weight %f", ErrInvalidTrustWeight, root, weight)
		}
	}
	if p.MaxHops <= 0 {
		return ErrInvalidMaxHops
	}
	if p.DecayFactor <= 0 || p.DecayFactor >= 1.0 {
		return ErrInvalidDecayFactor
	}
	if p.AlgorithmVersion == "" {
		p.AlgorithmVersion = DefaultAlgorithmVersion
	}
	if p.MinConfidence <= 0 {
		p.MinConfidence = DefaultMinConfidence
	}
	return nil
}

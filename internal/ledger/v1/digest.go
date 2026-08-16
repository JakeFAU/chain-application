package ledgerv1

import "crypto/sha256"

// domainSeparatedDigest hashes body under a protocol domain string joined by a
// single reserved separator byte, so digests taken in different domains cannot
// collide even when the bodies are identical.
func domainSeparatedDigest(domain string, body []byte) Digest {
	preimage := make([]byte, 0, len(domain)+1+len(body))
	preimage = append(preimage, domain...)
	preimage = append(preimage, digestDomainSeparator)
	preimage = append(preimage, body...)
	return Digest(sha256.Sum256(preimage))
}

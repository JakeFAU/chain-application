package ledgerv1

import "crypto/sha256"

// domainSeparatedDigest hashes body under a protocol domain string joined by a
// single reserved separator byte, so an identical body hashed in two different
// domains yields two distinct preimages. Domain separation prevents cross-domain
// preimage reuse; it does not strengthen SHA-256, which collision resistance
// still rests on.
func domainSeparatedDigest(domain string, body []byte) Digest {
	preimage := make([]byte, 0, len(domain)+1+len(body))
	preimage = append(preimage, domain...)
	preimage = append(preimage, digestDomainSeparator)
	preimage = append(preimage, body...)
	return Digest(sha256.Sum256(preimage))
}

// Package token issues and verifies opaque, database-backed security tokens.
//
// Only the presented value contains the secret. Applications persist the
// selector and SHA-256 digest, deliver Presented to the user, and discard it
// after delivery. Parsing a presented value reproduces only the lookup
// selector and digest.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	selectorBytes = 16
	secretBytes   = 32
	digestBytes   = sha256.Size

	selectorLength  = 22 // base64.RawURLEncoding.EncodedLen(selectorBytes)
	secretLength    = 43 // base64.RawURLEncoding.EncodedLen(secretBytes)
	presentedLength = selectorLength + 1 + secretLength
)

var (
	// ErrInvalidToken is returned for every malformed token. It deliberately
	// carries no token fragments or parsing details.
	ErrInvalidToken = errors.New("token: invalid token")
	tokenEncoding   = base64.RawURLEncoding.Strict()
)

// Issued contains the public selector, the digest to persist, and the complete
// value to deliver. Presented is the only returned value containing the secret.
type Issued struct {
	Selector  string
	Presented string
	digest    [digestBytes]byte
}

// Digest returns a defensive copy of the SHA-256 digest to persist.
func (issued Issued) Digest() []byte {
	return append([]byte(nil), issued.digest[:]...)
}

// Parsed contains the public selector and derived digest of a presented token.
// It never retains or exposes the raw secret.
type Parsed struct {
	Selector string
	digest   [digestBytes]byte
	valid    int32
}

// Digest returns a defensive copy of the digest derived during parsing.
func (parsed Parsed) Digest() []byte {
	return append([]byte(nil), parsed.digest[:]...)
}

// Issuer issues tokens from an explicit entropy source. Use Issue for the
// production crypto/rand path; an Issuer is primarily useful for deterministic
// tests or applications with their own cryptographic reader.
type Issuer struct {
	entropy io.Reader
}

// NewIssuer constructs an issuer using entropy. A nil reader is rejected when
// Issue is called rather than silently falling back to another source.
func NewIssuer(entropy io.Reader) Issuer {
	return Issuer{entropy: entropy}
}

// Issue creates a new token using crypto/rand.Reader.
func Issue() (Issued, error) {
	return NewIssuer(rand.Reader).Issue()
}

// Issue creates a new selector.secret token using the issuer's entropy source.
func (issuer Issuer) Issue() (Issued, error) {
	if issuer.entropy == nil {
		return Issued{}, errors.New("token: entropy source is required")
	}

	var material [selectorBytes + secretBytes]byte
	if _, err := io.ReadFull(issuer.entropy, material[:]); err != nil {
		clear(material[:])
		return Issued{}, fmt.Errorf("token: generate entropy: %w", err)
	}

	selector := tokenEncoding.EncodeToString(material[:selectorBytes])
	secret := tokenEncoding.EncodeToString(material[selectorBytes:])
	digest := sha256.Sum256(material[selectorBytes:])
	clear(material[:])

	return Issued{
		Selector:  selector,
		Presented: selector + "." + secret,
		digest:    digest,
	}, nil
}

// Parse validates the exact canonical selector.secret wire format and derives
// the digest used for database comparison. Every invalid input returns
// ErrInvalidToken without disclosing which part failed.
func Parse(presented string) (Parsed, error) {
	if len(presented) != presentedLength || presented[selectorLength] != '.' || strings.Count(presented, ".") != 1 {
		return Parsed{}, ErrInvalidToken
	}

	selectorText := presented[:selectorLength]
	secretText := presented[selectorLength+1:]
	var selector [selectorBytes]byte
	var secret [secretBytes]byte
	if !decodeCanonical(selectorText, selector[:]) || !decodeCanonical(secretText, secret[:]) {
		clear(secret[:])
		return Parsed{}, ErrInvalidToken
	}

	digest := sha256.Sum256(secret[:])
	clear(secret[:])
	return Parsed{Selector: selectorText, digest: digest, valid: 1}, nil
}

func decodeCanonical(encoded string, destination []byte) bool {
	written, err := tokenEncoding.Decode(destination, []byte(encoded))
	if err != nil || written != len(destination) {
		return false
	}
	return tokenEncoding.EncodeToString(destination) == encoded
}

// MatchDigest compares a stored digest with a parsed candidate in constant time.
// Invalid digest lengths never match, but still execute a full digest comparison.
func MatchDigest(stored []byte, candidate Parsed) bool {
	var expected [digestBytes]byte
	copy(expected[:], stored)
	digestMatches := subtle.ConstantTimeCompare(expected[:], candidate.digest[:])
	validCandidate := subtle.ConstantTimeEq(candidate.valid, 1)
	return len(stored) == digestBytes && validCandidate&digestMatches == 1
}

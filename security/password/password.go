// Package password hashes and verifies user passwords with the standard-library
// PBKDF2 implementation. Encoded hashes carry their parameters so applications
// can increase work over time without invalidating existing credentials.
package password

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultIterations = 600_000
	saltBytes         = 16
	keyBytes          = 32
	maximumPassword   = 4096
	maximumIterations = 10_000_000
)

var ErrInvalidHash = errors.New("invalid password hash")

type Hasher struct {
	// Iterations controls newly created hashes. Nonpositive values use the
	// default; Hash rejects values above 10,000,000, the verification limit.
	Iterations int
}

func New() Hasher { return Hasher{Iterations: DefaultIterations} }

func (hasher Hasher) Hash(plain string) (string, error) {
	if plain == "" {
		return "", fmt.Errorf("password cannot be empty")
	}
	if len(plain) > maximumPassword {
		return "", fmt.Errorf("password exceeds %d bytes", maximumPassword)
	}
	iterations := hasher.iterations()
	if iterations > maximumIterations {
		return "", fmt.Errorf("password iterations exceed %d", maximumIterations)
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	derived, err := pbkdf2.Key(sha256.New, plain, salt, iterations, keyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password hash: %w", err)
	}
	return fmt.Sprintf("$goforge$pbkdf2-sha256$%d$%s$%s", iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived)), nil
}

func (hasher Hasher) Verify(encoded, plain string) (bool, error) {
	iterations, salt, expected, err := parse(encoded)
	if err != nil {
		return false, err
	}
	if len(plain) > maximumPassword {
		return false, nil
	}
	actual, err := pbkdf2.Key(sha256.New, plain, salt, iterations, len(expected))
	if err != nil {
		return false, fmt.Errorf("derive password hash: %w", err)
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func (hasher Hasher) NeedsRehash(encoded string) bool {
	iterations, _, _, err := parse(encoded)
	return err != nil || iterations < hasher.iterations()
}

func (hasher Hasher) iterations() int {
	if hasher.Iterations <= 0 {
		return DefaultIterations
	}
	return hasher.Iterations
}

func parse(encoded string) (int, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "goforge" || parts[2] != "pbkdf2-sha256" {
		return 0, nil, nil, ErrInvalidHash
	}
	iterations, err := strconv.Atoi(parts[3])
	if err != nil || iterations < 1 || iterations > maximumIterations {
		return 0, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < saltBytes {
		return 0, nil, nil, ErrInvalidHash
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != keyBytes {
		return 0, nil, nil, ErrInvalidHash
	}
	return iterations, salt, expected, nil
}

// DummyVerify performs equivalent expensive work for unknown-user login paths,
// reducing the usefulness of account-enumeration timing differences.
func (hasher Hasher) DummyVerify(plain string) {
	iterations := hasher.iterations()
	if iterations > maximumIterations {
		iterations = maximumIterations
	}
	_, _ = pbkdf2.Key(sha256.New, plain, make([]byte, saltBytes), iterations, keyBytes)
}

// Package csrf provides session-bound synchronizer tokens.
//
// The package deliberately does not inspect HTTP requests. Applications choose
// where a submitted token comes from and pass it to Validate explicitly.
package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/ShanilKoshitha/goforge/session"
)

const (
	tokenBytes = 32
	sessionKey = "_goforge.csrf.token"
)

var tokenEncoding = base64.RawURLEncoding.Strict()

var (
	// ErrInvalidToken indicates that a submitted token is absent, malformed, or
	// does not belong to the current session.
	ErrInvalidToken = errors.New("csrf: invalid token")
	// ErrSessionRequired indicates programmer error: CSRF tokens must be bound
	// to an initialized session.
	ErrSessionRequired = errors.New("csrf: session is required")
)

// Ensure returns the current session's token, creating one when absent.
// The caller must save the session before writing the response when a token is
// created.
func Ensure(current *session.Session) (string, error) {
	if current == nil {
		return "", ErrSessionRequired
	}

	var token string
	present, err := current.Get(sessionKey, &token)
	if err != nil {
		return "", fmt.Errorf("csrf: read session token: %w", err)
	}
	if present {
		if _, ok := decode(token); !ok {
			return "", ErrInvalidToken
		}
		return token, nil
	}

	return Rotate(current)
}

// Rotate replaces the current session's token with a cryptographically random
// token. Rotate after authentication state changes or suspected disclosure.
// The caller must save the session before writing the response.
func Rotate(current *session.Session) (string, error) {
	if current == nil {
		return "", ErrSessionRequired
	}

	token, err := generate(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := current.Put(sessionKey, token); err != nil {
		return "", fmt.Errorf("csrf: store session token: %w", err)
	}
	return token, nil
}

// Validate verifies that submitted is a well-formed token belonging to the
// current session. Token bytes are compared in constant time. The returned
// errors never contain either the submitted or stored token.
func Validate(current *session.Session, submitted string) error {
	if current == nil {
		return ErrSessionRequired
	}

	var stored string
	present, err := current.Get(sessionKey, &stored)
	if err != nil {
		return fmt.Errorf("csrf: read session token: %w", err)
	}

	expected, expectedOK := decode(stored)
	provided, providedOK := decode(submitted)
	matched := subtle.ConstantTimeCompare(expected[:], provided[:])
	if !present || !expectedOK || !providedOK || matched != 1 {
		return ErrInvalidToken
	}
	return nil
}

func generate(source io.Reader) (string, error) {
	var token [tokenBytes]byte
	if _, err := io.ReadFull(source, token[:]); err != nil {
		return "", fmt.Errorf("csrf: generate token: %w", err)
	}
	return tokenEncoding.EncodeToString(token[:]), nil
}

func decode(value string) ([tokenBytes]byte, bool) {
	var token [tokenBytes]byte
	if len(value) != tokenEncoding.EncodedLen(tokenBytes) {
		return token, false
	}
	written, err := tokenEncoding.Decode(token[:], []byte(value))
	return token, err == nil && written == tokenBytes
}

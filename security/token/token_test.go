package token_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/security/token"
)

func TestIssueProducesCanonicalOpaqueToken(t *testing.T) {
	material := make([]byte, 48)
	for index := range material {
		material[index] = byte(index + 1)
	}
	issued, err := token.NewIssuer(bytes.NewReader(material)).Issue()
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(issued.Presented, ".")
	if len(parts) != 2 || parts[0] != issued.Selector {
		t.Fatalf("presented token has invalid selector framing")
	}
	if len(parts[0]) != 22 || len(parts[1]) != 43 || strings.Contains(issued.Presented, "=") {
		t.Fatalf("presented token is not canonical unpadded base64url")
	}
	selector, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(selector) != 16 {
		t.Fatalf("selector encoding: length=%d err=%v", len(selector), err)
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(secret) != 32 {
		t.Fatalf("secret encoding: length=%d err=%v", len(secret), err)
	}
	expected := sha256.Sum256(secret)
	if !bytes.Equal(issued.Digest(), expected[:]) {
		t.Fatal("issued digest does not hash the delivered secret")
	}

	parsed, err := token.Parse(issued.Presented)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Selector != issued.Selector || !token.MatchDigest(issued.Digest(), parsed) {
		t.Fatal("parsed lookup material does not match the issued token")
	}
}

func TestIssueUsesFreshEntropy(t *testing.T) {
	seenSelectors := make(map[string]struct{})
	seenTokens := make(map[string]struct{})
	for range 128 {
		issued, err := token.Issue()
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := seenSelectors[issued.Selector]; exists {
			t.Fatal("duplicate selector")
		}
		if _, exists := seenTokens[issued.Presented]; exists {
			t.Fatal("duplicate presented token")
		}
		seenSelectors[issued.Selector] = struct{}{}
		seenTokens[issued.Presented] = struct{}{}
	}
}

func TestParseRejectsMalformedNonCanonicalAndOversizedValues(t *testing.T) {
	valid, err := token.NewIssuer(bytes.NewReader(bytes.Repeat([]byte{0x5a}, 48))).Issue()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid.Presented, ".")

	cases := map[string]string{
		"empty":                 "",
		"selector only":         parts[0],
		"missing selector":      "." + parts[1],
		"missing secret":        parts[0] + ".",
		"extra separator":       parts[0] + ".." + parts[1],
		"padded selector":       parts[0] + "=." + parts[1],
		"padded secret":         parts[0] + "." + parts[1] + "=",
		"standard base64":       strings.Repeat("+", 22) + "." + parts[1],
		"whitespace":            " " + valid.Presented,
		"oversized":             valid.Presented + strings.Repeat("a", 4096),
		"noncanonical selector": parts[0][:21] + alternateTrailingBase64(parts[0][21]) + "." + parts[1],
		"noncanonical secret":   parts[0] + "." + parts[1][:42] + alternateTrailingBase64(parts[1][42]),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, parseErr := token.Parse(value)
			if !errors.Is(parseErr, token.ErrInvalidToken) || parseErr != token.ErrInvalidToken {
				t.Fatalf("error = %v, want exact ErrInvalidToken", parseErr)
			}
			if parsed.Selector != "" || len(parsed.Digest()) != sha256.Size || !bytes.Equal(parsed.Digest(), make([]byte, sha256.Size)) {
				t.Fatal("invalid parse returned lookup material")
			}
			if strings.Contains(parseErr.Error(), parts[0]) || strings.Contains(parseErr.Error(), parts[1]) {
				t.Fatal("invalid-token error disclosed token material")
			}
		})
	}
}

func TestMatchDigestRejectsWrongTokenAndInvalidStoredLengths(t *testing.T) {
	first, err := token.NewIssuer(bytes.NewReader(bytes.Repeat([]byte{1}, 48))).Issue()
	if err != nil {
		t.Fatal(err)
	}
	second, err := token.NewIssuer(bytes.NewReader(bytes.Repeat([]byte{2}, 48))).Issue()
	if err != nil {
		t.Fatal(err)
	}
	firstParsed, err := token.Parse(first.Presented)
	if err != nil {
		t.Fatal(err)
	}
	secondParsed, err := token.Parse(second.Presented)
	if err != nil {
		t.Fatal(err)
	}
	if !token.MatchDigest(first.Digest(), firstParsed) {
		t.Fatal("matching digest was rejected")
	}
	if token.MatchDigest(first.Digest(), secondParsed) {
		t.Fatal("different digest matched")
	}
	if token.MatchDigest(make([]byte, sha256.Size), token.Parsed{}) {
		t.Fatal("zero parsed value matched a stored digest")
	}
	for _, stored := range [][]byte{nil, {}, make([]byte, sha256.Size-1), make([]byte, sha256.Size+1)} {
		if token.MatchDigest(stored, firstParsed) {
			t.Fatalf("invalid stored digest length %d matched", len(stored))
		}
	}
}

func TestDigestAccessorsReturnDefensiveCopies(t *testing.T) {
	issued, err := token.NewIssuer(bytes.NewReader(bytes.Repeat([]byte{3}, 48))).Issue()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := token.Parse(issued.Presented)
	if err != nil {
		t.Fatal(err)
	}

	issuedDigest := issued.Digest()
	parsedDigest := parsed.Digest()
	issuedDigest[0] ^= 0xff
	parsedDigest[1] ^= 0xff
	if bytes.Equal(issuedDigest, issued.Digest()) || bytes.Equal(parsedDigest, parsed.Digest()) {
		t.Fatal("digest accessor exposed internal storage")
	}
	if !token.MatchDigest(issued.Digest(), parsed) {
		t.Fatal("caller mutation changed retained digest")
	}

	stored := issued.Digest()
	before := append([]byte(nil), stored...)
	if !token.MatchDigest(stored, parsed) || !bytes.Equal(stored, before) {
		t.Fatal("digest comparison mutated stored bytes")
	}
}

func TestIssuerPropagatesEntropyFailuresWithoutOutput(t *testing.T) {
	sentinel := errors.New("entropy unavailable")
	for name, reader := range map[string]io.Reader{
		"immediate": errorReader{err: sentinel},
		"short":     io.MultiReader(bytes.NewReader(make([]byte, 47)), errorReader{err: sentinel}),
	} {
		t.Run(name, func(t *testing.T) {
			issued, err := token.NewIssuer(reader).Issue()
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v, want entropy cause", err)
			}
			if issued.Selector != "" || issued.Presented != "" || !bytes.Equal(issued.Digest(), make([]byte, sha256.Size)) {
				t.Fatal("entropy failure returned token material")
			}
			if strings.Contains(err.Error(), strings.Repeat("A", 8)) {
				t.Fatal("entropy error disclosed generated material")
			}
		})
	}

	issued, err := token.NewIssuer(nil).Issue()
	if err == nil || issued.Selector != "" || issued.Presented != "" {
		t.Fatal("nil entropy source did not fail closed")
	}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

func alternateTrailingBase64(value byte) string {
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, value)
	if index < 0 {
		return "!"
	}
	// Selector and secret encodings both leave unused low bits. Flipping only a
	// low bit preserves the decoded bytes under permissive decoders but violates
	// RawURLEncoding.Strict's canonical trailing-bit requirement.
	return string(alphabet[index^1])
}

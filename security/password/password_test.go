package password_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/security/password"
)

func TestHashAndVerify(t *testing.T) {
	hasher := password.Hasher{Iterations: 1_000}
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "correct horse") {
		t.Fatal("encoded hash contains the plain password")
	}
	matched, err := hasher.Verify(encoded, "correct horse battery staple")
	if err != nil || !matched {
		t.Fatalf("expected match, got %t, %v", matched, err)
	}
	matched, err = hasher.Verify(encoded, "wrong")
	if err != nil || matched {
		t.Fatalf("expected mismatch, got %t, %v", matched, err)
	}
}

func TestHashUsesFreshSalt(t *testing.T) {
	hasher := password.Hasher{Iterations: 1}
	first, _ := hasher.Hash("password")
	second, _ := hasher.Hash("password")
	if first == second {
		t.Fatal("two hashes reused a salt")
	}
}

func TestVerifyRejectsMalformedAndExpensiveHashes(t *testing.T) {
	hasher := password.Hasher{Iterations: 1}
	for _, encoded := range []string{"garbage", "$goforge$pbkdf2-sha256$999999999$AA$AA"} {
		if _, err := hasher.Verify(encoded, "password"); !errors.Is(err, password.ErrInvalidHash) {
			t.Fatalf("expected invalid hash for %q, got %v", encoded, err)
		}
	}
}

func TestNeedsRehash(t *testing.T) {
	old := password.Hasher{Iterations: 1}
	encoded, err := old.Hash("password")
	if err != nil {
		t.Fatal(err)
	}
	if !(password.Hasher{Iterations: 2}).NeedsRehash(encoded) {
		t.Fatal("expected stronger hasher to request a rehash")
	}
}

func TestHashRejectsUnverifiableIterationCount(t *testing.T) {
	hasher := password.Hasher{Iterations: 10_000_001}
	if encoded, err := hasher.Hash("password"); err == nil || encoded != "" {
		t.Fatalf("expected invalid iteration count to fail before hashing, got %q, %v", encoded, err)
	}
}

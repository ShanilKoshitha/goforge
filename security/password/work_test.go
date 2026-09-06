package password

import (
	"bytes"
	"testing"
)

func TestVerifyCurrentPadsOnlyFailedWeakHashesToCurrentWork(t *testing.T) {
	weak := Hasher{Iterations: 2}
	encoded, err := weak.Hash("correct password")
	if err != nil {
		t.Fatal(err)
	}
	_, _, expected, err := parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	original := derivePassword
	t.Cleanup(func() { derivePassword = original })
	var work []int
	derivePassword = func(_ string, _ []byte, iterations, length int) ([]byte, error) {
		work = append(work, iterations)
		return bytes.Repeat([]byte{0xff}, length), nil
	}

	matched, err := (Hasher{Iterations: 10}).VerifyCurrent(encoded, "wrong password")
	if err != nil || matched {
		t.Fatalf("failed verification = matched %t, err %v", matched, err)
	}
	if len(work) != 2 || work[0] != 2 || work[1] != 8 {
		t.Fatalf("weak mismatch work = %v, want encoded 2 plus padding 8", work)
	}

	work = nil
	matched, err = (Hasher{Iterations: maximumIterations + 1}).VerifyCurrent(encoded, "wrong password")
	if err != nil || matched {
		t.Fatalf("capped verification = matched %t, err %v", matched, err)
	}
	if len(work) != 2 || work[0] != 2 || work[1] != maximumIterations-2 {
		t.Fatalf("misconfigured work = %v, want verification-limit cap", work)
	}

	work = nil
	derivePassword = func(_ string, _ []byte, iterations, _ int) ([]byte, error) {
		work = append(work, iterations)
		return append([]byte(nil), expected...), nil
	}
	matched, err = (Hasher{Iterations: 10}).VerifyCurrent(encoded, "correct password")
	if err != nil || !matched {
		t.Fatalf("successful verification = matched %t, err %v", matched, err)
	}
	if len(work) != 1 || work[0] != 2 {
		t.Fatalf("successful weak verification work = %v, want only encoded work before rehash", work)
	}
}

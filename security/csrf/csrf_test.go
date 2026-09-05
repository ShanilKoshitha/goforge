package csrf

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/session"
)

func TestEnsureCreatesAndReusesRandomToken(t *testing.T) {
	current := freshSession(t)
	first, err := Ensure(current)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("token is not unpadded base64url: %v", err)
	}
	if len(decoded) != tokenBytes {
		t.Fatalf("decoded token length = %d, want %d", len(decoded), tokenBytes)
	}
	if strings.Contains(first, "=") {
		t.Fatal("token contains base64 padding")
	}

	second, err := Ensure(current)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("Ensure replaced an existing valid token")
	}
	if err := Validate(current, first); err != nil {
		t.Fatalf("Validate rejected ensured token: %v", err)
	}
}

func TestRotateReplacesAndInvalidatesPreviousToken(t *testing.T) {
	current := freshSession(t)
	first, err := Ensure(current)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Rotate(current)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("Rotate returned the previous token")
	}
	if err := Validate(current, first); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old token validation error = %v, want ErrInvalidToken", err)
	}
	if err := Validate(current, second); err != nil {
		t.Fatalf("new token validation failed: %v", err)
	}
}

func TestTokensAreBoundToTheirSession(t *testing.T) {
	firstSession := freshSession(t)
	secondSession := freshSession(t)
	first, err := Ensure(firstSession)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Ensure(secondSession)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("independent sessions received the same token")
	}
	if err := Validate(firstSession, second); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("cross-session validation error = %v, want ErrInvalidToken", err)
	}
	if err := Validate(secondSession, first); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("cross-session validation error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenSurvivesSessionPersistence(t *testing.T) {
	manager, err := session.NewManager(
		session.NewMemoryStore(),
		[]byte(strings.Repeat("s", 32)),
		session.Cookie{UnsafeAllowHTTP: true},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	token, err := Ensure(current)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := manager.Save(ctx, response, current); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(response.Result().Cookies()[0])
	reloaded, err := manager.Load(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	reloadedToken, err := Ensure(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedToken != token {
		t.Fatal("persisted session did not retain its CSRF token")
	}
	if err := Validate(reloaded, token); err != nil {
		t.Fatalf("persisted token validation failed: %v", err)
	}
}

func TestValidateRejectsAbsentMalformedAndMismatchedTokens(t *testing.T) {
	withoutToken := freshSession(t)
	if err := Validate(withoutToken, strings.Repeat("A", 43)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("absent token error = %v, want ErrInvalidToken", err)
	}

	current := freshSession(t)
	valid, err := Ensure(current)
	if err != nil {
		t.Fatal(err)
	}
	otherBytes := bytes.Repeat([]byte{0xff}, tokenBytes)
	other := base64.RawURLEncoding.EncodeToString(otherBytes)
	tampered := differentBase64URLCharacter(valid[0]) + valid[1:]

	for name, submitted := range map[string]string{
		"empty":         "",
		"short":         valid[:len(valid)-1],
		"long":          valid + "A",
		"padded":        base64.URLEncoding.EncodeToString(otherBytes),
		"invalid chars": strings.Repeat("!", len(valid)),
		"other token":   other,
		"tampered":      tampered,
	} {
		t.Run(name, func(t *testing.T) {
			err := Validate(current, submitted)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("validation error = %v, want ErrInvalidToken", err)
			}
			if strings.Contains(err.Error(), submitted) && submitted != "" {
				t.Fatal("validation error disclosed the submitted token")
			}
		})
	}
}

func TestAPIsRejectNilSession(t *testing.T) {
	if _, err := Ensure(nil); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("Ensure error = %v, want ErrSessionRequired", err)
	}
	if _, err := Rotate(nil); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("Rotate error = %v, want ErrSessionRequired", err)
	}
	if err := Validate(nil, "token"); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("Validate error = %v, want ErrSessionRequired", err)
	}
}

func TestCorruptStoredTokenIsRejectedWithoutDisclosure(t *testing.T) {
	current := freshSession(t)
	const corrupt = "not a valid token"
	if err := current.Put(sessionKey, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(current); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Ensure error = %v, want ErrInvalidToken", err)
	} else if strings.Contains(err.Error(), corrupt) {
		t.Fatal("Ensure error disclosed the stored token")
	}
	if err := Validate(current, strings.Repeat("A", 43)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Validate error = %v, want ErrInvalidToken", err)
	} else if strings.Contains(err.Error(), corrupt) {
		t.Fatal("Validate error disclosed the stored token")
	}
}

func TestGenerateReportsEntropyFailureWithoutPartialToken(t *testing.T) {
	sentinel := errors.New("entropy unavailable")
	token, err := generate(io.MultiReader(bytes.NewReader(make([]byte, tokenBytes-1)), errorReader{sentinel}))
	if token != "" {
		t.Fatal("generation failure returned a partial token")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("generation error = %v, want entropy error", err)
	}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

func freshSession(t *testing.T) *session.Session {
	t.Helper()
	manager, err := session.NewManager(
		session.NewMemoryStore(),
		[]byte(strings.Repeat("s", 32)),
		session.Cookie{},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.Load(context.Background(), httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func differentBase64URLCharacter(current byte) string {
	if current == 'A' {
		return "B"
	}
	return "A"
}

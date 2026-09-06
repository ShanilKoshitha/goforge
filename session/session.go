// Package session provides signed-ID, server-side HTTP sessions. Session data is
// never placed in the cookie, and applications can replace the backing Store.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("session not found")
	ErrExists   = errors.New("session already exists")
)

// Store persists sessions until their expiry. Create must return ErrExists when
// an ID is already present. Get and Update must return ErrNotFound for missing
// or expired IDs. Rotate must atomically replace oldID with newID, returning
// ErrNotFound when oldID is no longer current and ErrExists when newID already
// exists. Implementations must be safe for concurrent use.
type Store interface {
	Get(context.Context, string) ([]byte, error)
	Create(context.Context, string, []byte, time.Time) error
	Update(context.Context, string, []byte, time.Time) error
	Rotate(context.Context, string, string, []byte, time.Time) error
	Delete(context.Context, string) error
}

type Cookie struct {
	Name                  string
	Path                  string
	Domain                string
	UnsafeAllowHTTP       bool
	UnsafeAllowJavaScript bool
	SameSite              http.SameSite
}

// Policy controls how long a session may remain idle and how long it may exist
// in total. AbsoluteLifetime zero disables the absolute deadline. When enabled,
// it must be at least IdleLifetime so the two expiration rules remain legible.
type Policy struct {
	IdleLifetime     time.Duration
	AbsoluteLifetime time.Duration
}

type Manager struct {
	store  Store
	secret []byte
	cookie Cookie
	policy Policy
	now    func() time.Time
}

// NewManager retains the original sliding-lifetime behavior. Applications that
// need a hard session deadline should use NewManagerWithPolicy.
func NewManager(store Store, secret []byte, cookie Cookie, lifetime time.Duration) (*Manager, error) {
	if lifetime <= 0 {
		lifetime = 2 * time.Hour
	}
	return NewManagerWithPolicy(store, secret, cookie, Policy{IdleLifetime: lifetime})
}

// NewManagerWithPolicy constructs a manager with explicit idle and absolute
// lifetimes. Session issuance is persisted in the server-side payload; changing
// or rotating the signed cookie cannot extend the absolute deadline.
func NewManagerWithPolicy(store Store, secret []byte, cookie Cookie, policy Policy) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("session secret must be at least 32 bytes")
	}
	if policy.IdleLifetime <= 0 {
		return nil, fmt.Errorf("session idle lifetime must be positive")
	}
	if policy.AbsoluteLifetime < 0 {
		return nil, fmt.Errorf("session absolute lifetime cannot be negative")
	}
	if policy.AbsoluteLifetime > 0 && policy.AbsoluteLifetime < policy.IdleLifetime {
		return nil, fmt.Errorf("session absolute lifetime cannot be shorter than idle lifetime")
	}
	if cookie.Name == "" {
		cookie.Name = "goforge_session"
	}
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if cookie.SameSite == 0 {
		cookie.SameSite = http.SameSiteLaxMode
	}
	return &Manager{
		store: store, secret: append([]byte(nil), secret...), cookie: cookie,
		policy: policy, now: time.Now,
	}, nil
}

type payload struct {
	IssuedAt time.Time                  `json:"issued_at,omitempty"`
	Values   map[string]json.RawMessage `json:"values,omitempty"`
	Flash    map[string]json.RawMessage `json:"flash,omitempty"`
}

// Session holds one request's mutable state and is not safe for concurrent use.
// Obtain sessions through Manager.Load and persist changes with Manager.Save.
type Session struct {
	id                      string
	persistedID             string
	values                  map[string]json.RawMessage
	oldFlash                map[string]json.RawMessage
	newFlash                map[string]json.RawMessage
	issuedAt                time.Time
	createOnMissingRotation bool
}

func (manager *Manager) Load(ctx context.Context, request *http.Request) (*Session, error) {
	id := ""
	if cookie, err := request.Cookie(manager.cookie.Name); err == nil {
		id = manager.verify(cookie.Value)
	}
	if id == "" {
		return manager.fresh()
	}
	encoded, err := manager.store.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return manager.fresh()
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	var stored payload
	if err := json.Unmarshal(encoded, &stored); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	if stored.Values == nil {
		stored.Values = make(map[string]json.RawMessage)
	}
	if stored.Flash == nil {
		stored.Flash = make(map[string]json.RawMessage)
	}
	now := manager.now().UTC()
	if stored.IssuedAt.IsZero() {
		if manager.policy.AbsoluteLifetime > 0 {
			// A legacy payload has no trustworthy absolute starting point. Enabling
			// an absolute policy therefore invalidates it instead of granting a new
			// full lifetime to an arbitrarily old authenticated session.
			if err := manager.store.Delete(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("remove session without issuance time: %w", err)
			}
			return manager.fresh()
		}
		stored.IssuedAt = now
	}
	if manager.absoluteExpired(stored.IssuedAt, now) {
		if err := manager.store.Delete(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("remove absolutely expired session: %w", err)
		}
		return manager.fresh()
	}
	return &Session{
		id: id, persistedID: id, values: stored.Values,
		oldFlash: stored.Flash, newFlash: make(map[string]json.RawMessage),
		issuedAt: stored.IssuedAt,
	}, nil
}

// Save persists changes and refreshes the cookie. Call it before writing a
// response; writers exposing Written report attempts to save too late.
func (manager *Manager) Save(ctx context.Context, response http.ResponseWriter, session *Session) error {
	if session == nil || session.id == "" {
		return fmt.Errorf("cannot save an uninitialized session")
	}
	if state, ok := response.(interface{ Written() bool }); ok && state.Written() {
		return fmt.Errorf("cannot save session after response header is written")
	}
	now := manager.now().UTC()
	if session.issuedAt.IsZero() {
		session.issuedAt = now
	}
	if manager.absoluteExpired(session.issuedAt, now) {
		return manager.expire(ctx, response, session)
	}
	expiresAt := now.Add(manager.policy.IdleLifetime)
	if manager.policy.AbsoluteLifetime > 0 {
		absoluteDeadline := session.issuedAt.Add(manager.policy.AbsoluteLifetime)
		if absoluteDeadline.Before(expiresAt) {
			expiresAt = absoluteDeadline
		}
	}
	encoded, err := json.Marshal(payload{
		IssuedAt: session.issuedAt, Values: session.values, Flash: session.newFlash,
	})
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	var persistErr error
	switch {
	case session.persistedID == "":
		persistErr = manager.store.Create(ctx, session.id, encoded, expiresAt)
	case session.persistedID == session.id:
		persistErr = manager.store.Update(ctx, session.id, encoded, expiresAt)
	default:
		persistErr = manager.store.Rotate(ctx, session.persistedID, session.id, encoded, expiresAt)
		if session.createOnMissingRotation && errors.Is(persistErr, ErrNotFound) {
			// Another request may have invalidated the old row after this request
			// authenticated but before a credential-change rotation was saved. The
			// new ID is independent and still carries the caller's freshly verified
			// identity, so creating it preserves the winning request without
			// restoring the invalidated ID.
			persistErr = manager.store.Create(ctx, session.id, encoded, expiresAt)
		}
	}
	if persistErr != nil {
		return fmt.Errorf("save session: %w", persistErr)
	}
	session.persistedID = session.id
	session.createOnMissingRotation = false
	session.oldFlash = make(map[string]json.RawMessage)
	manager.setCookie(response, &http.Cookie{
		Name: manager.cookie.Name, Value: manager.sign(session.id), Path: manager.cookie.Path,
		Domain: manager.cookie.Domain, Expires: expiresAt, MaxAge: cookieMaxAge(expiresAt.Sub(now)),
		Secure: !manager.cookie.UnsafeAllowHTTP, HttpOnly: !manager.cookie.UnsafeAllowJavaScript, SameSite: manager.cookie.SameSite,
	})
	return nil
}

// Regenerate stages a new ID while retaining values. Save atomically replaces
// a persisted old ID and sends the new cookie, typically after authentication.
// The context parameter is retained for API compatibility; persistence happens
// only in Save so a failed replacement cannot destroy the current session.
func (manager *Manager) Regenerate(_ context.Context, session *Session) error {
	return manager.regenerate(session, false)
}

// RegenerateOrCreate stages a new ID whose save may create the new row if a
// concurrent request removed the old one. Use this only after an independent,
// durable authorization change has already succeeded (for example a password
// compare-and-swap); ordinary rotations must use Regenerate and fail closed.
func (manager *Manager) RegenerateOrCreate(_ context.Context, session *Session) error {
	return manager.regenerate(session, true)
}

func (manager *Manager) regenerate(session *Session, createOnMissing bool) error {
	if session == nil {
		return fmt.Errorf("cannot regenerate a nil session")
	}
	fresh, err := newID()
	if err != nil {
		return err
	}
	session.id = fresh
	session.createOnMissingRotation = createOnMissing
	return nil
}

func (manager *Manager) Destroy(ctx context.Context, response http.ResponseWriter, session *Session) error {
	if state, ok := response.(interface{ Written() bool }); ok && state.Written() {
		return fmt.Errorf("cannot destroy session after response header is written")
	}
	if err := manager.expire(ctx, response, session); err != nil {
		return fmt.Errorf("destroy session: %w", err)
	}
	return nil
}

// Invalidate removes a server-side session without writing a cookie. It is
// intended for stale authenticated requests: a late deletion cookie from one
// response must not overwrite a newly rotated cookie from a concurrent winning
// response in the same browser. Explicit logout should continue to use Destroy.
func (manager *Manager) Invalidate(ctx context.Context, response http.ResponseWriter, session *Session) error {
	if state, ok := response.(interface{ Written() bool }); ok && state.Written() {
		return fmt.Errorf("cannot invalidate session after response header is written")
	}
	if err := manager.invalidate(ctx, session); err != nil {
		return fmt.Errorf("invalidate session: %w", err)
	}
	manager.removeCookie(response)
	return nil
}

func (session *Session) ID() string { return session.id }

func (session *Session) Put(key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode session value %q: %w", key, err)
	}
	session.values[key] = encoded
	return nil
}

func (session *Session) Get(key string, target any) (bool, error) {
	encoded, ok := session.values[key]
	if !ok {
		encoded, ok = session.oldFlash[key]
	}
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return true, fmt.Errorf("decode session value %q: %w", key, err)
	}
	return true, nil
}

func (session *Session) Forget(key string) {
	delete(session.values, key)
	delete(session.oldFlash, key)
	delete(session.newFlash, key)
}

func (session *Session) Flash(key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode flash value %q: %w", key, err)
	}
	session.newFlash[key] = encoded
	return nil
}

func (manager *Manager) fresh() (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return &Session{
		id: id, values: make(map[string]json.RawMessage),
		oldFlash: make(map[string]json.RawMessage), newFlash: make(map[string]json.RawMessage),
		issuedAt: manager.now().UTC(),
	}, nil
}

func (manager *Manager) absoluteExpired(issuedAt, now time.Time) bool {
	return manager.policy.AbsoluteLifetime > 0 && !now.Before(issuedAt.Add(manager.policy.AbsoluteLifetime))
}

func (manager *Manager) expire(ctx context.Context, response http.ResponseWriter, session *Session) error {
	if err := manager.invalidate(ctx, session); err != nil {
		return err
	}
	manager.setCookie(response, &http.Cookie{
		Name: manager.cookie.Name, Value: "", Path: manager.cookie.Path, Domain: manager.cookie.Domain,
		MaxAge: -1, Expires: time.Unix(1, 0), Secure: !manager.cookie.UnsafeAllowHTTP,
		HttpOnly: !manager.cookie.UnsafeAllowJavaScript, SameSite: manager.cookie.SameSite,
	})
	return nil
}

func (manager *Manager) invalidate(ctx context.Context, session *Session) error {
	if session != nil && session.id != "" {
		if session.persistedID != "" {
			if err := manager.store.Delete(ctx, session.persistedID); err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		session.id = ""
		session.persistedID = ""
		session.values = make(map[string]json.RawMessage)
		session.oldFlash = make(map[string]json.RawMessage)
		session.newFlash = make(map[string]json.RawMessage)
		session.issuedAt = time.Time{}
		session.createOnMissingRotation = false
	}
	return nil
}

// setCookie makes the manager's last write on a response authoritative. This
// matters when middleware refreshes a session before an authentication handler
// rotates or destroys it on the same response. Cookies with other names remain
// application-owned and retain their original order.
func (manager *Manager) setCookie(response http.ResponseWriter, cookie *http.Cookie) {
	manager.removeCookie(response)
	http.SetCookie(response, cookie)
}

func (manager *Manager) removeCookie(response http.ResponseWriter) {
	if response == nil {
		return
	}
	header := response.Header()
	existing := header.Values("Set-Cookie")
	header.Del("Set-Cookie")
	for _, value := range existing {
		parsed, err := http.ParseSetCookie(value)
		if err != nil || parsed.Name != manager.cookie.Name {
			header.Add("Set-Cookie", value)
		}
	}
}

func cookieMaxAge(remaining time.Duration) int {
	seconds := int(remaining / time.Second)
	if remaining <= 0 || seconds == 0 {
		// Max-Age has one-second resolution and takes precedence over Expires.
		// Deleting slightly early is the only representation that cannot let a
		// cookie outlive a sub-second absolute deadline.
		return -1
	}
	return seconds
}

func newID() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func (manager *Manager) sign(id string) string {
	mac := hmac.New(sha256.New, manager.secret)
	_, _ = mac.Write([]byte(id))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return id + "." + signature
}

func (manager *Manager) verify(value string) string {
	id, signature, ok := strings.Cut(value, ".")
	if !ok || len(id) != 64 {
		return ""
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, manager.secret)
	_, _ = mac.Write([]byte(id))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	return id
}

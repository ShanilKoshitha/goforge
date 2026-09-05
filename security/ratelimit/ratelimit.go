// Package ratelimit provides explicit fixed-window request throttling with a
// replaceable persistence contract. The in-memory store is bounded and suited
// to one-process applications; distributed deployments can replace Store.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
)

var ErrInvalidConfiguration = errors.New("rate limiter configuration is invalid")

type Policy struct {
	Limit  int
	Window time.Duration
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Store atomically consumes one attempt for key. Reset removes accumulated
// attempts, normally after successful authentication.
type Store interface {
	Take(context.Context, string, Policy, time.Time) (Decision, error)
	Reset(context.Context, string) error
}

type Limiter struct {
	store  Store
	policy Policy
	now    func() time.Time
}

func New(store Store, policy Policy) (*Limiter, error) {
	if store == nil || policy.Limit < 1 || policy.Window <= 0 {
		return nil, ErrInvalidConfiguration
	}
	return &Limiter{store: store, policy: policy, now: time.Now}, nil
}

func (limiter *Limiter) Take(ctx context.Context, key string) (Decision, error) {
	if limiter == nil || limiter.store == nil {
		return Decision{}, ErrInvalidConfiguration
	}
	if strings.TrimSpace(key) == "" {
		return Decision{}, fmt.Errorf("rate limit key is required")
	}
	decision, err := limiter.store.Take(ctx, key, limiter.policy, limiter.now().UTC())
	if err != nil {
		return Decision{}, fmt.Errorf("consume rate limit: %w", err)
	}
	return decision, nil
}

func (limiter *Limiter) Reset(ctx context.Context, key string) error {
	if limiter == nil || limiter.store == nil {
		return ErrInvalidConfiguration
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("rate limit key is required")
	}
	if err := limiter.store.Reset(ctx, key); err != nil {
		return fmt.Errorf("reset rate limit: %w", err)
	}
	return nil
}

// Enforce consumes one attempt and returns a generic HTTP 429 error when the
// policy is exhausted. The response discloses neither the key nor counters.
func (limiter *Limiter) Enforce(ctx *httpx.Context, key string) error {
	decision, err := limiter.Take(ctx.Request.Context(), key)
	if err != nil {
		return err
	}
	if decision.Allowed {
		return nil
	}
	ctx.Response.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds(decision.RetryAfter), 10))
	return httpx.NewHTTPError(http.StatusTooManyRequests, "too many requests")
}

type KeyFunc func(*httpx.Context) string

// Middleware enforces a limiter before invoking the next handler.
func Middleware(limiter *Limiter, key KeyFunc) httpx.Middleware {
	if limiter == nil {
		panic("ratelimit: limiter is required")
	}
	if key == nil {
		panic("ratelimit: key function is required")
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx *httpx.Context) error {
			if err := limiter.Enforce(ctx, key(ctx)); err != nil {
				return err
			}
			return next(ctx)
		}
	}
}

// Key hashes namespaced key parts so stores do not retain raw email addresses,
// IP addresses, or other identifying inputs.
func Key(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// RemoteAddress returns the direct peer address without trusting spoofable
// forwarding headers. Applications behind trusted proxies can supply their own
// KeyFunc after validating proxy configuration.
func RemoteAddress(request *http.Request) string {
	if request == nil {
		return "unknown"
	}
	address := strings.TrimSpace(request.RemoteAddr)
	host, _, err := net.SplitHostPort(address)
	if err == nil && host != "" {
		return host
	}
	if address == "" {
		return "unknown"
	}
	return address
}

func retryAfterSeconds(duration time.Duration) int64 {
	seconds := int64((duration + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

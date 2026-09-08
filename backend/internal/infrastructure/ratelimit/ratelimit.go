// Package ratelimit provides a dependency-free fixed-window rate limiter and
// an echo middleware helper. It is used to protect unauthenticated surfaces
// (webhook receivers) and the authentication failure path from brute force.
package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Limiter is an in-memory fixed-window rate limiter keyed by arbitrary
// strings (typically remote IP addresses). Accounting is per process, which
// matches the one-machine-per-app deployment model of the Fly API and
// Cloud Run services.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	buckets map[string]*bucket
}

type bucket struct {
	count   int
	resetAt time.Time
}

// New creates a limiter allowing maxPerWindow events per key per window.
// A non-positive max disables limiting entirely.
func New(maxPerWindow int, window time.Duration) *Limiter {
	return &Limiter{max: maxPerWindow, window: window, buckets: map[string]*bucket{}}
}

// Allow reports whether one more event is permitted for key in the current
// window. Disabled limiters (max <= 0) always allow.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok || now.After(b.resetAt) {
		// Opportunistic cleanup keeps the map bounded against IP churn.
		if len(l.buckets) > 10000 {
			for k, e := range l.buckets {
				if now.After(e.resetAt) {
					delete(l.buckets, k)
				}
			}
		}
		l.buckets[key] = &bucket{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

// EchoMiddleware returns echo middleware that enforces the limiter using the
// key produced by keyFn. Requests without a key bypass the limiter.
func EchoMiddleware(l *Limiter, keyFn func(c echo.Context) string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if key := keyFn(c); key == "" || !l.Allow(key) {
				c.Response().Header().Set(echo.HeaderRetryAfter, "60")
				return echo.NewHTTPError(http.StatusTooManyRequests, "Rate limit exceeded")
			}
			return next(c)
		}
	}
}

// Package authzscope carries the authenticated storage principal through a
// request context. It intentionally has no dependency on HTTP or persistence.
package authzscope

import "context"

type contextKey struct{}

// Principal is the identity allowed to open user- or team-owned KV records.
// An absent Principal denotes an internal/system operation for backwards
// compatibility with workers and administrative migration commands.
type Principal struct {
	ActorID string
	UserID  string
	TeamIDs []string
	Admin   bool
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	principal.TeamIDs = append([]string(nil), principal.TeamIDs...)
	return context.WithValue(ctx, contextKey{}, principal)
}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(contextKey{}).(Principal)
	return principal, ok
}

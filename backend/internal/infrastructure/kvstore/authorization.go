package kvstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/authzscope"
)

var invalidLabelCharacter = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

var teamOwnerLabels = []string{
	"agentapi.proxy/team-id", "agentapi.proxy/session-profile-team-id-hash",
	"agentapi.proxy/session-route-team-id-hash", "agentapi.proxy/slackbot-team-id-hash",
	"agentapi.proxy/webhook-team-id-hash", "agentapi.proxy/team-hash", "agentapi.proxy/schedule-team-id",
}

var userOwnerLabels = []string{
	"agentapi.proxy/user-id", "agentapi.proxy/session-profile-user-id",
	"agentapi.proxy/session-route-user-id", "agentapi.proxy/slackbot-user-id",
	"agentapi.proxy/webhook-user-id", "agentapi.proxy/owner-hash", "agentapi.proxy/schedule-user-id",
}

var ambiguousOwnerLabels = []string{
	"agentapi.proxy/api-token-owner", "agentapi.proxy/settings-name", "agentapi.proxy/credentials-name",
}

type scopeObservation struct {
	started time.Time
	scopes  map[string]struct{}
}

var accessObservations = struct {
	sync.Mutex
	actors map[string]*scopeObservation
}{actors: make(map[string]*scopeObservation)}

func ownerScopeForRecord(record Record) string {
	copy := record
	copy.OwnerScope = ""
	return scopedBranchScope(copy)
}

func authorizeRecord(ctx context.Context, record Record, operation string) bool {
	principal, restricted := authzscope.FromContext(ctx)
	if !restricted {
		return true
	}
	scope, scoped := recordOwnerScope(record)
	if !scoped {
		return true
	}
	allowed := principalCanAccess(principal, record.Labels)
	auditStorageAccess(principal.ActorID, scope, record, operation, allowed)
	return allowed
}

func recordOwnerScope(record Record) (string, bool) {
	for _, key := range append(append(append([]string{}, teamOwnerLabels...), userOwnerLabels...), ambiguousOwnerLabels...) {
		if record.Labels[key] != "" {
			return ownerScopeForRecord(record), true
		}
	}
	return ownerScopeForRecord(record), false
}

func principalCanAccess(principal authzscope.Principal, labels map[string]string) bool {
	for _, key := range teamOwnerLabels {
		if owner := labels[key]; owner != "" {
			if principal.Admin {
				return true
			}
			return anyIdentityMatches(owner, principal.TeamIDs)
		}
	}
	for _, key := range userOwnerLabels {
		if owner := labels[key]; owner != "" {
			return identityMatches(owner, principal.UserID)
		}
	}
	for _, key := range ambiguousOwnerLabels {
		if owner := labels[key]; owner != "" {
			if key == "agentapi.proxy/api-token-owner" && labels["agentapi.proxy/api-token-scope"] == "team" {
				return principal.Admin || anyIdentityMatches(owner, principal.TeamIDs)
			}
			if identityMatches(owner, principal.UserID) {
				return true
			}
			return principal.Admin || anyIdentityMatches(owner, principal.TeamIDs)
		}
	}
	return true
}

func anyIdentityMatches(stored string, identities []string) bool {
	for _, identity := range identities {
		if identityMatches(stored, identity) {
			return true
		}
	}
	return false
}

func identityMatches(stored, identity string) bool {
	if identity == "" {
		return false
	}
	digest := sha256.Sum256([]byte(identity))
	return stored == identity || stored == sanitizeOwnerLabel(identity) ||
		stored == hex.EncodeToString(digest[:])[:16] || stored == hex.EncodeToString(digest[:])[:63]
}

func sanitizeOwnerLabel(value string) string {
	value = invalidLabelCharacter.ReplaceAllString(value, "-")
	if len(value) > 63 {
		value = value[:63]
	}
	return strings.Trim(value, "-_.")
}

func auditStorageAccess(actor, scope string, record Record, operation string, allowed bool) {
	actorHash := shortAuditHash(actor)
	resourceHash := shortAuditHash(string(record.Kind) + "\x00" + record.Namespace + "\x00" + record.Key)
	if !allowed {
		slog.Warn("kv scoped access denied", "actor_hash", actorHash, "owner_scope", scope,
			"resource_hash", resourceHash, "operation", operation)
		return
	}
	slog.Info("kv scoped access", "actor_hash", actorHash, "owner_scope", scope,
		"resource_hash", resourceHash, "operation", operation)
	observeScope(actorHash, scope)
}

func observeScope(actorHash, scope string) {
	now := time.Now()
	accessObservations.Lock()
	defer accessObservations.Unlock()
	observation := accessObservations.actors[actorHash]
	if observation == nil || now.Sub(observation.started) >= time.Minute {
		observation = &scopeObservation{started: now, scopes: make(map[string]struct{})}
		accessObservations.actors[actorHash] = observation
	}
	observation.scopes[scope] = struct{}{}
	if len(observation.scopes) == 33 {
		slog.Warn("kv scoped access anomaly", "actor_hash", actorHash,
			"unique_owner_scopes", len(observation.scopes), "window", "1m")
	}
}

func shortAuditHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

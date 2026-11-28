---
name: verify-session-secret-authorization
description: Verify the authorization boundary for listing or retrieving profile-selected settings Secrets from a running session. Use whenever changing session-control authentication, route capability persistence, profile secret_ids, settings scope resolution, the provisioner settings-secret proxy, or ccplant client list-secrets/get-secret.
---

# Verify session settings-secret authorization

The promise is: a settings Secret value is returned to a session only when its runtime token and generation are current, its settings scope was resolved by the server, and its Secret ID belongs to the immutable profile snapshot captured for that session generation. Agent requests cannot expand the snapshot, and a running profile edit takes effect only after restart creates a new generation. Missing authorization data fails closed.

Run from the repository root:

```bash
verification/session_secret_authorization/verify.sh
```

The script runs TLC 1.8.0 over repeated requests, running profile edits, and restarts, as well as every combination of valid/invalid token, current/stale generation, user/team settings scope, and selected/unselected Secret ID. It then runs focused Go tests against the real controller, route persistence, local provisioner proxy, and CLI decoder. Java, curl, mise, and network access for the first TLC download are required; the jar is cached outside the repository.

The verifier also runs five deliberately broken configurations. Four remove exactly one gate—token, generation, scope, or Secret ID—and must produce an `AuthorizedDisclosure` counterexample. The fifth lets an agent request add its Secret ID to the active capability and must violate `CapabilityIsLaunchSnapshot`. If a broken configuration passes, the verification is invalid and the script fails.

## Reading a result

- `Model checking completed. No error has been found.` for `good.cfg` means every reachable profile-edit, restart, and request state preserves both invariants.
- `AuthorizedDisclosure` failing means a value was returned without every required authorization fact, or the returned value came from the wrong scope/resource.
- `CapabilityIsLaunchSnapshot` failing means an agent-controlled request changed the launch-time authorization snapshot.
- A focused Go test failure identifies drift in the concrete implementation.

For a TLC failure, use `/statechart` to render the shortest counterexample in repository terms. Do not weaken the invariant. Determine whether the implementation violates the boundary or the model no longer matches intended behavior.

## Code covered by the model

- `backend/internal/interfaces/controllers/session_secret_controller.go`: `ListAuthorized`, `GetAuthorized`, `authorizedSettingsSecrets`
- `backend/internal/usecases/ports/repositories/session_route_repository.go`: persisted capability fields
- `backend/internal/infrastructure/repositories/kubernetes_session_route_repository.go`: capability serialization and copy isolation
- `backend/internal/infrastructure/services/session_allocator.go`: built-in route capability construction
- `backend/internal/infrastructure/services/native_session_manager.go`: native-session authorization facts
- `backend/internal/infrastructure/services/kubernetes_session_manager.go`: session settings do not project profile secrets into environment variables or files
- `backend/internal/app/server.go`: pooled route capability construction
- `backend/internal/app/session_manager_runtime.go`: stock-runner alias capability preservation
- `backend/internal/interfaces/controllers/provisioner_controller.go`: allocation completion capability preservation
- `backend/pkg/provisioner/server.go`: loopback-only local proxy and parent credential forwarding
- `backend/cmd/client.go`: `list-secrets`, `get-secret`, and response handling

TLC treats each profile edit, restart, or request as atomic and verifies a two-scope/two-secret/two-generation finite model across repeated actions. It does not prove Kubernetes RBAC, encryption at rest, host/Pod isolation, memory erasure, logging by external middleware, or that only a human can edit a profile. The capability is a launch-time snapshot; restarting a session is required to pick up profile selection changes.

## Keeping the model synchronized

Last synchronized against implementation commit `125dad3`.

When covered code changes, compare every authentication and authorization gate with `Gate`, `AuthorizedDisclosure`, and `CapabilityIsLaunchSnapshot`. Update the model and focused tests when behavior intentionally changes. Run the verifier, confirm all five broken configurations are still caught, and move the synchronization marker only after the implementation commit is known.

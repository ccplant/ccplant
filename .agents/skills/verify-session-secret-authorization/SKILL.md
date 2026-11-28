---
name: verify-session-secret-authorization
description: Verify the authorization boundary for retrieving profile-selected settings Secret values from a running session. Use whenever changing session-control authentication, route capability persistence, profile secret_ids, settings scope resolution, the provisioner settings-secret proxy, or ccplant client get-secret.
---

# Verify session settings-secret authorization

The promise is: a settings Secret value is returned to a session only when its runtime token and generation are current, its settings scope was resolved by the server, and its Secret ID was explicitly selected in the session profile. Missing authorization data fails closed.

Run from the repository root:

```bash
verification/session_secret_authorization/verify.sh
```

The script runs TLC 1.8.0 over every combination of valid/invalid token, current/stale generation, user/team settings scope, and selected/unselected Secret ID. It then runs focused Go tests against the real controller, route persistence, local provisioner proxy, and CLI decoder. Java, curl, mise, and network access for the first TLC download are required; the jar is cached outside the repository.

The verifier also runs four deliberately broken configurations. Each removes exactly one gate—token, generation, scope, or Secret ID—and must produce an `AuthorizedDisclosure` counterexample. If a broken configuration passes, the verification is invalid and the script fails.

## Reading a result

- `Model checking completed. No error has been found.` for `good.cfg` means all 16 request combinations preserve the disclosure invariant.
- `AuthorizedDisclosure` failing means a value was returned without every required authorization fact, or the returned value came from the wrong scope/resource.
- A focused Go test failure identifies drift in the concrete implementation.

For a TLC failure, use `/statechart` to render the shortest counterexample in repository terms. Do not weaken the invariant. Determine whether the implementation violates the boundary or the model no longer matches intended behavior.

## Code covered by the model

- `backend/internal/interfaces/controllers/session_secret_controller.go`: `GetAuthorized`, `authorizedSettingsSecrets`
- `backend/internal/usecases/ports/repositories/session_route_repository.go`: persisted capability fields
- `backend/internal/infrastructure/repositories/kubernetes_session_route_repository.go`: capability serialization and copy isolation
- `backend/internal/infrastructure/services/session_allocator.go`: built-in route capability construction
- `backend/internal/infrastructure/services/native_session_manager.go`: native-session authorization facts
- `backend/internal/app/server.go`: pooled route capability construction
- `backend/internal/app/session_manager_runtime.go`: stock-runner alias capability preservation
- `backend/internal/interfaces/controllers/provisioner_controller.go`: allocation completion capability preservation
- `backend/pkg/provisioner/server.go`: loopback-only local proxy and parent credential forwarding
- `backend/cmd/client.go`: `get-secret` and response handling

TLC treats one request as atomic and verifies a two-scope/two-secret finite model. It does not prove Kubernetes RBAC, encryption at rest, host/Pod isolation, memory erasure, logging by external middleware, or authorization changes after a session starts. The capability is a launch-time snapshot; restarting a session is required to pick up profile selection changes.

## Keeping the model synchronized

Last synchronized against commit `3610e56`.

When covered code changes, compare every authentication and authorization gate with `Gate` and `AuthorizedDisclosure`. Update the model and focused tests when behavior intentionally changes. Run the verifier, confirm all four broken configurations are still caught, and move the synchronization marker only after the implementation commit is known.

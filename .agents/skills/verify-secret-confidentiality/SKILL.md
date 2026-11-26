---
name: verify-secret-confidentiality
description: Verify that API-managed secret values remain write-only and that updates which omit secret fields preserve existing values. Use whenever changing session profiles, webhooks, schedules, SlackBots, managed files, session sharing, or response DTOs and update merge semantics for those resources.
---

# Verify secret confidentiality

The user-facing promise is: a stored secret can be supplied or rotated, but ordinary create-after-the-fact, read, list, preview, simulation, and update responses never reveal it; omitting a secret field during update leaves the stored value unchanged.

Run from the repository root:

```bash
verification/secret_confidentiality/verify.sh
```

The script runs TLC 1.8.0 over `verification/secret_confidentiality/SecretConfidentiality.tla`, then runs the Go tests tied to the real response and merge code. Java, curl, mise, and network access for the first TLC download are required. The downloaded jar is cached outside the repository.

## Reading a result

- `Model checking completed. No error has been found.` means every reachable operation sequence in the finite model preserves all three invariants.
- `StoredSecretPreserved` failing means an omitted update erased or replaced a stored secret.
- `ReadResponsesAreSecretIndependent` failing means two tenants differing only in secret value can be distinguished through a read response.
- `SecretsAreWriteOnly` failing means a non-rotation response contains a stored secret.
- A Go test failure identifies the concrete response mapper or merge path that drifted from the model.

For a TLC failure, use `/statechart` to render the shortest counterexample in the repository's terms. Do not weaken an invariant to make a failure pass. Decide whether the code violates the promise or the model no longer describes the intended API.

## Code covered by the model

- `backend/internal/interfaces/controllers/session_profile_controller.go`: `UpdateSessionProfile`, `toResponse`
- `backend/internal/modules/webhook/controller.go`: `UpdateWebhook`, `toResponse`, `sessionConfigToResponse`
- `backend/internal/modules/webhook/handlers.go`: dry-run response construction
- `backend/internal/modules/schedule/handlers.go`: `UpdateSchedule`, `toResponse`
- `backend/internal/modules/slackbot/controller.go`: `UpdateSlackBot`, `ToResponse`, `fromEntitySessionConfig`
- `backend/internal/interfaces/controllers/file_controller.go`: `toFileResponse`
- `backend/internal/interfaces/controllers/share_controller.go`: `CreateShare`, `GetShare`
- `backend/internal/interfaces/controllers/acp_controller.go`: `HandleSessionSSE` header logging

TLC treats each handler's load/merge/save transition as atomic. It verifies every sequence of reads, omitted updates, provided updates, and rotations over two distinct secret values; it does not prove authorization, storage encryption, process-memory secrecy, or downstream agent behavior.

## Keeping the model synchronized

Last synchronized against commit `bcbd465385e141b7c375634bfb46ad7c91aaf0a4`, including the secret-response worktree changes dated 2026-11-26.

When any covered function changes, compare its response disclosure rule and omitted-field merge behavior with the corresponding TLA+ action. Update the model and Go tests when behavior intentionally changes, rerun the verifier, and move the synchronization marker to the checked commit only after confirming the model still describes the code.

Before trusting changes to this skill, prove it can fail: on a scratch change, make a read mapper return a stored secret or make an omitted update clear one, run the command and retain the counterexample/test failure, revert only that scratch change, and rerun until both TLC and the Go checks pass.
